import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/blocked_member.dart';
import 'package:vocatogether/api/languages.dart';
import 'package:vocatogether/api/member_profile.dart';
import 'package:vocatogether/api/report_reason.dart';
import 'package:vocatogether/auth/auth_tokens.dart';
import 'package:vocatogether/auth/google_identity_exception.dart';
import 'package:vocatogether/auth/token_store.dart';
import 'package:vocatogether/session.dart';

import 'support/fakes.dart';

/// Secrets with distinctive markers, so any copy of them can be found.
final _access1 = accessToken('LEAKaccessOne');
final _refresh1 = refreshToken('LEAKrefreshOne');
final _access2 = accessToken('LEAKaccessTwo');
final _refresh2 = refreshToken('LEAKrefreshTwo');
const _email = 'leak.email@example.com';
const _password = 'LEAK-password-123';
const _idToken = 'LEAKidtoken.payload.signature';
const _idToken2 = 'LEAKidtokenTwo.payload.signature';
const _displayName = 'LEAK-display-name';
const _bio = 'LEAK-bio about me';

/// No real code looks like this; the client sends a code as given.
const _language = 'LEAKlanguage';

/// A photo, marked: ASCII only, so it can be found in a request body read
/// as text. The client sends a photo as given and never looks inside.
final _image = Uint8List.fromList(utf8.encode('LEAKimage-bytes-of-a-photo'));

/// How those bytes would look if a list of them were ever printed.
final _imageAsList = _image.join(', ');

/// The picture the server stores and returns: not what was uploaded.
final _storedImage = [0xff, 0xd8, 0xff, 0xe0, 1, 2, 3];

/// A member's public identifier. It can't carry the word, being a UUID.
const _memberId = '1eac0000-1eac-41ea-81ea-c00000001eac';

/// A member the user blocks and reports: its identifier may travel only in
/// the path of the block and report routes.
const _targetId = '1eac1111-1eac-41ea-91ea-c11111111eac';

/// What a member types about another in a report.
const _details = 'LEAK-details of a report';

final _markers = ['LEAK', 'leak.email', _imageAsList, _memberId, _targetId];

/// Where each secret may appear in a request: (path, location).
typedef _Place = (String path, String location);

void main() {
  test(
    'secrets travel only where the backend reads them, and never leak',
    () async {
      final printed = <String>[];
      final strings = <String>[];
      final originalDebugPrint = debugPrint;
      debugPrint = (String? message, {int? wrapWidth}) {
        if (message != null) printed.add(message);
      };
      addTearDown(() => debugPrint = originalDebugPrint);

      await runZoned(
        () => _runEveryFlow(strings),
        zoneSpecification: ZoneSpecification(
          print: (self, parent, zone, line) => printed.add(line),
        ),
      ).then((server) {
        _checkPlacement(server.requests);
      });

      expect(printed, isEmpty, reason: 'nothing in api/auth/session prints');
      for (final s in strings) {
        for (final m in _markers) {
          expect(s, isNot(contains(m)), reason: s);
        }
      }
    },
  );
}

/// Runs every operation, successes and failures, recording every
/// `toString` a log line or crash report could show.
Future<FakeServer> _runEveryFlow(List<String> strings) async {
  final server = FakeServer();
  final store = InMemoryTokenStore();
  final clock = FakeAuthClock();
  final api = authApiFor(server.client);
  final account = accountApiFor(server.client);
  // Google hands over marked ID tokens, then fails.
  final google = FakeGoogleIdentity()
    ..next(_idToken)
    ..fail(const GoogleIdentityException(GoogleIdentityFailure.misconfigured))
    ..next(_idToken2);
  final manager = SessionManager(
    store: store,
    authApi: api,
    clock: clock,
    google: google,
  );
  addTearDown(manager.dispose);

  // Error bodies that echo secrets, as a broken proxy might.
  final echo =
      '{"error":{"code":"invalid_credentials","detail":"$_password '
      '$_email $_refresh1 $_idToken"}}';
  http.Response echoing(int status) => http.Response(
    echo,
    status,
    headers: {'content-type': 'application/json'},
  );

  Future<void> record(Future<Object?> Function() f) async {
    try {
      final v = await f();
      strings.add('$v');
    } on Object catch (e) {
      strings.add('$e');
      if (e is Error) strings.add('${e.stackTrace}');
    }
  }

  await manager.restore();

  // Public operations: success and secret-echoing failures.
  server
    ..once(
      'POST',
      ApiPaths.register,
      (_) => jsonResponse(202, {'status': 'accepted'}),
    )
    ..once('POST', ApiPaths.register, (_) => echoing(422))
    ..once(
      'POST',
      ApiPaths.resendVerification,
      (_) => jsonResponse(202, {'status': 'accepted'}),
    )
    ..once('POST', ApiPaths.forgotPassword, (_) => echoing(500));
  await record(() => account.register(email: _email, password: _password));
  await record(() => account.register(email: _email, password: _password));
  await record(() => account.resendVerification(email: _email));
  await record(() => account.forgotPassword(email: _email));

  // Sign-in failures, including a malformed 200 that contains tokens.
  server
    ..once('POST', ApiPaths.login, (_) => echoing(401))
    ..once('POST', ApiPaths.google, (_) => echoing(401))
    ..once(
      'POST',
      ApiPaths.login,
      (_) => jsonResponse(200, {'access_token': _access1, 'note': _password}),
    )
    ..once(
      'POST',
      ApiPaths.google,
      (_) => jsonResponse(200, tokenBody(_access1, _refresh1)),
    );
  await record(() => manager.signIn(email: _email, password: _password));
  await record(manager.signInWithGoogle);
  await record(() => manager.signIn(email: _email, password: _password));
  // Google gives no token, then Google sign-in succeeds with a new one.
  await record(manager.signInWithGoogle);
  await record(manager.signInWithGoogle);
  expect(manager.status, SessionStatus.signedIn);
  expect(store.raw, isNot(contains('LEAKidtoken')), reason: 'never stored');

  // An authorized call that is refused, refreshes and retries.
  var valid = _access2;
  server
    ..always('GET', ApiPaths.healthz, (_) => healthy())
    ..always(
      'GET',
      ApiPaths.me,
      (r) => r.headers['Authorization'] == 'Bearer $valid'
          ? jsonResponse(200, {
              'id': 'u1',
              'email': _email,
              'email_verified_at': '2026-10-01T00:00:00Z',
              'created_at': '2026-10-01T00:00:00Z',
            })
          : http.Response('401 $_access1', 401),
    )
    ..once(
      'POST',
      ApiPaths.refresh,
      (_) => jsonResponse(200, tokenBody(_access2, _refresh2)),
    );
  await record(() => manager.me());

  // The profile (027): none yet, saved with marked text that the server
  // returns, then refused with a body that echoes it.
  server
    ..once('GET', ApiPaths.profile, (_) => noProfile())
    ..once(
      'PUT',
      ApiPaths.profile,
      (_) =>
          jsonResponse(200, profileBody(displayName: _displayName, bio: _bio)),
    )
    ..once(
      'GET',
      ApiPaths.profile,
      (_) =>
          jsonResponse(200, profileBody(displayName: _displayName, bio: _bio)),
    )
    ..once(
      'PUT',
      ApiPaths.profile,
      (_) => http.Response(
        '{"error":{"code":"validation_failed","detail":"$_displayName $_bio"}}',
        422,
        headers: {'content-type': 'application/json'},
      ),
    );
  await record(() => manager.profile());
  await record(() => manager.saveProfile(displayName: _displayName, bio: _bio));
  await record(() => manager.profile());
  await record(() => manager.saveProfile(displayName: _displayName, bio: _bio));

  // Languages (029): the catalog, then the member's own, read, saved with a
  // marked code that the server returns, and refused with a body that
  // echoes it.
  final selection = UserLanguages(
    spoken: const [UserLanguage(_language, LanguageLevel.native)],
    learning: const [],
  );
  final stored = languagesBody(spoken: [(_language, 'native')]);
  server
    ..once('GET', ApiPaths.languages, (_) => jsonResponse(200, catalogBody()))
    ..once('GET', ApiPaths.myLanguages, (_) => jsonResponse(200, stored))
    ..once('PUT', ApiPaths.myLanguages, (_) => jsonResponse(200, stored))
    ..once(
      'PUT',
      ApiPaths.myLanguages,
      (_) => http.Response(
        '{"error":{"code":"validation_failed","detail":"$_language"}}',
        422,
        headers: {'content-type': 'application/json'},
      ),
    );
  await record(() => manager.languageCatalog());
  await record(() => manager.languages());
  await record(() => manager.saveLanguages(selection));
  await record(() => manager.saveLanguages(selection));
  strings
    ..add('$selection')
    ..add('${selection.spoken}')
    ..add('${selection.spoken.single}');

  // The picture (031): none yet, uploaded with marked bytes, read, refused
  // with a body that echoes them, and removed.
  server
    ..once('GET', ApiPaths.myAvatar, (_) => noAvatar())
    ..once('PUT', ApiPaths.myAvatar, (_) => imageResponse(_storedImage))
    ..once('GET', ApiPaths.myAvatar, (_) => imageResponse(_storedImage))
    ..once(
      'PUT',
      ApiPaths.myAvatar,
      (r) => http.Response(
        '{"error":{"code":"validation_failed","fields":[{"field":"avatar",'
        '"code":"invalid_image"}],"detail":"${r.body} $_imageAsList"}}',
        422,
        headers: {'content-type': 'application/json'},
      ),
    )
    // An answer that is the upload itself under the wrong type.
    ..once(
      'PUT',
      ApiPaths.myAvatar,
      (r) => http.Response.bytes(
        r.bodyBytes,
        200,
        headers: {'content-type': 'application/octet-stream'},
      ),
    )
    ..once('DELETE', ApiPaths.myAvatar, (_) => noContent())
    ..once('DELETE', ApiPaths.myAvatar, (_) => echoing(500));
  await record(() => manager.avatar());
  await record(() => manager.saveAvatar(_image));
  await record(() => manager.avatar());
  await record(() => manager.saveAvatar(_image));
  await record(() => manager.saveAvatar(_image));
  await record(() => manager.removeAvatar());
  await record(() => manager.removeAvatar());

  // A member's public profile and picture (031): read, missing, and refused
  // with a body that echoes the identifier and the profile's text.
  final memberPath = ApiPaths.memberProfile(_memberId);
  final memberAvatarPath = ApiPaths.memberAvatar(_memberId);
  final member = memberProfileBody(
    id: _memberId,
    displayName: _displayName,
    bio: _bio,
    hasAvatar: true,
    languages: languagesBody(spoken: [(_language, 'native')]),
  );
  http.Response echoingMember(int status) => http.Response(
    '{"error":{"code":"internal_error","detail":"$_memberId $_displayName"}}',
    status,
    headers: {'content-type': 'application/json'},
  );
  server
    ..once('GET', memberPath, (_) => jsonResponse(200, member))
    ..once('GET', memberPath, (_) => noProfile())
    ..once('GET', memberPath, (_) => echoingMember(500))
    // A profile that is off-contract, with everything personal in it.
    ..once('GET', memberPath, (_) => jsonResponse(200, {...member, 'bio': 1}))
    ..once('GET', memberAvatarPath, (_) => imageResponse(_storedImage))
    ..once('GET', memberAvatarPath, (_) => noAvatar())
    ..once('GET', memberAvatarPath, (_) => echoingMember(429));
  for (var i = 0; i < 4; i++) {
    await record(() => manager.memberProfile(_memberId));
  }
  for (var i = 0; i < 3; i++) {
    await record(() => manager.memberAvatar(_memberId));
  }
  // An identifier that isn't one is refused without being echoed.
  await record(() async => manager.memberProfile('$_memberId-LEAK'));
  await record(() async => manager.memberAvatar('$_memberId-LEAK'));
  await record(() async => ApiPaths.memberProfile('$_memberId-LEAK'));
  await record(() async => ApiPaths.memberAvatar(_memberId.toUpperCase()));
  strings.add('${MemberProfile.fromJson(member)}');

  // Blocks and reports (033): the list, a block, an unblock and a report
  // with marked details, each also refused with a body that echoes the
  // identifier, the name and the details.
  final blockPath = ApiPaths.myBlock(_targetId);
  final reportPath = ApiPaths.myReport(_targetId);
  final blocks = blocksBody([(_targetId, _displayName)]);
  http.Response echoingReport(int status) => http.Response(
    '{"error":{"code":"validation_failed","fields":[{"field":"details",'
    '"code":"too_long"}],"detail":"$_targetId $_displayName $_details"}}',
    status,
    headers: {'content-type': 'application/json'},
  );
  server
    ..once('GET', ApiPaths.myBlocks, (_) => jsonResponse(200, blocks))
    // A list that is off-contract, with a member in it.
    ..once(
      'GET',
      ApiPaths.myBlocks,
      (_) => jsonResponse(200, {
        'blocks': [
          {'id': _targetId, 'display_name': 7, 'note': _displayName},
        ],
      }),
    )
    ..once('GET', ApiPaths.myBlocks, (_) => echoingReport(500))
    ..once('PUT', blockPath, (_) => noContent())
    ..once('PUT', blockPath, (_) => echoingReport(422))
    ..once('DELETE', blockPath, (_) => noContent())
    ..once('DELETE', blockPath, (_) => echoingReport(429))
    ..once('PUT', reportPath, (_) => noContent())
    ..once('PUT', reportPath, (_) => echoingReport(422))
    // An answer that is the report itself.
    ..once(
      'PUT',
      reportPath,
      (r) => http.Response(
        r.body,
        200,
        headers: {'content-type': 'application/json'},
      ),
    );
  for (var i = 0; i < 3; i++) {
    await record(() => manager.blockedMembers());
  }
  for (var i = 0; i < 2; i++) {
    await record(() => manager.blockMember(_targetId));
  }
  for (var i = 0; i < 2; i++) {
    await record(() => manager.unblockMember(_targetId));
  }
  for (var i = 0; i < 3; i++) {
    await record(
      () => manager.reportMember(
        _targetId,
        reason: ReportReason.harassment,
        details: _details,
      ),
    );
  }
  // An identifier that isn't one is refused without being echoed.
  await record(() async => manager.blockMember('$_targetId-LEAK'));
  await record(() async => manager.unblockMember(_targetId.toUpperCase()));
  await record(
    () async => manager.reportMember(
      '$_targetId-LEAK',
      reason: ReportReason.other,
      details: _details,
    ),
  );
  await record(() async => ApiPaths.myBlock('$_targetId-LEAK'));
  await record(() async => ApiPaths.myReport(_targetId.toUpperCase()));
  final blocked = BlockedMember.listFromJson(blocks);
  strings
    ..add('$blocked')
    ..add('${blocked.single}')
    ..add('${ReportReason.harassment}');

  // Programming errors are refused without echoing the value.
  await record(() async => api.refresh(refreshToken: _access2));
  await record(() async => api.logout(accessToken: _refresh2));
  await record(() async => api.google(idToken: '$_idToken${'x' * 5000}'));
  await record(
    () => apiClientFor(server.client).send(
      'GET',
      ApiPaths.me,
      bearer: _refresh2,
      timeout: const Duration(seconds: 1),
    ),
  );

  // Stored data and value objects.
  strings
    ..add(store.raw == null ? '' : '${store.session}')
    ..add('${AuthTokens.fromJson(tokenBody(_access1, _refresh1))}')
    ..add('${StoredSessionCodec.decode(store.raw!)}')
    ..add('$manager');
  final saved = store.raw;
  await record(() async {
    store.raw = '{"at":"$_access1"}';
    return store.read();
  });
  store.raw = saved;

  // Refresh failures, then logout.
  valid = 'never';
  server.once('POST', ApiPaths.refresh, (_) => echoing(401));
  await record(() => manager.me());
  server
    ..once(
      'POST',
      ApiPaths.login,
      (_) => jsonResponse(200, tokenBody(_access1, _refresh1)),
    )
    ..once('POST', ApiPaths.logout, (_) => echoing(500));
  await record(() => manager.signIn(email: _email, password: _password));
  await record(manager.logout);
  expect(manager.status, SessionStatus.signedOut);

  // Exceptions built from hostile bodies directly.
  strings
    ..add('${ApiHttpException.fromBody(400, jsonDecode(echo))}')
    ..add(
      '${ApiHttpException.fromBody(400, {
        'error': {
          'code': _refresh1,
          'fields': [
            {'field': _email, 'code': _password},
          ],
        },
      })}',
    );
  return server;
}

/// Asserts each secret appears only in its one allowed place.
void _checkPlacement(List<http.Request> requests) {
  final memberPath = ApiPaths.memberProfile(_memberId);
  final memberAvatarPath = ApiPaths.memberAvatar(_memberId);
  final blockPath = ApiPaths.myBlock(_targetId);
  final reportPath = ApiPaths.myReport(_targetId);
  // The routes that carry the access token.
  final protected = [
    ApiPaths.me,
    ApiPaths.profile,
    ApiPaths.languages,
    ApiPaths.myLanguages,
    ApiPaths.myAvatar,
    memberPath,
    memberAvatarPath,
    ApiPaths.myBlocks,
    blockPath,
    reportPath,
    ApiPaths.logout,
  ];
  final allowed = <String, Set<_Place>>{
    _access1: {for (final path in protected) (path, 'authorization')},
    _access2: {for (final path in protected) (path, 'authorization')},
    // What a member writes goes only into the body of their own save.
    _displayName: {(ApiPaths.profile, 'body')},
    _bio: {(ApiPaths.profile, 'body')},
    // A member's languages go only into the body of their own save.
    _language: {(ApiPaths.myLanguages, 'body')},
    // A photo goes only into the body of the member's own upload.
    utf8.decode(_image): {(ApiPaths.myAvatar, 'body')},
    _imageAsList: {},
    // A member's identifier goes only into the path of the two member
    // routes: never a query, a header or a body.
    _memberId: {(memberPath, 'url'), (memberAvatarPath, 'url')},
    // The member a block or a report is about is named only by the path of
    // its own route, and what is typed about them goes only into the body
    // of the report.
    _targetId: {(blockPath, 'url'), (reportPath, 'url')},
    _details: {(reportPath, 'body')},
    _refresh1: {(ApiPaths.refresh, 'body')},
    _refresh2: {(ApiPaths.refresh, 'body')},
    _idToken: {(ApiPaths.google, 'body')},
    _idToken2: {(ApiPaths.google, 'body')},
    _password: {(ApiPaths.register, 'body'), (ApiPaths.login, 'body')},
    _email: {
      (ApiPaths.register, 'body'),
      (ApiPaths.login, 'body'),
      (ApiPaths.resendVerification, 'body'),
      (ApiPaths.forgotPassword, 'body'),
    },
  };
  final seen = <String, Set<_Place>>{};

  for (final r in requests) {
    final path = r.url.path;
    expect(r.url.hasQuery, isFalse);
    expect(r.url.origin, testBaseUrl.origin);
    final locations = <String, String>{
      'url': r.url.toString(),
      'body': r.body,
      for (final h in r.headers.entries) h.key.toLowerCase(): h.value,
    };
    for (final MapEntry(key: secret, value: places) in allowed.entries) {
      for (final MapEntry(key: where, value: text) in locations.entries) {
        if (text.contains(secret)) {
          final place = (path, where);
          expect(places, contains(place), reason: '$secret at $place');
          seen.putIfAbsent(secret, () => {}).add(place);
        }
      }
    }
    if (path == ApiPaths.healthz) {
      expect(r.headers.containsKey('Authorization'), isFalse);
      expect(r.bodyBytes, isEmpty);
    }
    if (r.headers['Authorization'] case final auth?) {
      expect(protected, contains(path));
      expect(isAccessToken(auth.replaceFirst('Bearer ', '')), isTrue);
    }
  }

  // The flows above really exercised each allowed place.
  expect(seen[_access2], contains((ApiPaths.me, 'authorization')));
  expect(seen[_access1], contains((ApiPaths.logout, 'authorization')));
  expect(seen[_access2], contains((ApiPaths.profile, 'authorization')));
  expect(seen[_displayName], contains((ApiPaths.profile, 'body')));
  expect(seen[_bio], contains((ApiPaths.profile, 'body')));
  expect(seen[_access2], contains((ApiPaths.languages, 'authorization')));
  expect(seen[_access2], contains((ApiPaths.myLanguages, 'authorization')));
  expect(seen[_language], contains((ApiPaths.myLanguages, 'body')));
  expect(seen[utf8.decode(_image)], {(ApiPaths.myAvatar, 'body')});
  expect(seen[_memberId], {(memberPath, 'url'), (memberAvatarPath, 'url')});
  // The bearer travelled on each of the five new requests.
  for (final (method, path) in [
    ('GET', ApiPaths.myAvatar),
    ('PUT', ApiPaths.myAvatar),
    ('DELETE', ApiPaths.myAvatar),
    ('GET', memberPath),
    ('GET', memberAvatarPath),
  ]) {
    final sent = requests.where(
      (r) => r.method == method && r.url.path == path,
    );
    expect(sent, isNotEmpty, reason: '$method $path');
    for (final r in sent) {
      expect(r.headers['Authorization'], 'Bearer $_access2', reason: '$r');
    }
  }
  expect(seen[_targetId], {(blockPath, 'url'), (reportPath, 'url')});
  expect(seen[_details], {(reportPath, 'body')});
  // The bearer travelled on each of the four block and report requests.
  for (final (method, path) in [
    ('GET', ApiPaths.myBlocks),
    ('PUT', blockPath),
    ('DELETE', blockPath),
    ('PUT', reportPath),
  ]) {
    final sent = requests.where(
      (r) => r.method == method && r.url.path == path,
    );
    expect(sent, isNotEmpty, reason: '$method $path');
    for (final r in sent) {
      expect(r.headers['Authorization'], 'Bearer $_access2', reason: '$r');
    }
  }
  // The list, a block and an unblock have no body; a report's body is the
  // reason and the details as typed, and nothing else.
  for (final r in requests) {
    if (r.url.path == reportPath) {
      expect(jsonDecode(r.body), {'reason': 'harassment', 'details': _details});
    } else if (r.url.path.startsWith(ApiPaths.myBlocks)) {
      expect(r.bodyBytes, isEmpty, reason: '$r');
    }
  }
  // The upload's body is the photo and nothing else; nothing but the upload
  // has a byte body.
  for (final r in requests) {
    if (r.method == 'PUT' && r.url.path == ApiPaths.myAvatar) {
      expect(r.bodyBytes, _image);
    } else if (r.url.path == ApiPaths.myAvatar ||
        r.url.path.startsWith('/v1/profiles/')) {
      expect(r.bodyBytes, isEmpty, reason: '$r');
    }
  }
  expect(seen[_refresh1], contains((ApiPaths.refresh, 'body')));
  expect(seen[_refresh2], contains((ApiPaths.refresh, 'body')));
  expect(seen[_idToken], contains((ApiPaths.google, 'body')));
  expect(seen[_idToken2], contains((ApiPaths.google, 'body')));
  // The app never sends an ID token a second time by itself.
  for (final token in [_idToken, _idToken2]) {
    expect(
      requests.where((r) => r.body.contains('"$token"')),
      hasLength(1),
      reason: 'one request per Google answer',
    );
  }
}

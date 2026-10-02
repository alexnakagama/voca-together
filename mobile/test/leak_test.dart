import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/auth/auth_tokens.dart';
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

const _markers = ['LEAK', 'leak.email'];

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
  final manager = SessionManager(store: store, authApi: api, clock: clock);
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
  await record(() => manager.signInWithGoogle(idToken: _idToken));
  await record(() => manager.signIn(email: _email, password: _password));
  // Google sign-in succeeds.
  await record(() => manager.signInWithGoogle(idToken: _idToken));
  expect(manager.status, SessionStatus.signedIn);

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
  final allowed = <String, Set<_Place>>{
    _access1: {
      (ApiPaths.me, 'authorization'),
      (ApiPaths.logout, 'authorization'),
    },
    _access2: {
      (ApiPaths.me, 'authorization'),
      (ApiPaths.logout, 'authorization'),
    },
    _refresh1: {(ApiPaths.refresh, 'body')},
    _refresh2: {(ApiPaths.refresh, 'body')},
    _idToken: {(ApiPaths.google, 'body')},
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
      expect(path, anyOf(ApiPaths.me, ApiPaths.logout));
      expect(isAccessToken(auth.replaceFirst('Bearer ', '')), isTrue);
    }
  }

  // The flows above really exercised each allowed place.
  expect(seen[_access2], contains((ApiPaths.me, 'authorization')));
  expect(seen[_access1], contains((ApiPaths.logout, 'authorization')));
  expect(seen[_refresh1], contains((ApiPaths.refresh, 'body')));
  expect(seen[_refresh2], contains((ApiPaths.refresh, 'body')));
  expect(seen[_idToken], contains((ApiPaths.google, 'body')));
}

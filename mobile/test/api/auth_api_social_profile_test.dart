import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/api/auth_api.dart';

import '../support/fakes.dart';

Matcher _protocol(ProtocolFailure failure, int status) =>
    isA<ApiProtocolException>()
        .having((e) => e.failure, 'failure', failure)
        .having((e) => e.statusCode, 'statusCode', status);

Matcher _http(int status, String? code) => isA<ApiHttpException>()
    .having((e) => e.statusCode, 'status', status)
    .having((e) => e.code, 'code', code);

const _memberPath = '/v1/profiles/$testMemberId';
const _memberAvatarPath = '/v1/profiles/$testMemberId/avatar';

/// Not a JPEG: the transport never looks inside a picture.
final _stored = [0xff, 0xd8, 0xff, 0xe0, 0, 1, 2, 3];

void main() {
  late FakeServer server;
  late AuthApi api;
  final token = accessToken('1');

  setUp(() {
    server = FakeServer();
    api = authApiFor(server.client);
  });

  /// The one request sent, checked for what every call here has in common.
  http.Request sentOnce(String method, String path) {
    final request = server.requests.single;
    expect(request.method, method);
    expect(request.url.toString(), 'https://api.test.example$path');
    expect(request.headers['Authorization'], 'Bearer $token');
    return request;
  }

  group('paths', () {
    test('are the backend\'s', () {
      expect(ApiPaths.myAvatar, '/v1/me/avatar');
      expect(ApiPaths.memberProfile(testMemberId), _memberPath);
      expect(ApiPaths.memberAvatar(testMemberId), _memberAvatarPath);
    });

    test('a member path takes only a canonical identifier', () {
      for (final id in [
        '',
        'abc',
        'self',
        testMemberId.toUpperCase(),
        '{$testMemberId}',
        testMemberId.replaceAll('-', ''),
        testMemberId.substring(1),
        '${testMemberId}0',
        ' $testMemberId',
        '$testMemberId ',
        '$testMemberId\n',
        '$testMemberId/avatar',
        '../me/profile',
        testMemberId.replaceFirst('0', 'g'),
        testMemberId.replaceFirst('-', '_'),
      ]) {
        for (final path in [ApiPaths.memberProfile, ApiPaths.memberAvatar]) {
          expect(
            () => path(id),
            throwsA(
              isA<ArgumentError>().having(
                (e) => '$e',
                'toString',
                // The value is never echoed.
                id.isEmpty ? anything : isNot(contains(id)),
              ),
            ),
            reason: id,
          );
        }
      }
    });
  });

  group('avatar', () {
    test('GETs the member\'s own picture', () async {
      server.once('GET', ApiPaths.myAvatar, (_) => imageResponse(_stored));
      expect(await api.avatar(accessToken: token), _stored);
      final request = sentOnce('GET', '/v1/me/avatar');
      expect(request.bodyBytes, isEmpty);
    });

    test('no picture is null, for the backend\'s own 404 only', () async {
      server.once('GET', ApiPaths.myAvatar, (_) => noAvatar());
      expect(await api.avatar(accessToken: token), isNull);

      for (final (response, code) in [
        (http.Response('Not Found', 404), null),
        (errorResponse(404, 'not_found'), 'not_found'),
        (noProfile(), 'profile_not_found'),
        (errorResponse(410, 'avatar_not_found'), 'avatar_not_found'),
      ]) {
        server.once('GET', ApiPaths.myAvatar, (_) => response);
        await expectLater(
          api.avatar(accessToken: token),
          throwsA(_http(response.statusCode, code)),
        );
      }
    });

    test('an answer that is not the picture is a protocol failure', () async {
      server
        ..once('GET', ApiPaths.myAvatar, (_) => jsonResponse(200, {'a': 1}))
        ..once(
          'GET',
          ApiPaths.myAvatar,
          (_) => imageResponse(_stored, status: 203),
        )
        ..once('GET', ApiPaths.myAvatar, (_) => noContent());
      await expectLater(
        api.avatar(accessToken: token),
        throwsA(_protocol(ProtocolFailure.notImage, 200)),
      );
      await expectLater(
        api.avatar(accessToken: token),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 203)),
      );
      await expectLater(
        api.avatar(accessToken: token),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 204)),
      );
    });
  });

  group('saveAvatar', () {
    // Every byte value: what is chosen is what is sent.
    final upload = Uint8List.fromList([for (var i = 0; i < 256; i++) i]);

    test('PUTs exactly the bytes and returns the stored picture', () async {
      server.once('PUT', ApiPaths.myAvatar, (_) => imageResponse(_stored));
      final saved = await api.saveAvatar(accessToken: token, image: upload);
      expect(saved, _stored);
      final request = sentOnce('PUT', '/v1/me/avatar');
      expect(request.bodyBytes, upload);
      expect(request.headers['Content-Type'], 'application/octet-stream');
    });

    test('applies no rule of its own: an empty photo is sent', () async {
      server.once(
        'PUT',
        ApiPaths.myAvatar,
        (_) => jsonResponse(422, {
          'error': {
            'code': 'validation_failed',
            'fields': [
              {'field': 'avatar', 'code': 'required'},
            ],
          },
        }),
      );
      await expectLater(
        api.saveAvatar(accessToken: token, image: Uint8List(0)),
        throwsA(
          isA<ApiHttpException>()
              .having((e) => e.statusCode, 'status', 422)
              .having(
                (e) => e.fields.map((f) => '${f.field}:${f.code}'),
                'fields',
                ['avatar:required'],
              ),
        ),
      );
      expect(server.requests.single.bodyBytes, isEmpty);
    });

    test('a 404 is an error, whatever its code', () async {
      for (final response in [noAvatar(), noProfile()]) {
        server.once('PUT', ApiPaths.myAvatar, (_) => response);
        await expectLater(
          api.saveAvatar(accessToken: token, image: upload),
          throwsA(isA<ApiHttpException>()),
        );
      }
    });

    test('requires exactly 200 with the picture', () async {
      server
        ..once(
          'PUT',
          ApiPaths.myAvatar,
          (_) => imageResponse(_stored, status: 201),
        )
        ..once('PUT', ApiPaths.myAvatar, (_) => noContent())
        ..once('PUT', ApiPaths.myAvatar, (_) => jsonResponse(200, {}));
      await expectLater(
        api.saveAvatar(accessToken: token, image: upload),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 201)),
      );
      await expectLater(
        api.saveAvatar(accessToken: token, image: upload),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 204)),
      );
      await expectLater(
        api.saveAvatar(accessToken: token, image: upload),
        throwsA(_protocol(ProtocolFailure.notImage, 200)),
      );
    });
  });

  group('removeAvatar', () {
    test('DELETEs with the bearer and no body', () async {
      server.once('DELETE', ApiPaths.myAvatar, (_) => noContent());
      await api.removeAvatar(accessToken: token);
      final request = sentOnce('DELETE', '/v1/me/avatar');
      expect(request.bodyBytes, isEmpty);
      expect(request.headers.containsKey('Content-Type'), isFalse);
    });

    test('requires exactly 204', () async {
      server.once('DELETE', ApiPaths.myAvatar, (_) => jsonResponse(200, {}));
      await expectLater(
        api.removeAvatar(accessToken: token),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 200)),
      );
    });

    test('a 404 is an error, whatever its code', () async {
      server.once('DELETE', ApiPaths.myAvatar, (_) => noAvatar());
      await expectLater(
        api.removeAvatar(accessToken: token),
        throwsA(_http(404, 'avatar_not_found')),
      );
    });
  });

  group('memberProfile', () {
    test('GETs the profile the identifier names', () async {
      server.once(
        'GET',
        _memberPath,
        (_) => jsonResponse(
          200,
          memberProfileBody(
            displayName: 'Ana',
            bio: 'Hi',
            hasAvatar: true,
            languages: languagesBody(spoken: [('es', 'native')]),
          ),
        ),
      );
      final profile = await api.memberProfile(
        accessToken: token,
        id: testMemberId,
      );
      expect(profile!.id, testMemberId);
      expect(profile.displayName, 'Ana');
      expect(profile.bio, 'Hi');
      expect(profile.hasAvatar, isTrue);
      expect(profile.languages.spoken.single.code, 'es');
      final request = sentOnce('GET', _memberPath);
      expect(request.bodyBytes, isEmpty);
    });

    test('no such profile is null, for the backend\'s own 404 only', () async {
      server.once('GET', _memberPath, (_) => noProfile());
      expect(
        await api.memberProfile(accessToken: token, id: testMemberId),
        isNull,
      );

      for (final (response, code) in [
        (http.Response('Not Found', 404), null),
        (errorResponse(404, 'not_found'), 'not_found'),
        (noAvatar(), 'avatar_not_found'),
        (errorResponse(410, 'profile_not_found'), 'profile_not_found'),
      ]) {
        server.once('GET', _memberPath, (_) => response);
        await expectLater(
          api.memberProfile(accessToken: token, id: testMemberId),
          throwsA(_http(response.statusCode, code)),
        );
      }
    });

    test('requires exactly 200 and a well-formed profile', () async {
      server
        ..once(
          'GET',
          _memberPath,
          (_) => jsonResponse(203, memberProfileBody()),
        )
        ..once(
          'GET',
          _memberPath,
          (_) => jsonResponse(200, memberProfileBody()..remove('languages')),
        )
        // The owner's own response is not a member profile.
        ..once('GET', _memberPath, (_) => jsonResponse(200, profileBody()));
      await expectLater(
        api.memberProfile(accessToken: token, id: testMemberId),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 203)),
      );
      for (var i = 0; i < 2; i++) {
        await expectLater(
          api.memberProfile(accessToken: token, id: testMemberId),
          throwsA(_protocol(ProtocolFailure.malformedBody, 200)),
        );
      }
    });
  });

  group('memberAvatar', () {
    test('GETs the picture of the member the identifier names', () async {
      server.once('GET', _memberAvatarPath, (_) => imageResponse(_stored));
      expect(
        await api.memberAvatar(accessToken: token, id: testMemberId),
        _stored,
      );
      final request = sentOnce('GET', _memberAvatarPath);
      expect(request.bodyBytes, isEmpty);
    });

    test('no picture is null, for 404 avatar_not_found only', () async {
      server.once('GET', _memberAvatarPath, (_) => noAvatar());
      expect(
        await api.memberAvatar(accessToken: token, id: testMemberId),
        isNull,
      );

      // "No such profile" is the profile call's answer, not "no picture".
      for (final (response, code) in [
        (noProfile(), 'profile_not_found'),
        (http.Response('Not Found', 404), null),
        (errorResponse(404, 'not_found'), 'not_found'),
        (errorResponse(410, 'avatar_not_found'), 'avatar_not_found'),
      ]) {
        server.once('GET', _memberAvatarPath, (_) => response);
        await expectLater(
          api.memberAvatar(accessToken: token, id: testMemberId),
          throwsA(_http(response.statusCode, code)),
        );
      }
    });
  });

  group('every call', () {
    test('refuses a non-canonical member id before any request', () async {
      for (final id in [
        '',
        'abc',
        testMemberId.toUpperCase(),
        testMemberId.replaceAll('-', ''),
        ' $testMemberId',
        '$testMemberId/avatar',
      ]) {
        await expectLater(
          api.memberProfile(accessToken: token, id: id),
          throwsArgumentError,
          reason: id,
        );
        await expectLater(
          api.memberAvatar(accessToken: token, id: id),
          throwsArgumentError,
          reason: id,
        );
      }
      expect(server.requests, isEmpty);
    });

    test('surfaces the server\'s failures unchanged, sent once', () async {
      final calls = <(String, String, Future<Object?> Function())>[
        ('GET', ApiPaths.myAvatar, () => api.avatar(accessToken: token)),
        (
          'PUT',
          ApiPaths.myAvatar,
          () => api.saveAvatar(accessToken: token, image: Uint8List(4)),
        ),
        (
          'DELETE',
          ApiPaths.myAvatar,
          () => api.removeAvatar(accessToken: token),
        ),
        (
          'GET',
          _memberPath,
          () => api.memberProfile(accessToken: token, id: testMemberId),
        ),
        (
          'GET',
          _memberAvatarPath,
          () => api.memberAvatar(accessToken: token, id: testMemberId),
        ),
      ];
      for (final (method, path, call) in calls) {
        final before = server.requests.length;
        server
          ..once(
            method,
            path,
            (_) => errorResponse(
              429,
              'rate_limited',
              headers: {'retry-after': '6'},
            ),
          )
          ..once(
            method,
            path,
            (_) => errorResponse(
              401,
              'invalid_access_token',
              headers: {'www-authenticate': 'Bearer'},
            ),
          )
          ..once(method, path, networkFailure);
        await expectLater(
          call(),
          throwsA(
            isA<ApiHttpException>()
                .having((e) => e.code, 'code', 'rate_limited')
                .having(
                  (e) => e.retryAfter,
                  'retryAfter',
                  const Duration(seconds: 6),
                ),
          ),
          reason: '$method $path',
        );
        await expectLater(call(), throwsA(_http(401, 'invalid_access_token')));
        await expectLater(call(), throwsA(isA<ApiNetworkException>()));
        expect(server.requests.length, before + 3);
      }
    });

    test('refuses a refresh token as bearer', () {
      final refresh = refreshToken('1');
      expect(() => api.avatar(accessToken: refresh), throwsArgumentError);
      expect(
        () => api.saveAvatar(accessToken: refresh, image: Uint8List(1)),
        throwsArgumentError,
      );
      expect(() => api.removeAvatar(accessToken: refresh), throwsArgumentError);
      expect(
        () => api.memberProfile(accessToken: refresh, id: testMemberId),
        throwsArgumentError,
      );
      expect(
        () => api.memberAvatar(accessToken: refresh, id: testMemberId),
        throwsArgumentError,
      );
      expect(server.requests, isEmpty);
    });
  });
}

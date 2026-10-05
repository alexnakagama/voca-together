import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/api/auth_api.dart';

import '../support/fakes.dart';

Object? _body(http.Request r) =>
    r.bodyBytes.isEmpty ? null : jsonDecode(utf8.decode(r.bodyBytes));

Matcher _protocol(ProtocolFailure failure, int status) =>
    isA<ApiProtocolException>()
        .having((e) => e.failure, 'failure', failure)
        .having((e) => e.statusCode, 'statusCode', status);

void main() {
  late FakeServer server;
  late AuthApi api;

  setUp(() {
    server = FakeServer();
    api = authApiFor(server.client);
  });

  group('token operations', () {
    final ops = <String, (String, Future<Object> Function(AuthApi), Object)>{
      'login': (
        ApiPaths.login,
        (a) => a.login(email: 'a@b.c', password: 'pw'),
        {'email': 'a@b.c', 'password': 'pw'},
      ),
      'google': (
        ApiPaths.google,
        (a) => a.google(idToken: 'header.payload.sig'),
        {'id_token': 'header.payload.sig'},
      ),
      'refresh': (
        ApiPaths.refresh,
        (a) => a.refresh(refreshToken: refreshToken('1')),
        {'refresh_token': refreshToken('1')},
      ),
    };

    ops.forEach((name, op) {
      final (path, call, body) = op;

      test('$name posts its body and returns tokens', () async {
        server.once(
          'POST',
          path,
          (_) =>
              jsonResponse(200, tokenBody(accessToken('2'), refreshToken('2'))),
        );
        final tokens = await call(api);
        final r = server.requests.single;
        expect(r.url.path, path);
        expect(_body(r), body);
        expect(r.headers.containsKey('Authorization'), isFalse);
        expect(tokens, isA<Object>());
      });

      test('$name rejects malformed token responses', () async {
        for (final b in [
          {
            ...tokenBody(accessToken('2'), refreshToken('2')),
            'token_type': 'x',
          },
          {...tokenBody(accessToken('2'), refreshToken('2')), 'expires_in': 0},
          {
            ...tokenBody(accessToken('2'), refreshToken('2')),
            'expires_in': 'x',
          },
          tokenBody(refreshToken('2'), accessToken('2')),
          tokenBody('vt_at_x', refreshToken('2')),
          <String, Object?>{},
        ]) {
          server.once('POST', path, (_) => jsonResponse(200, b));
          await expectLater(
            call(api),
            throwsA(_protocol(ProtocolFailure.malformedBody, 200)),
            reason: '$b',
          );
        }
      });

      test('$name requires exactly 200', () async {
        server.once(
          'POST',
          path,
          (_) =>
              jsonResponse(201, tokenBody(accessToken('2'), refreshToken('2'))),
        );
        await expectLater(
          call(api),
          throwsA(_protocol(ProtocolFailure.unexpectedStatus, 201)),
        );
      });
    });

    test('login surfaces invalid_credentials', () async {
      server.once(
        'POST',
        ApiPaths.login,
        (_) => errorResponse(401, 'invalid_credentials'),
      );
      await expectLater(
        api.login(email: 'a@b.c', password: 'pw'),
        throwsA(
          isA<ApiHttpException>().having(
            (e) => e.code,
            'code',
            'invalid_credentials',
          ),
        ),
      );
    });

    test('refresh refuses anything but a refresh token before sending', () {
      for (final bad in [accessToken('1'), '', 'vt_rt_short']) {
        expect(() => api.refresh(refreshToken: bad), throwsArgumentError);
      }
      expect(server.requests, isEmpty);
    });

    test('google refuses an empty or oversized ID token before sending', () {
      expect(() => api.google(idToken: ''), throwsArgumentError);
      expect(
        () => api.google(idToken: 'a' * (AuthApi.maxIdTokenBytes + 1)),
        throwsArgumentError,
      );
      expect(server.requests, isEmpty);
    });

    test('google accepts a 4096-byte ID token', () async {
      server.once(
        'POST',
        ApiPaths.google,
        (_) =>
            jsonResponse(200, tokenBody(accessToken('2'), refreshToken('2'))),
      );
      await api.google(idToken: 'a' * AuthApi.maxIdTokenBytes);
    });
  });

  group('logout', () {
    test('sends only the bearer token, no body', () async {
      server.once('POST', ApiPaths.logout, (_) => noContent());
      await api.logout(accessToken: accessToken('1'));
      final r = server.requests.single;
      expect(r.headers['Authorization'], 'Bearer ${accessToken('1')}');
      expect(r.bodyBytes, isEmpty);
      expect(r.headers.containsKey('Content-Type'), isFalse);
    });

    test('requires exactly 204', () async {
      server.once(
        'POST',
        ApiPaths.logout,
        (_) => jsonResponse(200, <String, Object?>{}),
      );
      await expectLater(
        api.logout(accessToken: accessToken('1')),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 200)),
      );
    });

    test('refuses a refresh token as bearer', () {
      expect(
        () => api.logout(accessToken: refreshToken('1')),
        throwsArgumentError,
      );
    });
  });

  group('me', () {
    final meBody = {
      'id': '0190c3b2-0000-7000-8000-000000000000',
      'email': 'a@b.c',
      'email_verified_at': '2026-10-01T10:00:00.123456Z',
      'created_at': '2026-09-30T08:00:00+02:00',
    };

    test('parses the user', () async {
      server.once('GET', ApiPaths.me, (_) => jsonResponse(200, meBody));
      final me = await api.me(accessToken: accessToken('1'));
      expect(me.id, meBody['id']);
      expect(me.email, 'a@b.c');
      expect(me.emailVerifiedAt, DateTime.utc(2026, 10, 1, 10, 0, 0, 123, 456));
      expect(me.createdAt, DateTime.utc(2026, 9, 30, 6));
      expect(
        server.requests.single.headers['Authorization'],
        'Bearer ${accessToken('1')}',
      );
      expect('$me', isNot(contains('a@b.c')));
    });

    test('rejects a malformed user', () async {
      for (final b in [
        {...meBody}..remove('id'),
        {...meBody, 'id': ''},
        {...meBody, 'email': null},
        {...meBody, 'email_verified_at': null},
        {...meBody, 'created_at': 'yesterday'},
        {...meBody, 'created_at': 5},
      ]) {
        server.once('GET', ApiPaths.me, (_) => jsonResponse(200, b));
        await expectLater(
          api.me(accessToken: accessToken('1')),
          throwsA(_protocol(ProtocolFailure.malformedBody, 200)),
        );
      }
    });

    test('surfaces invalid_access_token', () async {
      server.once(
        'GET',
        ApiPaths.me,
        (_) => errorResponse(
          401,
          'invalid_access_token',
          headers: {'www-authenticate': 'Bearer'},
        ),
      );
      await expectLater(
        api.me(accessToken: accessToken('1')),
        throwsA(
          isA<ApiHttpException>()
              .having((e) => e.statusCode, 'status', 401)
              .having((e) => e.code, 'code', 'invalid_access_token'),
        ),
      );
    });
  });

  group('profile', () {
    final body = {
      'display_name': 'Ana López',
      'bio': 'Learning Japanese.',
      'created_at': '2026-10-01T10:00:00.123456Z',
      'updated_at': '2026-10-02T08:00:00+02:00',
    };

    test('parses the profile', () async {
      server.once('GET', ApiPaths.profile, (_) => jsonResponse(200, body));
      final profile = await api.profile(accessToken: accessToken('1'));
      expect(profile!.displayName, 'Ana López');
      expect(profile.bio, 'Learning Japanese.');
      expect(profile.createdAt, DateTime.utc(2026, 10, 1, 10, 0, 0, 123, 456));
      expect(profile.updatedAt, DateTime.utc(2026, 10, 2, 6));
      final request = server.requests.single;
      expect(request.headers['Authorization'], 'Bearer ${accessToken('1')}');
      expect(request.bodyBytes, isEmpty);
      // What a member wrote is personal data: never in a string.
      expect('$profile', isNot(contains('Ana')));
      expect('$profile', isNot(contains('Japanese')));
    });

    test('an empty bio is a profile', () async {
      server.once(
        'GET',
        ApiPaths.profile,
        (_) => jsonResponse(200, {...body, 'bio': ''}),
      );
      final profile = await api.profile(accessToken: accessToken('1'));
      expect(profile!.bio, '');
    });

    test('no profile yet is null, for the backend\'s own 404 only', () async {
      server.once('GET', ApiPaths.profile, (_) => noProfile());
      expect(await api.profile(accessToken: accessToken('1')), isNull);

      // A 404 that isn't the backend saying "none saved" (a proxy's page, an
      // older backend without the route) is an error, not an empty profile.
      for (final response in [
        http.Response('Not Found', 404),
        errorResponse(404, 'not_found'),
        errorResponse(410, 'profile_not_found'),
      ]) {
        server.once('GET', ApiPaths.profile, (_) => response);
        await expectLater(
          api.profile(accessToken: accessToken('1')),
          throwsA(isA<ApiHttpException>()),
        );
      }
    });

    test('rejects a malformed profile', () async {
      for (final b in [
        {...body}..remove('display_name'),
        {...body}..remove('bio'),
        {...body, 'display_name': ''},
        {...body, 'display_name': 5},
        {...body, 'bio': null},
        {...body, 'created_at': 'yesterday'},
        {...body, 'updated_at': null},
        <Object?>[body],
      ]) {
        server
          ..once('GET', ApiPaths.profile, (_) => jsonResponse(200, b))
          ..once('PUT', ApiPaths.profile, (_) => jsonResponse(200, b));
        await expectLater(
          api.profile(accessToken: accessToken('1')),
          throwsA(_protocol(ProtocolFailure.malformedBody, 200)),
        );
        await expectLater(
          api.saveProfile(
            accessToken: accessToken('1'),
            displayName: 'Ana',
            bio: '',
          ),
          throwsA(_protocol(ProtocolFailure.malformedBody, 200)),
        );
      }
    });

    test('saveProfile PUTs exactly the two fields, as typed', () async {
      server.once('PUT', ApiPaths.profile, (_) => jsonResponse(200, body));
      final saved = await api.saveProfile(
        accessToken: accessToken('1'),
        displayName: '  Ana   López ',
        bio: 'Learning Japanese.\n',
      );
      // The server's normalized text is what comes back.
      expect(saved.displayName, 'Ana López');
      final request = server.requests.single;
      expect(request.method, 'PUT');
      expect(request.headers['Authorization'], 'Bearer ${accessToken('1')}');
      expect(_body(request), {
        'display_name': '  Ana   López ',
        'bio': 'Learning Japanese.\n',
      });
    });

    test('both calls require exactly 200', () async {
      server
        ..once('GET', ApiPaths.profile, (_) => jsonResponse(203, body))
        ..once('PUT', ApiPaths.profile, (_) => jsonResponse(201, body));
      await expectLater(
        api.profile(accessToken: accessToken('1')),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 203)),
      );
      await expectLater(
        api.saveProfile(
          accessToken: accessToken('1'),
          displayName: 'Ana',
          bio: '',
        ),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 201)),
      );
    });

    test('saveProfile surfaces validation errors with their fields', () async {
      server.once(
        'PUT',
        ApiPaths.profile,
        (_) => jsonResponse(422, {
          'error': {
            'code': 'validation_failed',
            'fields': [
              {'field': 'display_name', 'code': 'too_long'},
              {'field': 'bio', 'code': 'invalid'},
            ],
          },
        }),
      );
      await expectLater(
        api.saveProfile(
          accessToken: accessToken('1'),
          displayName: 'x' * 51,
          bio: 'y',
        ),
        throwsA(
          isA<ApiHttpException>()
              .having((e) => e.code, 'code', 'validation_failed')
              .having(
                (e) => e.fields.map((f) => '${f.field}:${f.code}'),
                'fields',
                ['display_name:too_long', 'bio:invalid'],
              ),
        ),
      );
    });

    test('refuses a refresh token as bearer', () {
      expect(
        () => api.profile(accessToken: refreshToken('1')),
        throwsArgumentError,
      );
      expect(
        () => api.saveProfile(
          accessToken: refreshToken('1'),
          displayName: 'Ana',
          bio: '',
        ),
        throwsArgumentError,
      );
      expect(server.requests, isEmpty);
    });
  });

  group('healthz', () {
    test('sends nothing but the probe', () async {
      server.once('GET', ApiPaths.healthz, (_) => healthy());
      await api.healthz();
      final r = server.requests.single;
      expect(r.method, 'GET');
      expect(r.bodyBytes, isEmpty);
      expect(r.headers.containsKey('Authorization'), isFalse);
    });

    test('fails on a proxy error', () async {
      server.once(
        'GET',
        ApiPaths.healthz,
        (_) => http.Response('bad gateway', 502),
      );
      await expectLater(api.healthz(), throwsA(isA<ApiHttpException>()));
    });
  });
}

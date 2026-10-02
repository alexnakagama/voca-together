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

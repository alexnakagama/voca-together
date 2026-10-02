import 'dart:async';
import 'dart:convert';

import 'package:fake_async/fake_async.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/me.dart';
import 'package:vocatogether/session.dart';

import 'support/fakes.dart';

http.Response _me() => jsonResponse(200, {
  'id': 'u1',
  'email': 'a@b.c',
  'email_verified_at': '2026-10-01T00:00:00Z',
  'created_at': '2026-10-01T00:00:00Z',
});

http.Response _unauthorized() => errorResponse(
  401,
  'invalid_access_token',
  headers: {'www-authenticate': 'Bearer'},
);

http.Response _rotated(String marker) =>
    jsonResponse(200, tokenBody(accessToken(marker), refreshToken(marker)));

String? _bearer(http.Request r) => r.headers['Authorization'];

void main() {
  late FakeServer server;
  late InMemoryTokenStore store;
  late FakeAuthClock clock;
  late SessionManager manager;

  /// The access token `/v1/me` accepts; any other gets a 401.
  late String valid;

  Future<Me> callMe() => manager.me();

  /// The refresh tokens sent, in order.
  List<String> refreshTokensSent() => server
      .to(ApiPaths.refresh)
      .map(
        (r) =>
            (jsonDecode(r.body) as Map<String, Object?>)['refresh_token']
                as String,
      )
      .toList();

  setUp(() async {
    server = FakeServer();
    store = InMemoryTokenStore();
    clock = FakeAuthClock();
    valid = accessToken('1');
    server
      ..always('GET', ApiPaths.healthz, (_) => healthy())
      ..always(
        'GET',
        ApiPaths.me,
        (r) => _bearer(r) == 'Bearer $valid' ? _me() : _unauthorized(),
      );
    manager = await signedInManager(server, store, clock);
  });

  tearDown(() => manager.dispose());

  /// Answers the next refresh with tokens for [marker], which `/v1/me` then
  /// accepts.
  void refreshTo(String marker, {Future<void>? after}) {
    server.once('POST', ApiPaths.refresh, (_) async {
      await after;
      valid = accessToken(marker);
      return _rotated(marker);
    });
  }

  group('expiry', () {
    test('a fresh token is used without refreshing', () async {
      await callMe();
      expect(server.requests.map((r) => r.url.path), [ApiPaths.me]);
    });

    test('a token within the margin is refreshed first', () async {
      // Stored with 10 minutes left; the margin is 30 s.
      clock.advance(const Duration(minutes: 9, seconds: 29));
      await callMe();
      expect(server.count(ApiPaths.refresh), 0);

      clock.advance(const Duration(seconds: 1));
      refreshTo('2');
      await callMe();
      expect(server.requests.skip(1).map((r) => r.url.path), [
        ApiPaths.healthz,
        ApiPaths.refresh,
        ApiPaths.me,
      ]);
      expect(_bearer(server.requests.last), 'Bearer ${accessToken('2')}');
      expect(store.session!.accessToken, accessToken('2'));
      expect(store.session!.refreshToken, refreshToken('2'));
    });

    test('the new token counts from when the refresh was sent', () async {
      clock.advance(const Duration(minutes: 10));
      refreshTo('2');
      await callMe();
      // Fresh for 15 min minus the margin, by either clock.
      clock.advance(const Duration(minutes: 14, seconds: 29));
      await callMe();
      expect(server.count(ApiPaths.refresh), 1);
      clock.advance(const Duration(seconds: 1));
      refreshTo('3');
      await callMe();
      expect(server.count(ApiPaths.refresh), 2);
    });

    test('a wall clock jumping forward refreshes early', () async {
      clock.wall = clock.wall.add(const Duration(hours: 1));
      refreshTo('2');
      await callMe();
      expect(server.count(ApiPaths.refresh), 1);
    });

    test('a wall clock moved back does not extend the token', () async {
      clock.wall = clock.wall.subtract(const Duration(hours: 1));
      await callMe();
      expect(server.count(ApiPaths.refresh), 0);

      clock.monotonic += const Duration(minutes: 9, seconds: 30);
      refreshTo('2');
      await callMe();
      expect(server.count(ApiPaths.refresh), 1);
    });

    test('a stored token past its expiry is refreshed on first use', () async {
      manager.dispose();
      store.raw = storedRaw(clock, '1', remaining: const Duration(seconds: -1));
      manager = await signedInManager(server, store, clock);
      refreshTo('2');
      await callMe();
      expect(server.count(ApiPaths.refresh), 1);
      expect(server.count(ApiPaths.me), 1);
    });

    test(
      'a stored expiry beyond its lifetime (clock moved back) is stale',
      () async {
        manager.dispose();
        store.raw = storedRaw(
          clock,
          '1',
          remaining: const Duration(minutes: 16),
        );
        manager = await signedInManager(server, store, clock);
        refreshTo('2');
        await callMe();
        expect(server.count(ApiPaths.refresh), 1);
      },
    );
  });

  group('401 handling', () {
    test('concurrent 401s share exactly one refresh', () async {
      final release = Completer<void>();
      valid = 'none yet';
      refreshTo('2', after: release.future);

      final calls = List.generate(5, (_) => callMe());
      await settle();
      expect(server.count(ApiPaths.me), 5);
      expect(server.count(ApiPaths.healthz), 1);
      expect(server.count(ApiPaths.refresh), 1);

      release.complete();
      await Future.wait(calls);
      expect(server.count(ApiPaths.healthz), 1);
      expect(server.count(ApiPaths.refresh), 1);
      expect(refreshTokensSent(), [refreshToken('1')]);
      final retries = server.to(ApiPaths.me).skip(5).map(_bearer);
      expect(retries, List.filled(5, 'Bearer ${accessToken('2')}'));
    });

    test(
      'tokens changed in flight: retry with the new token, no refresh',
      () async {
        final releaseA = Completer<void>();
        // A's request: held, then refused (its token was rotated meanwhile).
        server.once('GET', ApiPaths.me, (_) async {
          await releaseA.future;
          return _unauthorized();
        });
        // B's request: refused at once, so B refreshes.
        server.once('GET', ApiPaths.me, (_) => _unauthorized());
        refreshTo('2');

        final a = callMe();
        await settle();
        await callMe(); // B
        expect(server.count(ApiPaths.refresh), 1);

        releaseA.complete();
        await a;
        expect(server.count(ApiPaths.refresh), 1);
        expect(
          _bearer(server.to(ApiPaths.me).last),
          'Bearer ${accessToken('2')}',
        );
      },
    );

    test('a second 401 is rethrown without looping', () async {
      valid = 'never';
      // The refresh succeeds, but the server refuses its access token too.
      server.once('POST', ApiPaths.refresh, (_) => _rotated('2'));
      await expectLater(
        callMe(),
        throwsA(isA<ApiHttpException>().having((e) => e.statusCode, 's', 401)),
      );
      // Sent, refreshed, resent once.
      expect(server.count(ApiPaths.me), 2);
      expect(server.count(ApiPaths.refresh), 1);
      expect(manager.status, SessionStatus.signedIn);

      // The refused token is now stale: the next call refreshes first, and a
      // 401 for a token fresh from that refresh is again final.
      server.once('POST', ApiPaths.refresh, (_) => _rotated('3'));
      await expectLater(callMe(), throwsA(isA<ApiHttpException>()));
      expect(server.count(ApiPaths.refresh), 2);
      expect(server.count(ApiPaths.me), 3);
    });

    test('a 401 once the session ended is SignedOutException', () async {
      final release = Completer<void>();
      server.once('GET', ApiPaths.me, (_) async {
        await release.future;
        return _unauthorized();
      });
      server.once('POST', ApiPaths.logout, (_) => noContent());
      final call = callMe();
      await settle();
      await manager.logout();
      release.complete();
      await expectLater(call, throwsA(isA<SignedOutException>()));
      expect(server.count(ApiPaths.refresh), 0);
    });
  });

  group('refresh outcomes', () {
    test('401 ends the session for every waiter', () async {
      valid = 'never';
      final release = Completer<void>();
      server.once('POST', ApiPaths.refresh, (_) async {
        await release.future;
        return errorResponse(401, 'invalid_refresh_token');
      });
      final notified = <SessionStatus>[];
      manager.addListener(() => notified.add(manager.status));

      final calls = List.generate(
        3,
        (_) => expectLater(callMe(), throwsA(isA<SignedOutException>())),
      );
      await settle();
      release.complete();
      await Future.wait(calls);

      expect(manager.status, SessionStatus.signedOut);
      expect(notified, [SessionStatus.signedOut]);
      expect(store.raw, isNull);
      expect(server.count(ApiPaths.refresh), 1);
      await expectLater(callMe(), throwsA(isA<SignedOutException>()));
    });

    test('429 keeps the session and waits for Retry-After', () async {
      valid = 'rotated elsewhere';
      server.once(
        'POST',
        ApiPaths.refresh,
        (_) =>
            errorResponse(429, 'rate_limited', headers: {'retry-after': '30'}),
      );
      await expectLater(
        callMe(),
        throwsA(
          isA<ApiHttpException>()
              .having((e) => e.statusCode, 'status', 429)
              .having(
                (e) => e.retryAfter,
                'retryAfter',
                const Duration(seconds: 30),
              ),
        ),
      );
      expect(manager.status, SessionStatus.signedIn);
      expect(store.session!.refreshToken, refreshToken('1'));

      // Before Retry-After: no probe and no refresh are sent.
      clock.advance(const Duration(seconds: 20));
      await expectLater(
        callMe(),
        throwsA(
          isA<ApiHttpException>()
              .having((e) => e.statusCode, 'status', 429)
              .having(
                (e) => e.retryAfter,
                'retryAfter',
                const Duration(seconds: 10),
              ),
        ),
      );
      expect(server.count(ApiPaths.healthz), 1);
      expect(server.count(ApiPaths.refresh), 1);

      // After it: the same refresh token is sent again (018).
      clock.advance(const Duration(seconds: 10));
      refreshTo('2');
      await callMe();
      expect(refreshTokensSent(), [refreshToken('1'), refreshToken('1')]);
      expect(store.session!.refreshToken, refreshToken('2'));
    });

    test(
      '429 blocks proactive refresh but still tries the stale token',
      () async {
        clock.advance(const Duration(minutes: 10));
        server.once(
          'POST',
          ApiPaths.refresh,
          (_) => errorResponse(
            429,
            'rate_limited',
            headers: {'retry-after': '60'},
          ),
        );
        await expectLater(callMe(), throwsA(isA<ApiHttpException>()));
        expect(server.count(ApiPaths.me), 0);

        // The server may still accept it; refresh stays blocked.
        await callMe();
        expect(server.count(ApiPaths.refresh), 1);
        expect(
          _bearer(server.to(ApiPaths.me).single),
          'Bearer ${accessToken('1')}',
        );
      },
    );

    test('429 without Retry-After waits the default', () async {
      valid = 'never';
      server.once(
        'POST',
        ApiPaths.refresh,
        (_) => errorResponse(429, 'rate_limited'),
      );
      await expectLater(callMe(), throwsA(isA<ApiHttpException>()));
      clock.advance(const Duration(seconds: 4));
      await expectLater(
        callMe(),
        throwsA(
          isA<ApiHttpException>().having((e) => e.statusCode, 'status', 429),
        ),
      );
      expect(server.count(ApiPaths.refresh), 1);
      clock.advance(const Duration(seconds: 1));
      refreshTo('2');
      await callMe();
      expect(server.count(ApiPaths.refresh), 2);
    });

    final ending = <String, Responder>{
      '500': (_) => errorResponse(500, 'internal_error'),
      '502 page': (_) => http.Response('bad gateway', 502),
      '503': (_) => errorResponse(
        503,
        'service_unavailable',
        headers: {'retry-after': '5'},
      ),
      '400': (_) => errorResponse(400, 'invalid_request'),
      '403': (_) => errorResponse(403, 'forbidden'),
      '404': (_) => http.Response('404 page not found', 404),
      '409': (_) => errorResponse(409, 'account_exists'),
      '422': (_) => jsonResponse(422, {
        'error': {
          'code': 'validation_failed',
          'fields': [
            {'field': 'refresh_token', 'code': 'required'},
          ],
        },
      }),
      'network error': (_) => throw http.ClientException('connection reset'),
      'timeout': (r) => throw http.RequestAbortedException(r.url),
      'malformed 200': (_) => jsonResponse(200, {'access_token': 'x'}),
      '200 with a non-JSON body': (_) => http.Response('ok', 200),
      '201': (_) =>
          jsonResponse(201, tokenBody(accessToken('2'), refreshToken('2'))),
      '204': (_) => noContent(),
      '302': (_) => http.Response('', 302, headers: {'location': '/x'}),
    };

    ending.forEach((name, responder) {
      test('$name ends the session; the refresh is never resent', () async {
        valid = 'never';
        server.once('POST', ApiPaths.refresh, responder);
        await expectLater(callMe(), throwsA(isA<SignedOutException>()));
        expect(manager.status, SessionStatus.signedOut);
        expect(store.raw, isNull);
        expect(server.count(ApiPaths.refresh), 1);
        expect(server.count(ApiPaths.healthz), 1);
      });
    });

    test('a real timeout ends the session', () {
      fakeAsync((async) {
        valid = 'never';
        server.once(
          'POST',
          ApiPaths.refresh,
          (_) => Completer<http.Response>().future,
        );
        Object? error;
        callMe().catchError((Object e) {
          error = e;
          return _meValue;
        });
        async.elapse(const Duration(seconds: 14));
        expect(error, isNull);
        expect(manager.status, SessionStatus.signedIn);
        async.elapse(const Duration(seconds: 2));
        expect(error, isA<SignedOutException>());
        expect(manager.status, SessionStatus.signedOut);
        expect(store.raw, isNull);
      });
    });

    test('a store write failure keeps the new tokens in memory only', () async {
      valid = 'never';
      store.writeError = Exception('disk full');
      refreshTo('2');
      await callMe();
      expect(manager.status, SessionStatus.signedIn);
      // The rotated-out refresh token isn't left on disk.
      expect(store.raw, isNull);
      await callMe();
      expect(server.count(ApiPaths.refresh), 1);
      expect(
        _bearer(server.to(ApiPaths.me).last),
        'Bearer ${accessToken('2')}',
      );
    });
  });

  group('reachability probe', () {
    final failures = <String, (Responder, Matcher)>{
      'network error': (
        (_) => throw http.ClientException('no route'),
        isA<ApiNetworkException>(),
      ),
      'timeout': (
        (r) => throw http.RequestAbortedException(r.url),
        isA<ApiTimeoutException>(),
      ),
      '502': (
        (_) => http.Response('bad gateway', 502),
        isA<ApiHttpException>().having((e) => e.statusCode, 's', 502),
      ),
      '503': (
        (_) => errorResponse(503, 'service_unavailable'),
        isA<ApiHttpException>().having((e) => e.statusCode, 's', 503),
      ),
      'redirect': (
        (_) => http.Response('', 302, headers: {'location': '/'}),
        isA<ApiProtocolException>(),
      ),
    };

    failures.forEach((name, failure) {
      final (responder, matcher) = failure;
      test('$name: no refresh is sent and the session stays', () async {
        valid = 'never';
        server.once('GET', ApiPaths.healthz, responder);
        await expectLater(callMe(), throwsA(matcher));
        expect(server.count(ApiPaths.refresh), 0);
        expect(manager.status, SessionStatus.signedIn);
        expect(store.session!.refreshToken, refreshToken('1'));

        // Once reachable, the next call refreshes with the same token.
        refreshTo('2');
        await callMe();
        expect(refreshTokensSent(), [refreshToken('1')]);
      });
    });

    test('a slow probe times out after 5 s without refreshing', () {
      fakeAsync((async) {
        valid = 'never';
        server.once(
          'GET',
          ApiPaths.healthz,
          (_) => Completer<http.Response>().future,
        );
        Object? error;
        callMe().catchError((Object e) {
          error = e;
          return _meValue;
        });
        async.elapse(const Duration(seconds: 6));
        expect(error, isA<ApiTimeoutException>());
        expect(server.count(ApiPaths.refresh), 0);
        expect(manager.status, SessionStatus.signedIn);
      });
    });
  });
}

final _meValue = Me(
  id: 'x',
  email: 'x',
  emailVerifiedAt: DateTime.utc(2026),
  createdAt: DateTime.utc(2026),
);

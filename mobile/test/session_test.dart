import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/auth/token_store.dart';
import 'package:vocatogether/session.dart';

import 'support/fakes.dart';

void main() {
  late FakeServer server;
  late InMemoryTokenStore store;
  late FakeAuthClock clock;
  late SessionManager manager;
  late List<SessionStatus> notified;

  SessionManager build() {
    final m = SessionManager(
      store: store,
      authApi: authApiFor(server.client),
      clock: clock,
    );
    notified = [];
    m.addListener(() => notified.add(m.status));
    return m;
  }

  setUp(() {
    server = FakeServer();
    store = InMemoryTokenStore();
    clock = FakeAuthClock();
    manager = build();
  });

  tearDown(() {
    // Some tests dispose on purpose.
    try {
      manager.dispose();
    } on FlutterError {
      // Already disposed.
    }
  });

  group('restore', () {
    test('starts unknown', () {
      expect(manager.status, SessionStatus.unknown);
    });

    test('an empty store starts signed out', () async {
      await manager.restore();
      expect(manager.status, SessionStatus.signedOut);
      expect(notified, [SessionStatus.signedOut]);
      expect(server.requests, isEmpty);
    });

    test('a stored session starts signed in without any request', () async {
      store.raw = storedRaw(clock, '1');
      await manager.restore();
      expect(manager.status, SessionStatus.signedIn);
      expect(notified, [SessionStatus.signedIn]);
      expect(server.requests, isEmpty);
    });

    test('corrupt data is cleared and starts signed out', () async {
      store.raw = '{"v":1,"at":"x"}';
      await manager.restore();
      expect(manager.status, SessionStatus.signedOut);
      expect(store.raw, isNull);
      expect(store.clears, 1);
    });

    test(
      'corrupt data that cannot be cleared still starts signed out',
      () async {
        store
          ..raw = 'garbage'
          ..failingClears = 1;
        await manager.restore();
        expect(manager.status, SessionStatus.signedOut);
      },
    );

    test('a storage error starts signed out and keeps the data', () async {
      store
        ..raw = storedRaw(clock, '1')
        ..readError = const TokenStoreException('read');
      await manager.restore();
      expect(manager.status, SessionStatus.signedOut);
      expect(store.raw, isNotNull);
      expect(store.clears, 0);
    });

    test('a hung storage read gives up after the timeout', () {
      fakeAsync((async) {
        store
          ..raw = storedRaw(clock, '1')
          ..readGate = Completer<void>();
        unawaited(manager.restore());
        async.elapse(const Duration(seconds: 9));
        expect(manager.status, SessionStatus.unknown);
        async.elapse(const Duration(seconds: 2));
        expect(manager.status, SessionStatus.signedOut);
        expect(store.clears, 0);
      });
    });

    test('runs once', () async {
      final a = manager.restore();
      final b = manager.restore();
      expect(identical(a, b), isTrue);
      await a;
      await manager.restore();
      expect(store.reads, 1);
    });
  });

  group('sign-in', () {
    setUp(() async => manager.restore());

    test('stores the tokens, then signs in', () async {
      server.once(
        'POST',
        ApiPaths.login,
        (_) =>
            jsonResponse(200, tokenBody(accessToken('1'), refreshToken('1'))),
      );
      await manager.signIn(email: 'a@b.c', password: 'pw');
      expect(manager.status, SessionStatus.signedIn);
      expect(notified, [SessionStatus.signedOut, SessionStatus.signedIn]);
      final s = store.session!;
      expect(s.accessToken, accessToken('1'));
      expect(s.refreshToken, refreshToken('1'));
      expect(s.accessExpiresAt, clock.now().add(const Duration(minutes: 15)));
      expect(s.accessLifetime, const Duration(minutes: 15));
    });

    test('measures expiry from when the request was sent', () async {
      server.once('POST', ApiPaths.login, (_) {
        clock.advance(const Duration(seconds: 4));
        return jsonResponse(
          200,
          tokenBody(accessToken('1'), refreshToken('1')),
        );
      });
      final sent = clock.now();
      await manager.signIn(email: 'a@b.c', password: 'pw');
      expect(
        store.session!.accessExpiresAt,
        sent.add(const Duration(minutes: 15)),
      );
    });

    test('an API error propagates and stays signed out', () async {
      server.once(
        'POST',
        ApiPaths.login,
        (_) => errorResponse(401, 'invalid_credentials'),
      );
      await expectLater(
        manager.signIn(email: 'a@b.c', password: 'pw'),
        throwsA(
          isA<ApiHttpException>().having(
            (e) => e.code,
            'code',
            'invalid_credentials',
          ),
        ),
      );
      expect(manager.status, SessionStatus.signedOut);
      expect(store.writes, 0);
      // Not wedged.
      server.once(
        'POST',
        ApiPaths.login,
        (_) =>
            jsonResponse(200, tokenBody(accessToken('1'), refreshToken('1'))),
      );
      await manager.signIn(email: 'a@b.c', password: 'pw');
      expect(manager.status, SessionStatus.signedIn);
    });

    test('a failed store write leaves the user signed out', () async {
      store.writeError = const TokenStoreException('write');
      server.once(
        'POST',
        ApiPaths.login,
        (_) =>
            jsonResponse(200, tokenBody(accessToken('1'), refreshToken('1'))),
      );
      await expectLater(
        manager.signIn(email: 'a@b.c', password: 'pw'),
        throwsA(isA<TokenStoreException>()),
      );
      expect(manager.status, SessionStatus.signedOut);
      await expectLater(manager.me(), throwsA(isA<SignedOutException>()));
    });

    test('is refused while signed in, unknown, or already running', () async {
      final gate = Completer<void>();
      server.once('POST', ApiPaths.login, (_) async {
        await gate.future;
        return jsonResponse(
          200,
          tokenBody(accessToken('1'), refreshToken('1')),
        );
      });
      final first = manager.signIn(email: 'a@b.c', password: 'pw');
      expect(
        () => manager.signIn(email: 'a@b.c', password: 'pw'),
        throwsStateError,
      );
      gate.complete();
      await first;
      expect(
        () => manager.signIn(email: 'a@b.c', password: 'pw'),
        throwsStateError,
      );
      expect(server.count(ApiPaths.login), 1);

      final fresh = build();
      addTearDown(fresh.dispose);
      expect(
        () => fresh.signIn(email: 'a@b.c', password: 'pw'),
        throwsStateError,
      );
    });
  });

  group('me', () {
    final meBody = {
      'id': 'u1',
      'email': 'a@b.c',
      'email_verified_at': '2026-10-01T00:00:00Z',
      'created_at': '2026-10-01T00:00:00Z',
    };

    test(
      'without a session throws SignedOutException and sends nothing',
      () async {
        await manager.restore();
        await expectLater(manager.me(), throwsA(isA<SignedOutException>()));
        expect(server.requests, isEmpty);
      },
    );

    test('returns the user, sending the access token as Bearer', () async {
      store.raw = storedRaw(clock, '1');
      await manager.restore();
      server.once('GET', ApiPaths.me, (_) => jsonResponse(200, meBody));
      final me = await manager.me();
      expect(me.id, 'u1');
      expect(me.email, 'a@b.c');
      final r = server.requests.single;
      expect(r.method, 'GET');
      expect(r.url.path, ApiPaths.me);
      expect(r.headers['Authorization'], 'Bearer ${accessToken('1')}');
      expect(r.bodyBytes, isEmpty);
    });

    test('non-401 failures pass through and keep the session', () async {
      store.raw = storedRaw(clock, '1');
      await manager.restore();
      final failures = <Responder, Matcher>{
        (_) => errorResponse(500, 'internal_error'): isA<ApiHttpException>()
            .having((e) => e.statusCode, 'status', 500),
        (_) => errorResponse(403, 'forbidden'): isA<ApiHttpException>().having(
          (e) => e.statusCode,
          'status',
          403,
        ),
        (_) => throw http.ClientException('reset'): isA<ApiNetworkException>(),
        (r) => throw http.RequestAbortedException(r.url):
            isA<ApiTimeoutException>(),
        (_) => jsonResponse(200, {'id': 'u1'}): isA<ApiProtocolException>(),
      };
      for (final MapEntry(key: responder, value: matcher) in failures.entries) {
        server.once('GET', ApiPaths.me, responder);
        await expectLater(manager.me(), throwsA(matcher));
      }
      expect(manager.status, SessionStatus.signedIn);
      expect(server.count(ApiPaths.refresh), 0);
      expect(store.session!.refreshToken, refreshToken('1'));
    });
  });

  group('dispose', () {
    test('stops notifications and refuses further calls', () async {
      store.raw = storedRaw(clock, '1');
      final gate = Completer<void>();
      store.readGate = gate;
      final restoring = manager.restore();
      manager.dispose();
      gate.complete();
      await restoring;
      expect(notified, isEmpty);
      expect(() => manager.restore(), throwsStateError);
      expect(() => manager.logout(), throwsStateError);
      expect(() => manager.signIn(email: 'a', password: 'b'), throwsStateError);
      expect(() => manager.me(), throwsStateError);
    });
  });
}

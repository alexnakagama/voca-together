import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/session.dart';

import 'support/fakes.dart';

void main() {
  late FakeServer server;
  late InMemoryTokenStore store;
  late FakeAuthClock clock;
  late SessionManager manager;

  setUp(() async {
    server = FakeServer()..always('GET', ApiPaths.healthz, (_) => healthy());
    store = InMemoryTokenStore();
    clock = FakeAuthClock();
    manager = await signedInManager(server, store, clock);
  });

  tearDown(() => manager.dispose());

  String? bearerOf(http.Request r) => r.headers['Authorization'];

  final outcomes = <String, Responder>{
    '204': (_) => noContent(),
    '401': (_) => errorResponse(401, 'invalid_access_token'),
    '500': (_) => errorResponse(500, 'internal_error'),
    '503': (_) => errorResponse(503, 'service_unavailable'),
    'unexpected 200': (_) => jsonResponse(200, <String, Object?>{}),
    'redirect': (_) => http.Response('', 302, headers: {'location': '/'}),
    'network error': (_) => throw http.ClientException('reset'),
    'timeout': (r) => throw http.RequestAbortedException(r.url),
  };

  outcomes.forEach((name, responder) {
    test('$name: signed out and credentials deleted', () async {
      server.once('POST', ApiPaths.logout, responder);
      await manager.logout();
      expect(manager.status, SessionStatus.signedOut);
      expect(store.raw, isNull);
      final r = server.to(ApiPaths.logout).single;
      expect(bearerOf(r), 'Bearer ${accessToken('1')}');
      expect(r.bodyBytes, isEmpty);
    });
  });

  test('ends the local session before calling the server', () async {
    final release = Completer<void>();
    server.once('POST', ApiPaths.logout, (_) async {
      await release.future;
      return noContent();
    });
    final notified = <SessionStatus>[];
    manager.addListener(() => notified.add(manager.status));

    final logout = manager.logout();
    await settle();
    expect(server.count(ApiPaths.logout), 1);
    expect(manager.status, SessionStatus.signedOut);
    expect(notified, [SessionStatus.signedOut]);
    expect(store.raw, isNull);
    await expectLater(manager.me(), throwsA(isA<SignedOutException>()));

    release.complete();
    await logout;
    expect(notified, [SessionStatus.signedOut]);
  });

  test(
    'waits for a running refresh and logs out with the rotated token',
    () async {
      final release = Completer<void>();
      server.once('POST', ApiPaths.refresh, (_) async {
        await release.future;
        return jsonResponse(
          200,
          tokenBody(accessToken('2'), refreshToken('2')),
        );
      });
      server
        ..always('GET', ApiPaths.me, (_) => errorResponse(401, 'x'))
        ..once('POST', ApiPaths.logout, (_) => noContent());

      clock.advance(const Duration(minutes: 10)); // stale: refresh first
      final call = manager.me().then<Object?>(
        (v) => v,
        onError: (Object e) => e,
      );
      await settle();
      expect(server.count(ApiPaths.refresh), 1);

      final logout = manager.logout();
      await settle();
      expect(server.count(ApiPaths.logout), 0);
      expect(manager.status, SessionStatus.signedIn);

      release.complete();
      await logout;
      await call;
      expect(
        bearerOf(server.to(ApiPaths.logout).single),
        'Bearer ${accessToken('2')}',
      );
      expect(manager.status, SessionStatus.signedOut);
      expect(store.raw, isNull);
      // Nothing else is sent once the logout has begun.
      expect(server.count(ApiPaths.refresh), 1);
      expect(server.count(ApiPaths.me), 0);
      expect(await call, isA<SignedOutException>());
    },
  );

  test(
    'a refresh that fails during logout leaves nothing to resurrect',
    () async {
      final release = Completer<void>();
      server.once('POST', ApiPaths.refresh, (_) async {
        await release.future;
        return errorResponse(401, 'invalid_refresh_token');
      });
      clock.advance(const Duration(minutes: 10));
      final call = expectLater(
        manager.me(),
        throwsA(isA<SignedOutException>()),
      );
      await settle();
      final logout = manager.logout();
      release.complete();
      await logout;
      await call;
      expect(manager.status, SessionStatus.signedOut);
      expect(store.raw, isNull);
      // The session was already gone: no token to send.
      expect(server.count(ApiPaths.logout), 0);
    },
  );

  test('concurrent logouts share one server call', () async {
    server.once('POST', ApiPaths.logout, (_) => noContent());
    final a = manager.logout();
    final b = manager.logout();
    expect(identical(a, b), isTrue);
    await Future.wait([a, b]);
    expect(server.count(ApiPaths.logout), 1);
    await manager.logout();
    expect(server.count(ApiPaths.logout), 1);
  });

  test('a sign-in after logout is never cleared by the old logout', () async {
    final release = Completer<void>();
    server
      ..once('POST', ApiPaths.logout, (_) async {
        await release.future;
        return noContent();
      })
      ..once(
        'POST',
        ApiPaths.login,
        (_) =>
            jsonResponse(200, tokenBody(accessToken('2'), refreshToken('2'))),
      );

    final logout = manager.logout();
    await settle();
    expect(manager.status, SessionStatus.signedOut);

    await manager.signIn(email: 'a@b.c', password: 'pw');
    release.complete();
    await logout;

    expect(manager.status, SessionStatus.signedIn);
    expect(store.session!.refreshToken, refreshToken('2'));
  });

  test('logout during sign-in waits for it, then ends that session', () async {
    manager.dispose();
    store = InMemoryTokenStore();
    manager = SessionManager(
      store: store,
      authApi: authApiFor(server.client),
      clock: clock,
    );
    await manager.restore();
    final release = Completer<void>();
    server
      ..once('POST', ApiPaths.login, (_) async {
        await release.future;
        return jsonResponse(
          200,
          tokenBody(accessToken('2'), refreshToken('2')),
        );
      })
      ..once('POST', ApiPaths.logout, (_) => noContent());

    final signIn = manager.signIn(email: 'a@b.c', password: 'pw');
    await settle();
    final logout = manager.logout();
    release.complete();
    await signIn;
    await logout;

    expect(manager.status, SessionStatus.signedOut);
    expect(store.raw, isNull);
    expect(
      bearerOf(server.to(ApiPaths.logout).single),
      'Bearer ${accessToken('2')}',
    );
  });

  test('logout during restore waits for it', () async {
    manager.dispose();
    store = InMemoryTokenStore(raw: storedRaw(clock, '1'))
      ..readGate = Completer<void>();
    manager = SessionManager(
      store: store,
      authApi: authApiFor(server.client),
      clock: clock,
    );
    server.once('POST', ApiPaths.logout, (_) => noContent());
    final restore = manager.restore();
    final logout = manager.logout();
    store.readGate!.complete();
    await restore;
    await logout;
    expect(manager.status, SessionStatus.signedOut);
    expect(store.raw, isNull);
    expect(server.count(ApiPaths.logout), 1);
  });

  test('a failing store clear is retried once', () async {
    server.once('POST', ApiPaths.logout, (_) => noContent());
    store.failingClears = 1;
    await manager.logout();
    expect(store.clears, 2);
    expect(store.raw, isNull);
    expect(manager.status, SessionStatus.signedOut);
  });

  test('a store that cannot be cleared still ends signed out', () async {
    server.once('POST', ApiPaths.logout, (_) => noContent());
    store.failingClears = 2;
    await manager.logout();
    expect(manager.status, SessionStatus.signedOut);
    // The server session was still revoked with the latest token.
    expect(server.count(ApiPaths.logout), 1);
  });

  test('when signed out, logout sends nothing', () async {
    server.once('POST', ApiPaths.logout, (_) => noContent());
    await manager.logout();
    await manager.logout();
    expect(server.count(ApiPaths.logout), 1);
    expect(manager.status, SessionStatus.signedOut);
  });
}

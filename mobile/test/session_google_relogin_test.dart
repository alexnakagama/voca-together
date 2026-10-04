import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_sign_in_platform_interface/google_sign_in_platform_interface.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/auth/google_identity_plugin.dart';
import 'package:vocatogether/session.dart';

import 'support/fake_google_platform.dart';
import 'support/fakes.dart';

/// Signing in with Google more than once (decisions 025 and 026).
///
/// The real `SessionManager` and the real plugin adapter run over a scripted
/// Google platform, so the calls that reach Google and the backend are the
/// ones the app would make on a device. Observed there: Google returns the
/// same ID token again while it is valid, even to a new app process. The
/// backend accepts it every time, so each of these sign-ins must succeed.
void main() {
  /// The one token Google keeps returning.
  const token = 'cached.payload.sig';
  const posted = '{"id_token":"$token"}';

  late FakeServer server;
  late InMemoryTokenStore store;
  late FakeGooglePlatform platform;
  late SessionManager manager;

  http.Response ok(String marker) =>
      jsonResponse(200, tokenBody(accessToken(marker), refreshToken(marker)));

  List<String> bodies() =>
      server.to(ApiPaths.google).map((r) => r.body).toList();

  /// A new app process: a new session manager and a new plugin adapter (so
  /// the plugin is initialized again), over the same device and backend.
  SessionManager newProcess() => SessionManager(
    store: store,
    authApi: authApiFor(server.client),
    clock: FakeAuthClock(),
    google: PluginGoogleIdentity(
      serverClientId: '1234-abc.apps.googleusercontent.com',
    ),
  );

  // Builds everything in the current zone, so a fake-time test can own it.
  void build() {
    server = FakeServer();
    store = InMemoryTokenStore();
    platform = FakeGooglePlatform();
    GoogleSignInPlatform.instance = platform;
    manager = newProcess();
  }

  setUp(() async {
    build();
    await manager.restore();
  });

  tearDown(() => manager.dispose());

  test('log out, then sign in with Google again: the identical token is sent '
      'again and both sign-ins succeed', () async {
    platform
      ..answer(token)
      ..answer(token);
    server
      ..once('POST', ApiPaths.google, (_) => ok('1'))
      ..once('POST', ApiPaths.logout, (_) => noContent())
      ..once('POST', ApiPaths.google, (_) => ok('2'));

    await manager.signInWithGoogle();
    expect(manager.status, SessionStatus.signedIn);
    await manager.logout();
    expect(manager.status, SessionStatus.signedOut);
    await manager.signInWithGoogle();

    expect(manager.status, SessionStatus.signedIn);
    expect(bodies(), [posted, posted]);
    expect(platform.authenticates, hasLength(2), reason: 'Google asked twice');
    expect(store.session!.accessToken, accessToken('2'));
    expect(store.raw, isNot(contains('cached')));
  });

  test('two Google sign-ins that get the identical token both succeed, '
      'whatever ended the first session', () async {
    platform
      ..answer(token)
      ..answer(token);
    server
      ..once('POST', ApiPaths.google, (_) => ok('1'))
      ..always('GET', ApiPaths.healthz, (_) => healthy())
      ..once(
        'GET',
        ApiPaths.me,
        (_) => errorResponse(401, 'invalid_access_token'),
      )
      ..once(
        'POST',
        ApiPaths.refresh,
        (_) => errorResponse(401, 'invalid_refresh_token'),
      )
      ..once('POST', ApiPaths.google, (_) => ok('2'));

    await manager.signInWithGoogle();
    // The session is revoked on the server; no logout, so no Google sign-out.
    await expectLater(manager.me(), throwsA(isA<SignedOutException>()));
    expect(manager.status, SessionStatus.signedOut);
    expect(platform.signOuts, 0);

    await manager.signInWithGoogle();

    expect(manager.status, SessionStatus.signedIn);
    expect(bodies(), [posted, posted]);
  });

  test('after the app is force-stopped, a new process signs in with the '
      'identical token', () async {
    platform
      ..answer(token)
      ..answer(token);
    server
      ..once('POST', ApiPaths.google, (_) => ok('1'))
      ..once('POST', ApiPaths.logout, (_) => noContent())
      ..once('POST', ApiPaths.google, (_) => ok('2'));
    await manager.signInWithGoogle();
    await manager.logout();
    await settle();

    // Force-stop: the process and everything in its memory are gone.
    manager.dispose();
    manager = newProcess();
    await manager.restore();
    expect(manager.status, SessionStatus.signedOut);

    await manager.signInWithGoogle();

    expect(manager.status, SessionStatus.signedIn);
    expect(bodies(), [posted, posted]);
    expect(platform.inits, hasLength(2), reason: 'one per process');
  });

  test('a force-stopped app that was signed in restores its session without '
      'asking Google', () async {
    platform.answer(token);
    server.once('POST', ApiPaths.google, (_) => ok('1'));
    await manager.signInWithGoogle();

    manager.dispose();
    manager = newProcess();
    await manager.restore();

    expect(manager.status, SessionStatus.signedIn);
    expect(platform.authenticates, hasLength(1));
    expect(server.count(ApiPaths.google), 1);
  });

  group('order of sign-out and the next sign-in (the plugin\'s contract, not '
      'a security rule)', () {
    test('a sign-in started while the logout\'s Google sign-out is still '
        'running waits for it, then authenticates', () async {
      platform
        ..answer(token)
        ..answer(token);
      server
        ..once('POST', ApiPaths.google, (_) => ok('1'))
        ..once('POST', ApiPaths.logout, (_) => noContent())
        ..once('POST', ApiPaths.google, (_) => ok('2'));
      await manager.signInWithGoogle();

      // Logout returns although Google's sign-out hasn't finished.
      final signOut = Completer<void>();
      platform.signOutGate = signOut;
      await manager.logout();
      expect(manager.status, SessionStatus.signedOut);
      expect(store.raw, isNull);
      expect(server.count(ApiPaths.logout), 1);
      expect(platform.log.last, 'signOut:start');

      // A new sign-in starts and holds back until the sign-out is done.
      final signIn = manager.signInWithGoogle();
      await settle();
      expect(platform.authenticates, hasLength(1), reason: 'not yet');
      expect(server.count(ApiPaths.google), 1);

      signOut.complete();
      await signIn;

      expect(platform.log, [
        'authenticate',
        'signOut:start',
        'signOut:end',
        'authenticate',
      ]);
      expect(bodies(), [posted, posted]);
      expect(manager.status, SessionStatus.signedIn);
    });

    test('a failed Google sign-out fails nothing: logout succeeds and the '
        'next Google sign-in works', () async {
      platform
        ..answer(token)
        ..answer(token)
        ..signOutError = StateError('clear failed');
      server
        ..once('POST', ApiPaths.google, (_) => ok('1'))
        ..once('POST', ApiPaths.logout, (_) => noContent())
        ..once('POST', ApiPaths.google, (_) => ok('2'));

      await manager.signInWithGoogle();
      await manager.logout();
      expect(manager.status, SessionStatus.signedOut);
      expect(store.raw, isNull);

      await manager.signInWithGoogle();

      expect(platform.signOuts, 1);
      expect(platform.log, [
        'authenticate',
        'signOut:start',
        'signOut:end',
        'authenticate',
      ]);
      expect(manager.status, SessionStatus.signedIn);
    });

    test('a Google sign-out that never finishes delays the next sign-in only '
        'for a bounded time', () {
      fakeAsync((async) {
        manager.dispose();
        build();
        manager.restore();
        async.flushMicrotasks();
        platform
          ..answer(token)
          ..answer(token);
        server
          ..once('POST', ApiPaths.google, (_) => ok('1'))
          // The server's answer to logout doesn't matter; this is instant.
          ..once('POST', ApiPaths.logout, networkFailure)
          ..once('POST', ApiPaths.google, (_) => ok('2'));
        manager.signInWithGoogle();
        async.elapse(const Duration(seconds: 1));
        expect(manager.status, SessionStatus.signedIn);
        platform.signOutGate = Completer<void>();
        var loggedOut = false;
        manager.logout().then((_) => loggedOut = true);
        async.elapse(const Duration(seconds: 1));
        expect(loggedOut, isTrue, reason: 'logout never waits for Google');

        manager.signInWithGoogle();
        async.elapse(
          SessionManager.googleSignOutWait - const Duration(milliseconds: 1),
        );
        expect(platform.authenticates, hasLength(1));
        async.elapse(const Duration(milliseconds: 1));
        async.flushMicrotasks();

        expect(platform.authenticates, hasLength(2));
        expect(manager.status, SessionStatus.signedIn);
        expect(bodies(), [posted, posted]);
      });
    });
  });
}

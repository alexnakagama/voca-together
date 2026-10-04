import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/auth/google_identity_exception.dart';
import 'package:vocatogether/auth/token_store.dart';
import 'package:vocatogether/session.dart';

import 'support/fakes.dart';

void main() {
  late FakeServer server;
  late InMemoryTokenStore store;
  late FakeAuthClock clock;
  late FakeGoogleIdentity google;
  late SessionManager manager;
  var disposed = false;

  SessionManager build({bool withGoogle = true}) => SessionManager(
    store: store,
    authApi: authApiFor(server.client),
    clock: clock,
    google: withGoogle ? google : null,
  );

  http.Response ok(String marker) =>
      jsonResponse(200, tokenBody(accessToken(marker), refreshToken(marker)));

  setUp(() async {
    server = FakeServer();
    store = InMemoryTokenStore();
    clock = FakeAuthClock();
    google = FakeGoogleIdentity();
    disposed = false;
    manager = build();
    await manager.restore();
  });

  tearDown(() {
    if (!disposed) manager.dispose();
  });

  test('is available only with a Google identity', () async {
    expect(manager.googleSignInAvailable, isTrue);
    final without = build(withGoogle: false);
    addTearDown(without.dispose);
    await without.restore();
    expect(without.googleSignInAvailable, isFalse);
    expect(without.signInWithGoogle, throwsStateError);
    expect(server.requests, isEmpty);
    // Not wedged: password sign-in still works.
    server.once('POST', ApiPaths.login, (_) => ok('1'));
    await without.signIn(email: 'a@b.c', password: 'pw');
    expect(without.status, SessionStatus.signedIn);
  });

  test('posts only the ID token, in the body, and starts a session', () async {
    google.next(googleIdToken('one'));
    server.once('POST', ApiPaths.google, (_) => ok('1'));

    await manager.signInWithGoogle();

    expect(manager.status, SessionStatus.signedIn);
    final request = server.requests.single;
    expect(request.url.path, ApiPaths.google);
    expect(request.body, '{"id_token":"${googleIdToken('one')}"}');
    expect(request.headers.containsKey('Authorization'), isFalse);
    expect(store.session!.accessToken, accessToken('1'));
    expect(store.raw, isNot(contains('GID')), reason: 'never persisted');
  });

  test('a new account and a returning one are the same 200: each sign-in '
      'asks Google again', () async {
    google
      ..next(googleIdToken('first'))
      ..next(googleIdToken('second'));
    server
      ..once('POST', ApiPaths.google, (_) => ok('1'))
      ..once('POST', ApiPaths.logout, (_) => noContent())
      ..once('POST', ApiPaths.google, (_) => ok('2'));

    await manager.signInWithGoogle();
    await manager.logout();
    await manager.signInWithGoogle();

    expect(manager.status, SessionStatus.signedIn);
    expect(google.calls, 2);
    expect(server.to(ApiPaths.google).map((r) => r.body), [
      '{"id_token":"${googleIdToken('first')}"}',
      '{"id_token":"${googleIdToken('second')}"}',
    ]);
    expect(store.session!.accessToken, accessToken('2'));
  });

  group('a provider failure', () {
    for (final failure in GoogleIdentityFailure.values) {
      test('${failure.name} sends nothing and stays signed out', () async {
        google.fail(GoogleIdentityException(failure));

        await expectLater(
          manager.signInWithGoogle(),
          throwsA(
            isA<GoogleIdentityException>().having(
              (e) => e.failure,
              'failure',
              failure,
            ),
          ),
        );

        expect(server.requests, isEmpty);
        expect(manager.status, SessionStatus.signedOut);
        expect(store.writes, 0);
        // Not wedged.
        google.next(googleIdToken('retry'));
        server.once('POST', ApiPaths.google, (_) => ok('1'));
        await manager.signInWithGoogle();
        expect(manager.status, SessionStatus.signedIn);
      });
    }
  });

  group('a backend failure', () {
    final failures = <String, (Responder, Matcher)>{
      '401 invalid_google_token': (
        (_) => errorResponse(401, 'invalid_google_token'),
        isA<ApiHttpException>().having((e) => e.statusCode, 'status', 401),
      ),
      '403 google_email_unusable': (
        (_) => errorResponse(403, 'google_email_unusable'),
        isA<ApiHttpException>().having(
          (e) => e.code,
          'code',
          'google_email_unusable',
        ),
      ),
      '409 account_exists': (
        (_) => errorResponse(409, 'account_exists'),
        isA<ApiHttpException>().having((e) => e.code, 'code', 'account_exists'),
      ),
      '429': (
        (_) =>
            errorResponse(429, 'rate_limited', headers: {'retry-after': '7'}),
        isA<ApiHttpException>().having(
          (e) => e.retryAfter,
          'retryAfter',
          const Duration(seconds: 7),
        ),
      ),
      '500': (
        (_) => errorResponse(500, 'internal_error'),
        isA<ApiHttpException>().having((e) => e.statusCode, 'status', 500),
      ),
      '503': (
        (_) => errorResponse(
          503,
          'service_unavailable',
          headers: {'retry-after': '5'},
        ),
        isA<ApiHttpException>().having((e) => e.statusCode, 'status', 503),
      ),
      'a network error': (networkFailure, isA<ApiNetworkException>()),
      'a redirect': (
        (_) => http.Response('', 302, headers: {'location': '/'}),
        isA<ApiProtocolException>(),
      ),
      'a 200 without tokens': (
        (_) => jsonResponse(200, {'access_token': accessToken('1')}),
        isA<ApiProtocolException>(),
      ),
      'a 200 that is not JSON': (
        (_) => http.Response('ok', 200, headers: {'content-type': 'text/html'}),
        isA<ApiProtocolException>(),
      ),
      'an unexpected 2xx': (
        (_) => jsonResponse(202, {'status': 'accepted'}),
        isA<ApiProtocolException>(),
      ),
    };

    failures.forEach((name, value) {
      final (responder, matcher) = value;
      test('$name is rethrown, never retried, and the next attempt asks '
          'Google again', () async {
        google
          ..next(googleIdToken('first'))
          ..next(googleIdToken('second'));
        server.once('POST', ApiPaths.google, responder);

        await expectLater(manager.signInWithGoogle(), throwsA(matcher));

        expect(server.count(ApiPaths.google), 1, reason: 'no retry');
        expect(google.calls, 1);
        expect(manager.status, SessionStatus.signedOut);
        expect(store.writes, 0);

        server.once('POST', ApiPaths.google, (_) => ok('1'));
        await manager.signInWithGoogle();
        expect(
          server.to(ApiPaths.google).last.body,
          '{"id_token":"${googleIdToken('second')}"}',
        );
        expect(manager.status, SessionStatus.signedIn);
      });
    });

    test('a timeout is rethrown and the token is not sent again', () {
      fakeAsync((async) {
        google.next(googleIdToken('one'));
        server.once('POST', ApiPaths.google, neverAnswers);
        Object? error;
        manager.signInWithGoogle().catchError((Object e) {
          error = e;
        });
        async.elapse(const Duration(seconds: 16));
        expect(error, isA<ApiTimeoutException>());
        expect(server.count(ApiPaths.google), 1);
        expect(manager.status, SessionStatus.signedOut);
      });
    });

    test('a token AuthApi refuses is an error, not a request', () async {
      google.next('');
      await expectLater(manager.signInWithGoogle(), throwsArgumentError);
      expect(server.requests, isEmpty);
      expect(manager.status, SessionStatus.signedOut);
    });
  });

  test('a failed store write leaves the user signed out', () async {
    google.next(googleIdToken('one'));
    server.once('POST', ApiPaths.google, (_) => ok('1'));
    store.writeError = const TokenStoreException('write');

    await expectLater(
      manager.signInWithGoogle(),
      throwsA(isA<TokenStoreException>()),
    );
    expect(manager.status, SessionStatus.signedOut);
  });

  group('concurrency', () {
    test('a second sign-in of either kind is refused while the account '
        'chooser is open', () async {
      final chooser = Completer<String>();
      google.wait(chooser);
      server.once('POST', ApiPaths.google, (_) => ok('1'));

      final first = manager.signInWithGoogle();
      expect(manager.signInWithGoogle, throwsStateError);
      expect(
        () => manager.signIn(email: 'a@b.c', password: 'pw'),
        throwsStateError,
      );
      expect(google.calls, 1);
      expect(server.requests, isEmpty);

      chooser.complete(googleIdToken('one'));
      await first;
      expect(manager.status, SessionStatus.signedIn);
      expect(server.count(ApiPaths.google), 1);
    });

    test('Google sign-in is refused while a password sign-in runs', () async {
      final answer = Completer<http.Response>();
      server.once('POST', ApiPaths.login, (_) => answer.future);
      final first = manager.signIn(email: 'a@b.c', password: 'pw');

      expect(manager.signInWithGoogle, throwsStateError);
      expect(google.calls, 0);

      answer.complete(ok('1'));
      await first;
    });

    test('is refused while signed in', () async {
      google.next(googleIdToken('one'));
      server.once('POST', ApiPaths.google, (_) => ok('1'));
      await manager.signInWithGoogle();

      expect(manager.signInWithGoogle, throwsStateError);
      expect(google.calls, 1);
    });

    test('a logout requested while the chooser is open waits for the '
        'sign-in, then ends its session', () async {
      final chooser = Completer<String>();
      google.wait(chooser);
      server
        ..once('POST', ApiPaths.google, (_) => ok('1'))
        ..once('POST', ApiPaths.logout, (_) => noContent());

      final signIn = manager.signInWithGoogle();
      final logout = manager.logout();
      await settle();
      expect(server.requests, isEmpty, reason: 'logout waits');

      chooser.complete(googleIdToken('one'));
      await signIn;
      await logout;

      expect(manager.status, SessionStatus.signedOut);
      expect(store.raw, isNull);
      expect(
        server.to(ApiPaths.logout).single.headers['Authorization'],
        'Bearer ${accessToken('1')}',
      );
    });

    test('a logout requested while the chooser is open and then cancelled '
        'leaves a clean signed-out state', () async {
      final chooser = Completer<String>();
      google.wait(chooser);

      final signIn = manager.signInWithGoogle();
      final logout = manager.logout();
      chooser.completeError(
        const GoogleIdentityException(GoogleIdentityFailure.cancelled),
      );
      await expectLater(signIn, throwsA(isA<GoogleIdentityException>()));
      await logout;

      expect(manager.status, SessionStatus.signedOut);
      expect(server.requests, isEmpty);
    });
  });

  group('lifecycle', () {
    test(
      'disposed while the chooser is open: the token is never sent',
      () async {
        final chooser = Completer<String>();
        google.wait(chooser);

        final signIn = manager.signInWithGoogle();
        manager.dispose();
        disposed = true;
        chooser.complete(googleIdToken('one'));
        await signIn;

        expect(server.requests, isEmpty);
        expect(store.writes, 0);
        expect(manager.status, SessionStatus.signedOut);
      },
    );

    test('the access token lifetime counts from the request, not from when '
        'the chooser opened', () async {
      final chooser = Completer<String>();
      google.wait(chooser);
      server.once('POST', ApiPaths.google, (_) => ok('1'));

      final signIn = manager.signInWithGoogle();
      // The user takes twenty minutes to pick an account.
      clock.advance(const Duration(minutes: 20));
      final sent = clock.now();
      chooser.complete(googleIdToken('one'));
      await signIn;

      expect(
        store.session!.accessExpiresAt,
        sent.add(const Duration(minutes: 15)),
      );
      // So the new session is usable without a refresh.
      server.once('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()));
      await manager.me();
      expect(server.count(ApiPaths.refresh), 0);
    });
  });

  group('logout', () {
    Future<void> signInWithGoogle() async {
      google.next(googleIdToken('one'));
      server.once('POST', ApiPaths.google, (_) => ok('1'));
      await manager.signInWithGoogle();
    }

    test(
      "clears Google's credential state once, after the local session",
      () async {
        await signInWithGoogle();
        String? storedAtClear = 'unset';
        SessionStatus? statusAtClear;
        var logoutSentAtClear = -1;
        google.onClear = () {
          storedAtClear = store.raw;
          statusAtClear = manager.status;
          logoutSentAtClear = server.count(ApiPaths.logout);
        };
        server.once('POST', ApiPaths.logout, (_) => noContent());

        await manager.logout();

        expect(logoutSentAtClear, 0);
        expect(storedAtClear, isNull);
        expect(statusAtClear, SessionStatus.signedOut);
        expect(google.clears, 1);
      },
    );

    test('does not wait for Google: a clear that never finishes changes '
        'nothing', () async {
      await signInWithGoogle();
      google.clearGate = Completer<void>();
      server.once('POST', ApiPaths.logout, (_) => noContent());

      await manager.logout();

      expect(manager.status, SessionStatus.signedOut);
      expect(store.raw, isNull);
      expect(server.count(ApiPaths.logout), 1);
    });

    test(
      'succeeds when clearing Google fails, without an unhandled error',
      () async {
        await signInWithGoogle();
        google.clearError = StateError('google is broken');
        server.once('POST', ApiPaths.logout, (_) => noContent());

        await manager.logout();
        await settle();

        expect(manager.status, SessionStatus.signedOut);
        expect(store.raw, isNull);
        expect(google.clears, 1);
      },
    );

    test('also clears Google after a password session (the account may have '
        'been chosen in an earlier run)', () async {
      server
        ..once('POST', ApiPaths.login, (_) => ok('1'))
        ..once('POST', ApiPaths.logout, (_) => noContent());
      await manager.signIn(email: 'a@b.c', password: 'pw');
      await manager.logout();
      expect(google.clears, 1);
    });

    test('works without a Google identity', () async {
      final without = build(withGoogle: false);
      addTearDown(without.dispose);
      await without.restore();
      server
        ..once('POST', ApiPaths.login, (_) => ok('1'))
        ..once('POST', ApiPaths.logout, (_) => noContent());
      await without.signIn(email: 'a@b.c', password: 'pw');
      await without.logout();
      expect(without.status, SessionStatus.signedOut);
    });
  });
}

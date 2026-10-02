import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/app.dart';
import 'package:vocatogether/auth/token_store.dart';
import 'package:vocatogether/config.dart';
import 'package:vocatogether/router.dart';
import 'package:vocatogether/screens/forgot_password_screen.dart';
import 'package:vocatogether/screens/home_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/register_screen.dart';
import 'package:vocatogether/screens/splash_screen.dart';
import 'package:vocatogether/session.dart';

import 'support/fakes.dart';

final _config = AppConfig(apiBaseUrl: Uri.parse('http://10.0.2.2:8080'));

void main() {
  group('authRedirect', () {
    const s = Routes.splash;
    const l = Routes.login;
    const h = Routes.home;
    // path → expected result for unknown, signedOut, signedIn (null = stay).
    const cases = <String, List<String?>>{
      '/splash': [null, l, h],
      '/login': [s, null, h],
      '/register': [s, null, h],
      '/forgot-password': [s, null, h],
      '/home': [s, l, null],
      '/home?x=1': [s, l, null],
      '/login/': [s, l, h],
      '/home/': [s, l, h],
      '/nope': [s, l, h],
      '/': [s, l, h],
      '': [s, l, h],
    };

    cases.forEach((location, expected) {
      for (final status in SessionStatus.values) {
        test('$status at "$location"', () {
          expect(
            authRedirect(status, Uri.parse(location)),
            expected[status.index],
          );
        });
      }
    });

    test('every destination is final (no redirect loop)', () {
      for (final status in SessionStatus.values) {
        for (final location in cases.keys) {
          final target = authRedirect(status, Uri.parse(location));
          if (target != null) {
            expect(authRedirect(status, Uri.parse(target)), isNull);
          }
        }
      }
    });
  });

  group('routing', () {
    Future<SessionManager> pumpApp(
      WidgetTester tester,
      SessionStatus initial,
    ) async {
      final session = await _managerAt(initial);
      addTearDown(session.dispose);
      await tester.pumpWidget(
        VocaTogetherApp(config: _config, session: session),
      );
      await tester.pump();
      return session;
    }

    // Navigates as a screen would; the redirect decides where it ends up.
    Future<void> go(WidgetTester tester, String location) async {
      GoRouter.of(tester.element(find.byType(Navigator).first)).go(location);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
    }

    testWidgets('unknown shows only the splash', (tester) async {
      await pumpApp(tester, SessionStatus.unknown);
      expect(find.byType(SplashScreen), findsOneWidget);

      for (final location in [
        '/login',
        '/register',
        '/forgot-password',
        '/home',
        '/nope',
      ]) {
        await go(tester, location);
        expect(find.byType(SplashScreen), findsOneWidget, reason: location);
      }
    });

    testWidgets('signedOut starts on login and reaches only auth routes', (
      tester,
    ) async {
      await pumpApp(tester, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);

      await go(tester, Routes.register);
      expect(find.byType(RegisterScreen), findsOneWidget);

      await go(tester, Routes.home);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(find.byType(HomeScreen), findsNothing);

      await go(tester, Routes.forgotPassword);
      expect(find.byType(ForgotPasswordScreen), findsOneWidget);

      for (final location in ['/splash', '/home?x=1', '/home/', '/nope']) {
        await go(tester, location);
        expect(find.byType(LoginScreen), findsOneWidget, reason: location);
        expect(find.byType(HomeScreen), findsNothing, reason: location);
      }
    });

    testWidgets('auth screens link to each other', (tester) async {
      await pumpApp(tester, SessionStatus.signedOut);

      await tester.tap(find.text('Create account'));
      await tester.pumpAndSettle();
      expect(find.byType(RegisterScreen), findsOneWidget);

      await tester.tap(find.text('Back to log in'));
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);

      await tester.tap(find.text('Forgot password?'));
      await tester.pumpAndSettle();
      expect(find.byType(ForgotPasswordScreen), findsOneWidget);

      await tester.tap(find.text('Back to log in'));
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);
    });

    testWidgets('signedIn cannot stay on auth routes', (tester) async {
      await pumpApp(tester, SessionStatus.signedIn);
      expect(find.byType(HomeScreen), findsOneWidget);

      for (final location in [
        '/login',
        '/register',
        '/forgot-password',
        '/splash',
        '/nope',
      ]) {
        await go(tester, location);
        expect(find.byType(HomeScreen), findsOneWidget, reason: location);
      }
    });

    testWidgets('session changes drive navigation', (tester) async {
      final server = FakeServer()
        ..once(
          'POST',
          ApiPaths.login,
          (_) =>
              jsonResponse(200, tokenBody(accessToken('1'), refreshToken('1'))),
        )
        ..once('POST', ApiPaths.logout, (_) => noContent());
      final session = SessionManager(
        store: InMemoryTokenStore(),
        authApi: authApiFor(server.client),
        clock: FakeAuthClock(),
      );
      addTearDown(session.dispose);
      await tester.pumpWidget(
        VocaTogetherApp(config: _config, session: session),
      );
      await tester.pump();
      expect(find.byType(SplashScreen), findsOneWidget);

      await session.restore();
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);

      await go(tester, Routes.register);
      await session.signIn(email: 'a@b.c', password: 'pw');
      await tester.pumpAndSettle();
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(find.byType(RegisterScreen), findsNothing);

      await session.logout();
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(find.byType(HomeScreen), findsNothing);
    });

    testWidgets('the router stops listening when the app is removed', (
      tester,
    ) async {
      final session = _CountingSession(InMemoryTokenStore());
      addTearDown(session.dispose);

      await tester.pumpWidget(
        VocaTogetherApp(config: _config, session: session),
      );
      expect(session.listeners, 1);

      await tester.pumpWidget(const SizedBox());
      expect(session.listeners, 0);
    });

    testWidgets('replacing the session replaces the router', (tester) async {
      final first = _CountingSession(InMemoryTokenStore());
      addTearDown(first.dispose);
      final clock = FakeAuthClock();
      final second = _CountingSession(
        InMemoryTokenStore(raw: storedRaw(clock, '1')),
        clock: clock,
      );
      addTearDown(second.dispose);
      await second.restore();
      expect(second.status, SessionStatus.signedIn);

      await tester.pumpWidget(VocaTogetherApp(config: _config, session: first));
      await tester.pumpWidget(
        VocaTogetherApp(config: _config, session: second),
      );
      expect(first.listeners, 0);
      expect(second.listeners, 1);

      await second.logout();
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);
    });
  });
}

/// A manager over fakes, brought to [status] through restore.
Future<SessionManager> _managerAt(SessionStatus status) async {
  final clock = FakeAuthClock();
  final manager = SessionManager(
    store: InMemoryTokenStore(
      raw: status == SessionStatus.signedIn ? storedRaw(clock, '1') : null,
    ),
    authApi: authApiFor(FakeServer().client),
    clock: clock,
  );
  if (status != SessionStatus.unknown) await manager.restore();
  assert(manager.status == status);
  return manager;
}

class _CountingSession extends SessionManager {
  _CountingSession(TokenStore store, {FakeAuthClock? clock})
    : super(
        store: store,
        authApi: authApiFor(
          (FakeServer()..always('POST', ApiPaths.logout, (_) => noContent()))
              .client,
        ),
        clock: clock ?? FakeAuthClock(),
      );

  int listeners = 0;

  @override
  void addListener(VoidCallback listener) {
    listeners++;
    super.addListener(listener);
  }

  @override
  void removeListener(VoidCallback listener) {
    listeners--;
    super.removeListener(listener);
  }
}

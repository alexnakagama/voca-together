import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:vocatogether/app.dart';
import 'package:vocatogether/config.dart';
import 'package:vocatogether/router.dart';
import 'package:vocatogether/screens/forgot_password_screen.dart';
import 'package:vocatogether/screens/home_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/register_screen.dart';
import 'package:vocatogether/screens/splash_screen.dart';
import 'package:vocatogether/session.dart';

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
    Future<Session> pumpApp(WidgetTester tester, SessionStatus initial) async {
      final session = Session(initial: initial);
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
      final session = await pumpApp(tester, SessionStatus.unknown);
      expect(find.byType(SplashScreen), findsOneWidget);

      session.markSignedOut();
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);

      await go(tester, Routes.register);
      session.markSignedIn();
      await tester.pumpAndSettle();
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(find.byType(RegisterScreen), findsNothing);

      session.markSignedOut();
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(find.byType(HomeScreen), findsNothing);
    });

    testWidgets('the router stops listening when the app is removed', (
      tester,
    ) async {
      final session = _CountingSession();
      addTearDown(session.dispose);

      await tester.pumpWidget(
        VocaTogetherApp(config: _config, session: session),
      );
      expect(session.listeners, 1);

      await tester.pumpWidget(const SizedBox());
      expect(session.listeners, 0);
    });

    testWidgets('replacing the session replaces the router', (tester) async {
      final first = _CountingSession();
      addTearDown(first.dispose);
      final second = _CountingSession(initial: SessionStatus.signedIn);
      addTearDown(second.dispose);

      await tester.pumpWidget(VocaTogetherApp(config: _config, session: first));
      await tester.pumpWidget(
        VocaTogetherApp(config: _config, session: second),
      );
      expect(first.listeners, 0);
      expect(second.listeners, 1);

      second.markSignedOut();
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);
    });
  });
}

class _CountingSession extends Session {
  _CountingSession({super.initial});

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

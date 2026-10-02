import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/screens/home_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/register_screen.dart';
import 'package:vocatogether/session.dart';

import '../support/fakes.dart';
import 'harness.dart';

/// End-to-end navigation: screens express intent, the session changes, and
/// only the router's redirect decides where the user ends up.
void main() {
  FakeServer backend() => FakeServer()
    ..always(
      'POST',
      ApiPaths.login,
      (_) => jsonResponse(200, tokenBody(accessToken('1'), refreshToken('1'))),
    )
    ..always('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()))
    ..always('POST', ApiPaths.logout, (_) => noContent());

  Future<void> logIn(WidgetTester tester) async {
    await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
    await tester.enterText(field(l10n.passwordLabel), 'correct horse');
    await tapAndSettle(
      tester,
      find.widgetWithText(FilledButton, l10n.logInButton),
    );
  }

  testWidgets('log in → home → log out → log in', (tester) async {
    final app = await pumpApp(tester, server: backend());
    expect(app.location(tester), '/login');

    await logIn(tester);
    expect(app.session.status, SessionStatus.signedIn);
    expect(app.location(tester), '/home');
    expect(find.byType(HomeScreen), findsOneWidget);
    // Nothing to go back to: back leaves the app rather than reaching login.
    expect(
      Navigator.of(tester.element(find.byType(HomeScreen))).canPop(),
      isFalse,
    );

    await tapAndSettle(
      tester,
      find.widgetWithText(OutlinedButton, l10n.logOutButton),
    );
    expect(app.location(tester), '/login');
    expect(find.byType(LoginScreen), findsOneWidget);
    expect(find.byType(HomeScreen), findsNothing);
  });

  testWidgets('/home while signed out goes to log in', (tester) async {
    final app = await pumpApp(tester, server: backend());
    // As a screen would navigate; the redirect refuses.
    tester.element(find.byType(LoginScreen)).go('/home');
    await tester.pumpAndSettle();
    expect(app.location(tester), '/login');
    expect(find.byType(HomeScreen), findsNothing);
    expect(app.server.count(ApiPaths.me), 0);
  });

  testWidgets('a pushed screen, then back, returns to log in', (tester) async {
    final app = await pumpApp(tester, server: backend());
    await tapAndSettle(tester, find.text(l10n.createAccountLink));
    expect(find.byType(RegisterScreen), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(app.location(tester), '/login');
    expect(find.byType(LoginScreen), findsOneWidget);
  });

  testWidgets('signed in while on a pushed screen: home and nothing below', (
    tester,
  ) async {
    final app = await pumpApp(tester, server: backend());
    await tapAndSettle(tester, find.text(l10n.createAccountLink));
    expect(find.byType(RegisterScreen), findsOneWidget);

    // A sign-in from elsewhere (stage 6's Google flow, say) while the
    // register screen is on top.
    await app.session.signIn(email: 'ana@example.com', password: 'pw');
    await tester.pumpAndSettle();
    expect(app.location(tester), '/home');
    expect(find.byType(HomeScreen), findsOneWidget);
    expect(find.byType(RegisterScreen), findsNothing);
    expect(find.byType(LoginScreen), findsNothing);
    expect(
      Navigator.of(tester.element(find.byType(HomeScreen))).canPop(),
      isFalse,
    );
  });

  testWidgets('session ended elsewhere: home gives way to log in', (
    tester,
  ) async {
    final app = await pumpApp(tester, server: backend(), signedIn: true);
    expect(find.byType(HomeScreen), findsOneWidget);
    await app.session.logout();
    await tester.pumpAndSettle();
    expect(find.byType(LoginScreen), findsOneWidget);
  });
}

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/screens/home_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/session.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';

import '../support/fakes.dart';
import 'harness.dart';

Finder get _logOut => find.widgetWithText(OutlinedButton, l10n.logOutButton);
Finder get _retry => find.widgetWithText(FilledButton, l10n.tryAgain);

http.Response _me(http.Request _) => jsonResponse(
  200,
  meBody(id: 'user-SECRETID', createdAt: '2026-03-15T12:00:00Z'),
);

/// Every text on screen.
Iterable<String> _texts(WidgetTester tester) =>
    tester.widgetList<Text>(find.byType(Text)).map((t) => t.data ?? '');

void main() {
  testWidgets('loads the account: a labelled spinner, then email and date', (
    tester,
  ) async {
    final reply = Completer<http.Response>();
    final server = FakeServer()..once('GET', ApiPaths.me, (_) => reply.future);
    final handle = tester.ensureSemantics();
    await pumpApp(tester, server: server, signedIn: true, settle: false);

    expect(find.byType(HomeScreen), findsOneWidget);
    expect(find.bySemanticsLabel(l10n.homeLoading), findsOneWidget);
    expect(_logOut, findsOneWidget);

    reply.complete(_me(http.Request('GET', Uri())));
    await tester.pumpAndSettle();
    expect(find.text(l10n.homeSignedInTitle), findsOneWidget);
    expect(find.text(l10n.homeSignedInAs('ana@example.com')), findsOneWidget);
    expect(find.text('Member since March 15, 2026'), findsOneWidget);
    expect(find.bySemanticsLabel(l10n.homeLoading), findsNothing);
    // Only what the user needs: no id, and never a token.
    for (final text in _texts(tester)) {
      expect(text, isNot(contains('SECRETID')));
      expect(text, isNot(contains('vt_')));
    }
    handle.dispose();
  });

  group('failures keep the session and offer a retry', () {
    final cases = <String, (Responder, String)>{
      '500': (
        (_) => errorResponse(500, 'internal_error'),
        l10n.errorUnexpected,
      ),
      'network': (networkFailure, l10n.errorNetwork),
      '429': (
        (_) =>
            errorResponse(429, 'rate_limited', headers: {'retry-after': '10'}),
        'Too many attempts. Try again in 10 seconds.',
      ),
      'malformed 200': (
        (_) => jsonResponse(200, {'id': 'x'}),
        l10n.errorUnexpected,
      ),
    };
    cases.forEach((name, c) {
      final (responder, message) = c;
      testWidgets(name, (tester) async {
        final server = FakeServer()
          ..once('GET', ApiPaths.me, responder)
          ..once('GET', ApiPaths.me, _me);
        final app = await pumpApp(tester, server: server, signedIn: true);
        expect(find.text(message), findsOneWidget);
        expect(app.session.status, SessionStatus.signedIn);

        await tapAndSettle(tester, _retry);
        expect(find.text(message), findsNothing);
        expect(
          find.text(l10n.homeSignedInAs('ana@example.com')),
          findsOneWidget,
        );
        expect(server.count(ApiPaths.me), 2);
      });
    });

    testWidgets('timeout', (tester) async {
      final server = FakeServer()..once('GET', ApiPaths.me, neverAnswers);
      final app = await pumpApp(
        tester,
        server: server,
        signedIn: true,
        settle: false,
      );
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(app.session.status, SessionStatus.signedIn);
    });

    testWidgets('offline: the refresh probe fails, the session stays', (
      tester,
    ) async {
      // A 401 makes the session refresh; offline, its /healthz probe fails
      // and nothing that could rotate the tokens is sent (023).
      final server = FakeServer()
        ..once(
          'GET',
          ApiPaths.me,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..once('GET', ApiPaths.healthz, networkFailure);
      final app = await pumpApp(tester, server: server, signedIn: true);
      expect(find.text(l10n.errorNetwork), findsOneWidget);
      expect(app.session.status, SessionStatus.signedIn);
      expect(server.count(ApiPaths.refresh), 0);
    });

    testWidgets('401 even after a refresh: session message, then retry', (
      tester,
    ) async {
      final server = FakeServer()
        ..always('GET', ApiPaths.healthz, (_) => healthy())
        ..once(
          'GET',
          ApiPaths.me,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..once(
          'POST',
          ApiPaths.refresh,
          (_) =>
              jsonResponse(200, tokenBody(accessToken('2'), refreshToken('2'))),
        )
        ..once(
          'GET',
          ApiPaths.me,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..once(
          'POST',
          ApiPaths.refresh,
          (_) =>
              jsonResponse(200, tokenBody(accessToken('3'), refreshToken('3'))),
        )
        ..once('GET', ApiPaths.me, _me);
      final app = await pumpApp(tester, server: server, signedIn: true);
      expect(find.text(l10n.errorSessionInvalid), findsOneWidget);
      expect(app.session.status, SessionStatus.signedIn);

      await tapAndSettle(tester, _retry);
      expect(find.text(l10n.homeSignedInAs('ana@example.com')), findsOneWidget);
    });
  });

  testWidgets('the session ends during the load: log in, no error flash', (
    tester,
  ) async {
    final server = FakeServer()
      ..once(
        'GET',
        ApiPaths.me,
        (_) => errorResponse(401, 'invalid_access_token'),
      )
      ..once('GET', ApiPaths.healthz, (_) => healthy())
      ..once(
        'POST',
        ApiPaths.refresh,
        (_) => errorResponse(401, 'invalid_refresh_token'),
      );
    final app = await pumpApp(
      tester,
      server: server,
      signedIn: true,
      settle: false,
    );

    // Every frame until the router has moved on shows no error.
    for (var i = 0; i < 30; i++) {
      await tester.pump(const Duration(milliseconds: 50));
      expect(find.byType(FormErrorBanner), findsNothing);
    }
    await tester.pumpAndSettle();
    expect(app.session.status, SessionStatus.signedOut);
    expect(find.byType(LoginScreen), findsOneWidget);
    expect(find.byType(HomeScreen), findsNothing);
  });

  group('log out always returns to log in', () {
    final cases = <String, Responder>{
      '204': (_) => noContent(),
      '500': (_) => errorResponse(500, 'internal_error'),
      'network': networkFailure,
    };
    cases.forEach((name, responder) {
      testWidgets(name, (tester) async {
        final server = FakeServer()
          ..once('GET', ApiPaths.me, _me)
          ..once('POST', ApiPaths.logout, responder);
        final app = await pumpApp(tester, server: server, signedIn: true);
        await tapAndSettle(tester, _logOut);

        expect(find.byType(LoginScreen), findsOneWidget);
        expect(app.session.status, SessionStatus.signedOut);
        expect(app.store.raw, isNull);
        expect(server.count(ApiPaths.logout), 1);
        expect(app.location(tester), '/login');
      });
    });

    testWidgets('server never answers', (tester) async {
      final server = FakeServer()
        ..once('GET', ApiPaths.me, _me)
        ..once('POST', ApiPaths.logout, neverAnswers);
      final app = await pumpApp(tester, server: server, signedIn: true);
      await tapAndSettle(tester, _logOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(app.session.status, SessionStatus.signedOut);
      // Let the abandoned server call time out.
      await tester.pump(const Duration(seconds: 11));
    });

    testWidgets('while the account is still loading, without an error', (
      tester,
    ) async {
      final server = FakeServer()
        ..once('GET', ApiPaths.me, neverAnswers)
        ..once('POST', ApiPaths.logout, (_) => noContent());
      final app = await pumpApp(
        tester,
        server: server,
        signedIn: true,
        settle: false,
      );
      await tester.tap(_logOut);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(app.session.status, SessionStatus.signedOut);

      // The abandoned load times out later; nothing shows it.
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(tester.takeException(), isNull);
    });

    testWidgets('a double tap logs out once', (tester) async {
      final server = FakeServer()
        ..once('GET', ApiPaths.me, _me)
        ..always('POST', ApiPaths.logout, (_) => noContent());
      await pumpApp(tester, server: server, signedIn: true);
      await tester.tap(_logOut);
      await tester.tap(_logOut, warnIfMissed: false);
      await tester.pumpAndSettle();
      expect(server.count(ApiPaths.logout), 1);
    });
  });
}

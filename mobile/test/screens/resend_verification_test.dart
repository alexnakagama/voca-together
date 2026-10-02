import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/screens/resend_verification.dart';

import '../support/fakes.dart';
import 'harness.dart';

const _email = 'ana@example.com';

Finder get _button =>
    find.widgetWithText(OutlinedButton, l10n.resendVerificationButton);

/// Brings the log-in screen to its email-not-verified state, where the
/// section is shown.
Future<TestApp> _showSection(WidgetTester tester, FakeServer server) async {
  server.once(
    'POST',
    ApiPaths.login,
    (_) => errorResponse(403, 'email_not_verified'),
  );
  final app = await pumpApp(tester, server: server);
  await tester.enterText(field(l10n.emailLabel), _email);
  await tester.enterText(field(l10n.passwordLabel), 'correct horse');
  await tapAndSettle(
    tester,
    find.widgetWithText(FilledButton, l10n.logInButton),
  );
  expect(find.byType(ResendVerificationSection), findsOneWidget);
  return app;
}

void main() {
  testWidgets('202: a neutral confirmation; the button stays available', (
    tester,
  ) async {
    final server = FakeServer()
      ..always('POST', ApiPaths.resendVerification, (_) => accepted());
    await _showSection(tester, server);
    await tapAndSettle(tester, _button);
    expect(find.text(l10n.resendVerificationSent(_email)), findsOneWidget);
    expect(tester.widget<OutlinedButton>(_button).onPressed, isNotNull);

    // Asking again is the user's choice; the server's limit decides.
    await tapAndSettle(tester, _button);
    expect(server.count(ApiPaths.resendVerification), 2);
  });

  group('failures', () {
    final cases = <String, (Responder, String)>{
      '429': (
        (_) => errorResponse(
          429,
          'rate_limited',
          headers: {'retry-after': '1800'},
        ),
        'Too many attempts. Try again in 30 minutes.',
      ),
      '503': (
        (_) => errorResponse(503, 'service_unavailable'),
        l10n.errorUnavailableNoWait,
      ),
      '500': (
        (_) => errorResponse(500, 'internal_error'),
        l10n.errorUnexpected,
      ),
      'network': (networkFailure, l10n.errorNetwork),
    };
    cases.forEach((name, c) {
      final (responder, message) = c;
      testWidgets(name, (tester) async {
        final server = FakeServer()
          ..once('POST', ApiPaths.resendVerification, responder);
        await _showSection(tester, server);
        await tapAndSettle(tester, _button);
        expect(find.text(message), findsOneWidget);
        expect(find.text(l10n.resendVerificationSent(_email)), findsNothing);
        expect(server.count(ApiPaths.resendVerification), 1);
      });
    });

    testWidgets('timeout', (tester) async {
      final server = FakeServer()
        ..once('POST', ApiPaths.resendVerification, neverAnswers);
      await _showSection(tester, server);
      await tester.ensureVisible(_button);
      await tester.tap(_button);
      await tester.pump();
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(find.text(l10n.errorTimeout), findsOneWidget);
    });

    testWidgets('a success after a failure replaces the error', (tester) async {
      final server = FakeServer()
        ..once('POST', ApiPaths.resendVerification, networkFailure)
        ..once('POST', ApiPaths.resendVerification, (_) => accepted());
      await _showSection(tester, server);
      await tapAndSettle(tester, _button);
      expect(find.text(l10n.errorNetwork), findsOneWidget);
      await tapAndSettle(tester, _button);
      expect(find.text(l10n.errorNetwork), findsNothing);
      expect(find.text(l10n.resendVerificationSent(_email)), findsOneWidget);
    });
  });

  testWidgets('busy: a spinner and one request for a double tap', (
    tester,
  ) async {
    final reply = Completer<http.Response>();
    final server = FakeServer()
      ..once('POST', ApiPaths.resendVerification, (_) => reply.future);
    await _showSection(tester, server);
    await tester.ensureVisible(_button);
    await tester.tap(_button);
    await tester.tap(_button, warnIfMissed: false);
    await tester.pump();
    await tester.tap(_button, warnIfMissed: false);
    await tester.pump();
    expect(server.count(ApiPaths.resendVerification), 1);
    expect(
      find.descendant(
        of: find.byType(ResendVerificationSection),
        matching: find.byType(CircularProgressIndicator),
      ),
      findsOneWidget,
    );

    reply.complete(accepted());
    await tester.pumpAndSettle();
    expect(find.text(l10n.resendVerificationSent(_email)), findsOneWidget);
  });

  testWidgets('removed mid-request: the late answer is ignored', (
    tester,
  ) async {
    final reply = Completer<http.Response>();
    final server = FakeServer()
      ..once('POST', ApiPaths.resendVerification, (_) => reply.future);
    await _showSection(tester, server);
    await tester.ensureVisible(_button);
    await tester.tap(_button);
    await tester.pump();

    // Editing the address leaves the not-verified state.
    await tester.enterText(field(l10n.emailLabel), 'other@example.com');
    await tester.pump();
    expect(find.byType(ResendVerificationSection), findsNothing);

    reply.complete(accepted());
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(find.text(l10n.resendVerificationSent(_email)), findsNothing);
  });
}

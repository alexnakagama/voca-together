import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/screens/forgot_password_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';

import '../support/fakes.dart';
import 'harness.dart';

Finder get _submit =>
    find.widgetWithText(FilledButton, l10n.forgotPasswordButton);

Future<TestApp> _open(WidgetTester tester, FakeServer server) async {
  final app = await pumpApp(tester, server: server);
  await tapAndSettle(tester, find.text(l10n.forgotPasswordLink));
  expect(find.byType(ForgotPasswordScreen), findsOneWidget);
  return app;
}

/// Every text on screen, in order.
List<String> _texts(WidgetTester tester) => [
  for (final t in tester.widgetList<Text>(find.byType(Text))) t.data ?? '',
];

void main() {
  testWidgets('an empty email is refused without a request', (tester) async {
    final app = await _open(tester, FakeServer());
    await tapAndSettle(tester, _submit);
    expect(errorOf(tester, l10n.emailLabel), l10n.emailRequired);
    expect(hasFocus(tester, l10n.emailLabel), isTrue);
    expect(app.server.requests, isEmpty);
  });

  testWidgets('202: the neutral confirmation, the address sent as typed', (
    tester,
  ) async {
    final server = FakeServer()
      ..once('POST', ApiPaths.forgotPassword, (_) => accepted());
    await _open(tester, server);
    await tester.enterText(field(l10n.emailLabel), ' Ana@Example.com ');
    await tapAndSettle(tester, _submit);

    expect(jsonDecode(server.to(ApiPaths.forgotPassword).single.body), {
      'email': ' Ana@Example.com ',
    });
    expect(
      find.text(l10n.forgotPasswordSent(' Ana@Example.com ')),
      findsOneWidget,
    );
    expect(find.byType(TextField), findsNothing);

    await tapAndSettle(tester, find.text(l10n.backToLogIn));
    expect(find.byType(LoginScreen), findsOneWidget);
  });

  testWidgets('the confirmation is the same for any address', (tester) async {
    // The server answers 202 for unknown, unverified, verified and
    // passwordless accounts alike; the screen adds nothing of its own.
    Future<List<String>> confirmationFor(String email) async {
      final server = FakeServer()
        ..once('POST', ApiPaths.forgotPassword, (_) => accepted());
      await _open(tester, server);
      await tester.enterText(field(l10n.emailLabel), email);
      await tapAndSettle(tester, _submit);
      return _texts(tester).map((t) => t.replaceAll(email, '<email>')).toList();
    }

    final known = await confirmationFor('known@example.com');
    final unknown = await confirmationFor('nobody@example.com');
    expect(known, unknown);
  });

  group('failures', () {
    final cases = <String, (Responder, String)>{
      '429': (
        (_) =>
            errorResponse(429, 'rate_limited', headers: {'retry-after': '30'}),
        'Too many attempts. Try again in 30 seconds.',
      ),
      '503': (
        (_) => errorResponse(
          503,
          'service_unavailable',
          headers: {'retry-after': '5'},
        ),
        'VocaTogether is busy right now. Try again in 5 seconds.',
      ),
      '500': (
        (_) => errorResponse(500, 'internal_error'),
        l10n.errorUnexpected,
      ),
      'network': (networkFailure, l10n.errorNetwork),
      'malformed 202': (
        (_) => jsonResponse(202, {'status': 'queued'}),
        l10n.errorUnexpected,
      ),
    };
    cases.forEach((name, c) {
      final (responder, message) = c;
      testWidgets(name, (tester) async {
        final server = FakeServer()
          ..once('POST', ApiPaths.forgotPassword, responder);
        await _open(tester, server);
        await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
        await tapAndSettle(tester, _submit);
        expect(find.text(message), findsOneWidget);
        expect(
          textFieldOf(tester, field(l10n.emailLabel)).controller!.text,
          'ana@example.com',
        );
      });
    });

    testWidgets('422 email:invalid goes on the field', (tester) async {
      final server = FakeServer()
        ..once(
          'POST',
          ApiPaths.forgotPassword,
          (_) => jsonResponse(422, {
            'error': {
              'code': 'validation_failed',
              'fields': [
                {'field': 'email', 'code': 'invalid'},
              ],
            },
          }),
        );
      await _open(tester, server);
      await tester.enterText(field(l10n.emailLabel), 'nope');
      await tapAndSettle(tester, _submit);
      expect(errorOf(tester, l10n.emailLabel), l10n.errorEmailInvalid);
      expect(hasFocus(tester, l10n.emailLabel), isTrue);
    });

    testWidgets('timeout', (tester) async {
      final server = FakeServer()
        ..once('POST', ApiPaths.forgotPassword, neverAnswers);
      await _open(tester, server);
      await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
      await tester.tap(_submit);
      await tester.pump();
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(find.text(l10n.errorTimeout), findsOneWidget);
    });
  });

  testWidgets('busy: one request however often it is submitted', (
    tester,
  ) async {
    final reply = Completer<http.Response>();
    final server = FakeServer()
      ..once('POST', ApiPaths.forgotPassword, (_) => reply.future);
    await _open(tester, server);
    await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
    await tester.showKeyboard(field(l10n.emailLabel));
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pump();
    await tester.tap(_submit, warnIfMissed: false);
    await tester.pump();
    expect(server.count(ApiPaths.forgotPassword), 1);

    reply.complete(accepted());
    await tester.pumpAndSettle();
    expect(
      find.text(l10n.forgotPasswordSent('ana@example.com')),
      findsOneWidget,
    );
  });
}

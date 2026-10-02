import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/register_screen.dart';

import '../support/fakes.dart';
import 'harness.dart';

const _email = 'ana@example.com';
const _password = 'correct horse battery';

Finder get _submit => find.widgetWithText(FilledButton, l10n.registerButton);

Future<TestApp> _openRegister(WidgetTester tester, FakeServer server) async {
  final app = await pumpApp(tester, server: server);
  await tapAndSettle(tester, find.text(l10n.createAccountLink));
  expect(find.byType(RegisterScreen), findsOneWidget);
  return app;
}

Future<void> _fill(
  WidgetTester tester, {
  String email = _email,
  String password = _password,
  String? confirm,
}) async {
  await tester.enterText(field(l10n.emailLabel), email);
  await tester.enterText(field(l10n.passwordLabel), password);
  await tester.enterText(field(l10n.confirmPasswordLabel), confirm ?? password);
  await tester.pump();
}

http.Response _invalid(String field, String code) => jsonResponse(422, {
  'error': {
    'code': 'validation_failed',
    'fields': [
      {'field': field, 'code': code},
    ],
  },
});

void main() {
  group('client checks, with no request', () {
    testWidgets('every field empty', (tester) async {
      final app = await _openRegister(tester, FakeServer());
      await tapAndSettle(tester, _submit);
      expect(errorOf(tester, l10n.emailLabel), l10n.emailRequired);
      expect(errorOf(tester, l10n.passwordLabel), l10n.passwordRequired);
      expect(
        errorOf(tester, l10n.confirmPasswordLabel),
        l10n.confirmPasswordRequired,
      );
      expect(hasFocus(tester, l10n.emailLabel), isTrue);
      expect(app.server.requests, isEmpty);
    });

    testWidgets('confirmation differs: error on confirm, focused', (
      tester,
    ) async {
      final app = await _openRegister(tester, FakeServer());
      await _fill(tester, confirm: '$_password!');
      await tapAndSettle(tester, _submit);
      expect(
        errorOf(tester, l10n.confirmPasswordLabel),
        l10n.passwordsDoNotMatch,
      );
      expect(errorOf(tester, l10n.passwordLabel), isNull);
      expect(hasFocus(tester, l10n.confirmPasswordLabel), isTrue);
      expect(app.server.requests, isEmpty);
    });

    testWidgets('no length or policy check on the client', (tester) async {
      final server = FakeServer()
        ..once(
          'POST',
          ApiPaths.register,
          (_) => _invalid('password', 'too_short'),
        );
      await _openRegister(tester, server);
      await _fill(tester, password: 'a');
      await tapAndSettle(tester, _submit);
      expect(server.count(ApiPaths.register), 1);
    });
  });

  testWidgets('202: the check-email view, passwords cleared, values sent raw', (
    tester,
  ) async {
    final server = FakeServer()
      ..once('POST', ApiPaths.register, (_) => accepted());
    final app = await _openRegister(tester, server);
    const email = ' Ana@Example.com';
    const password = 'ﬁne pässword ';
    await _fill(tester, email: email, password: password);
    await tapAndSettle(tester, _submit);

    expect(jsonDecode(server.to(ApiPaths.register).single.body), {
      'email': email,
      'password': password,
    });
    expect(find.text(l10n.checkEmailTitle), findsOneWidget);
    expect(find.text(l10n.registerSent(email)), findsOneWidget);
    expect(find.text(l10n.resendVerificationButton), findsOneWidget);
    expect(find.byType(TextField), findsNothing);
    // The neutral wording never claims an account was or wasn't created.
    expect(l10n.registerSent(email), isNot(contains('created')));
    expect(app.location(tester), '/register');

    await tapAndSettle(tester, find.text(l10n.backToLogIn));
    expect(find.byType(LoginScreen), findsOneWidget);
  });

  testWidgets('the check-email view can resend', (tester) async {
    final server = FakeServer()
      ..once('POST', ApiPaths.register, (_) => accepted())
      ..once('POST', ApiPaths.resendVerification, (_) => accepted());
    await _openRegister(tester, server);
    await _fill(tester);
    await tapAndSettle(tester, _submit);
    await tapAndSettle(tester, find.text(l10n.resendVerificationButton));
    expect(jsonDecode(server.to(ApiPaths.resendVerification).single.body), {
      'email': _email,
    });
    expect(find.text(l10n.resendVerificationSent(_email)), findsOneWidget);
  });

  group('422 errors land on their fields', () {
    final cases = <(String, String), String>{
      ('email', 'invalid'): l10n.errorEmailInvalid,
      ('password', 'required'): l10n.passwordRequired,
      ('password', 'too_short'): l10n.errorPasswordTooShort,
      ('password', 'too_long'): l10n.errorPasswordTooLong,
      ('password', 'too_common'): l10n.errorPasswordTooCommon,
      ('password', 'same_as_email'): l10n.errorPasswordSameAsEmail,
    };
    cases.forEach((input, message) {
      final (name, code) = input;
      testWidgets('$name:$code', (tester) async {
        final server = FakeServer()
          ..once('POST', ApiPaths.register, (_) => _invalid(name, code));
        await _openRegister(tester, server);
        await _fill(tester);
        await tapAndSettle(tester, _submit);

        final label = name == 'email' ? l10n.emailLabel : l10n.passwordLabel;
        expect(errorOf(tester, label), message);
        expect(hasFocus(tester, label), isTrue);
        expect(errorOf(tester, l10n.confirmPasswordLabel), isNull);
        // Nothing is cleared: the user fixes the one field.
        final password = textFieldOf(tester, field(l10n.passwordLabel));
        expect(password.controller!.text, _password);
        // No numbers: the policy belongs to the server.
        expect(message, isNot(matches(RegExp(r'\d'))));
      });
    });

    testWidgets('an unknown field gives the generic banner', (tester) async {
      final server = FakeServer()
        ..once('POST', ApiPaths.register, (_) => _invalid('username', 'taken'));
      await _openRegister(tester, server);
      await _fill(tester);
      await tapAndSettle(tester, _submit);
      expect(find.text(l10n.errorCheckInput), findsOneWidget);
    });
  });

  group('other failures keep every input', () {
    final cases = <String, (Responder, String)>{
      '429': (
        (_) => errorResponse(
          429,
          'rate_limited',
          headers: {'retry-after': '3600'},
        ),
        'Too many attempts. Try again in 1 hour.',
      ),
      '503': (
        (_) => errorResponse(503, 'service_unavailable'),
        l10n.errorUnavailableNoWait,
      ),
      '500': (
        (_) => errorResponse(500, 'internal_error'),
        l10n.errorUnexpected,
      ),
      '400': (
        (_) => errorResponse(400, 'invalid_request'),
        l10n.errorUnexpected,
      ),
      'network': (networkFailure, l10n.errorNetwork),
      'unexpected 200': (
        (_) => jsonResponse(200, {'status': 'accepted'}),
        l10n.errorUnexpected,
      ),
    };
    cases.forEach((name, c) {
      final (responder, message) = c;
      testWidgets(name, (tester) async {
        final server = FakeServer()..once('POST', ApiPaths.register, responder);
        await _openRegister(tester, server);
        await _fill(tester);
        await tapAndSettle(tester, _submit);
        expect(find.text(message), findsOneWidget);
        expect(find.text(l10n.checkEmailTitle), findsNothing);
        for (final label in [l10n.passwordLabel, l10n.confirmPasswordLabel]) {
          expect(textFieldOf(tester, field(label)).controller!.text, _password);
        }
        expect(server.count(ApiPaths.register), 1);
      });
    });

    testWidgets('timeout', (tester) async {
      final server = FakeServer()
        ..once('POST', ApiPaths.register, neverAnswers);
      await _openRegister(tester, server);
      await _fill(tester);
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
      ..once('POST', ApiPaths.register, (_) => reply.future);
    await _openRegister(tester, server);
    await _fill(tester);
    await tester.showKeyboard(field(l10n.confirmPasswordLabel));
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pump();
    await tester.tap(_submit, warnIfMissed: false);
    await tester.pump();
    expect(server.count(ApiPaths.register), 1);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    reply.complete(accepted());
    await tester.pumpAndSettle();
    expect(find.text(l10n.checkEmailTitle), findsOneWidget);
  });

  testWidgets('leaving mid-request is harmless', (tester) async {
    final reply = Completer<http.Response>();
    final server = FakeServer()
      ..once('POST', ApiPaths.register, (_) => reply.future);
    await _openRegister(tester, server);
    await _fill(tester);
    await tester.tap(_submit);
    await tester.pump();
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.byType(LoginScreen), findsOneWidget);

    reply.complete(accepted());
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(find.byType(LoginScreen), findsOneWidget);
  });
}

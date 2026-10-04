import 'dart:async';
import 'dart:convert';

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

const _email = 'ana@example.com';
const _password = 'correct horse battery';

Future<void> _fill(
  WidgetTester tester, {
  String email = _email,
  String password = _password,
}) async {
  await tester.enterText(field(l10n.emailLabel), email);
  await tester.enterText(field(l10n.passwordLabel), password);
  await tester.pump();
}

String _passwordText(WidgetTester tester) =>
    textFieldOf(tester, field(l10n.passwordLabel)).controller!.text;

String _emailText(WidgetTester tester) =>
    textFieldOf(tester, field(l10n.emailLabel)).controller!.text;

Finder get _submit => find.widgetWithText(FilledButton, l10n.logInButton);

void main() {
  testWidgets('starts empty, with nothing focused and no errors', (
    tester,
  ) async {
    await pumpApp(tester);
    expect(find.byType(LoginScreen), findsOneWidget);
    expect(_emailText(tester), isEmpty);
    expect(hasFocus(tester, l10n.emailLabel), isFalse);
    expect(hasFocus(tester, l10n.passwordLabel), isFalse);
    expect(errorOf(tester, l10n.emailLabel), isNull);
    expect(find.text(l10n.forgotPasswordLink), findsOneWidget);
    expect(find.text(l10n.createAccountLink), findsOneWidget);
    // This app was built without Google configuration.
    expect(find.text(l10n.continueWithGoogle), findsNothing);
  });

  group('empty fields are refused without a request', () {
    testWidgets('both empty: both errors, email focused', (tester) async {
      final app = await pumpApp(tester);
      await tapAndSettle(tester, _submit);
      expect(errorOf(tester, l10n.emailLabel), l10n.emailRequired);
      expect(errorOf(tester, l10n.passwordLabel), l10n.passwordRequired);
      expect(hasFocus(tester, l10n.emailLabel), isTrue);
      expect(app.server.requests, isEmpty);
    });

    testWidgets('blank email', (tester) async {
      final app = await pumpApp(tester);
      await _fill(tester, email: '   ');
      await tapAndSettle(tester, _submit);
      expect(errorOf(tester, l10n.emailLabel), l10n.emailRequired);
      expect(errorOf(tester, l10n.passwordLabel), isNull);
      expect(app.server.requests, isEmpty);
    });

    testWidgets('empty password: password focused', (tester) async {
      final app = await pumpApp(tester);
      await _fill(tester, password: '');
      await tapAndSettle(tester, _submit);
      expect(errorOf(tester, l10n.emailLabel), isNull);
      expect(errorOf(tester, l10n.passwordLabel), l10n.passwordRequired);
      expect(hasFocus(tester, l10n.passwordLabel), isTrue);
      expect(app.server.requests, isEmpty);
    });

    testWidgets('editing a field clears its error', (tester) async {
      await pumpApp(tester);
      await tapAndSettle(tester, _submit);
      await tester.enterText(field(l10n.emailLabel), 'a');
      await tester.pump();
      expect(errorOf(tester, l10n.emailLabel), isNull);
      expect(errorOf(tester, l10n.passwordLabel), l10n.passwordRequired);
    });
  });

  testWidgets('sends the input exactly as typed, then reaches home', (
    tester,
  ) async {
    final server = FakeServer()
      ..once(
        'POST',
        ApiPaths.login,
        (_) =>
            jsonResponse(200, tokenBody(accessToken('1'), refreshToken('1'))),
      )
      ..always('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()));
    final app = await pumpApp(tester, server: server);

    const email = '  Ana@Example.COM ';
    const password = ' pässwörd ﬁ ';
    await _fill(tester, email: email, password: password);
    await tapAndSettle(tester, _submit);

    expect(jsonDecode(server.to(ApiPaths.login).single.body), {
      'email': email,
      'password': password,
    });
    expect(app.session.status, SessionStatus.signedIn);
    expect(find.byType(HomeScreen), findsOneWidget);
    expect(find.byType(LoginScreen), findsNothing);
    expect(app.location(tester), '/home');
  });

  testWidgets('busy: one request however often it is submitted', (
    tester,
  ) async {
    final reply = Completer<http.Response>();
    final server = FakeServer()
      ..once('POST', ApiPaths.login, (_) => reply.future);
    await pumpApp(tester, server: server);
    await _fill(tester);

    // Done twice in one frame (before the screen can rebuild), then taps.
    await tester.showKeyboard(field(l10n.passwordLabel));
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pump();
    await tester.tap(_submit, warnIfMissed: false);
    await tester.pump();

    expect(server.count(ApiPaths.login), 1);
    expect(
      find.descendant(
        of: _submit,
        matching: find.byType(CircularProgressIndicator),
      ),
      findsOneWidget,
    );
    expect(textFieldOf(tester, field(l10n.emailLabel)).enabled, isFalse);
    expect(textFieldOf(tester, field(l10n.passwordLabel)).enabled, isFalse);
    final links = tester.widget<TextButton>(
      find.widgetWithText(TextButton, l10n.createAccountLink),
    );
    expect(links.onPressed, isNull);

    reply.complete(errorResponse(401, 'invalid_credentials'));
    await tester.pumpAndSettle();
    expect(find.text(l10n.errorInvalidCredentials), findsOneWidget);
    expect(textFieldOf(tester, field(l10n.emailLabel)).enabled, isTrue);
    expect(find.byType(CircularProgressIndicator), findsNothing);
  });

  testWidgets('the keyboard: next moves to the password, done submits', (
    tester,
  ) async {
    final server = FakeServer()
      ..once(
        'POST',
        ApiPaths.login,
        (_) => errorResponse(401, 'invalid_credentials'),
      );
    await pumpApp(tester, server: server);
    await tester.showKeyboard(field(l10n.emailLabel));
    await tester.enterText(field(l10n.emailLabel), _email);
    await tester.testTextInput.receiveAction(TextInputAction.next);
    await tester.pump();
    expect(hasFocus(tester, l10n.passwordLabel), isTrue);

    await tester.enterText(field(l10n.passwordLabel), _password);
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();
    expect(server.count(ApiPaths.login), 1);
    expect(find.text(l10n.errorInvalidCredentials), findsOneWidget);
  });

  testWidgets(
    '401: generic message, password cleared and focused, email kept',
    (tester) async {
      final server = FakeServer()
        ..once(
          'POST',
          ApiPaths.login,
          (_) => errorResponse(401, 'invalid_credentials'),
        );
      final app = await pumpApp(tester, server: server);
      await _fill(tester);
      await tapAndSettle(tester, _submit);

      expect(find.text(l10n.errorInvalidCredentials), findsOneWidget);
      expect(_passwordText(tester), isEmpty);
      expect(_emailText(tester), _email);
      expect(hasFocus(tester, l10n.passwordLabel), isTrue);
      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
    },
  );

  group('403 email_not_verified', () {
    Future<TestApp> unverified(WidgetTester tester, FakeServer server) async {
      server.once(
        'POST',
        ApiPaths.login,
        (_) => errorResponse(403, 'email_not_verified'),
      );
      final app = await pumpApp(tester, server: server);
      await _fill(tester);
      await tapAndSettle(tester, _submit);
      return app;
    }

    testWidgets('offers to resend the link to the address used', (
      tester,
    ) async {
      final server = FakeServer()
        ..once('POST', ApiPaths.resendVerification, (_) => accepted());
      await unverified(tester, server);

      expect(find.text(l10n.emailNotVerifiedNotice(_email)), findsOneWidget);
      expect(find.text(l10n.errorEmailNotVerified), findsNothing);
      expect(_passwordText(tester), isEmpty);
      expect(_emailText(tester), _email);

      await tapAndSettle(tester, find.text(l10n.resendVerificationButton));
      expect(jsonDecode(server.to(ApiPaths.resendVerification).single.body), {
        'email': _email,
      });
      expect(find.text(l10n.resendVerificationSent(_email)), findsOneWidget);
    });

    testWidgets('editing the email leaves the state', (tester) async {
      await unverified(tester, FakeServer());
      await tester.enterText(field(l10n.emailLabel), 'other@example.com');
      await tester.pump();
      expect(find.text(l10n.resendVerificationButton), findsNothing);
      expect(find.text(l10n.emailNotVerifiedNotice(_email)), findsNothing);
    });

    testWidgets('a new attempt replaces it', (tester) async {
      final server = FakeServer();
      await unverified(tester, server);
      server.once(
        'POST',
        ApiPaths.login,
        (_) => errorResponse(401, 'invalid_credentials'),
      );
      await tester.enterText(field(l10n.passwordLabel), 'wrong');
      await tapAndSettle(tester, _submit);
      expect(find.text(l10n.resendVerificationButton), findsNothing);
      expect(find.text(l10n.errorInvalidCredentials), findsOneWidget);
    });
  });

  group('failures that say nothing about the credentials keep both fields', () {
    final cases = <String, (Responder, String)>{
      '429 with Retry-After': (
        (_) =>
            errorResponse(429, 'rate_limited', headers: {'retry-after': '120'}),
        'Too many attempts. Try again in 2 minutes.',
      ),
      '429 without Retry-After': (
        (_) => errorResponse(429, 'rate_limited'),
        l10n.errorRateLimitedNoWait,
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
      '502 from a proxy': (
        (_) => http.Response('<html>Bad gateway</html>', 502),
        l10n.errorUnexpected,
      ),
      '400': (
        (_) => errorResponse(400, 'invalid_request'),
        l10n.errorUnexpected,
      ),
      'unknown code': (
        (_) => errorResponse(418, 'brand_new_code'),
        l10n.errorUnexpected,
      ),
      'malformed 200': (
        (_) => jsonResponse(200, {'access_token': 'nope'}),
        l10n.errorUnexpected,
      ),
      'network': (networkFailure, l10n.errorNetwork),
    };

    cases.forEach((name, c) {
      final (responder, message) = c;
      testWidgets(name, (tester) async {
        final server = FakeServer()..once('POST', ApiPaths.login, responder);
        final app = await pumpApp(tester, server: server);
        await _fill(tester);
        await tapAndSettle(tester, _submit);

        expect(find.text(message), findsOneWidget);
        expect(_emailText(tester), _email);
        expect(_passwordText(tester), _password);
        expect(app.session.status, SessionStatus.signedOut);
        // Nothing is retried automatically.
        expect(server.count(ApiPaths.login), 1);
      });
    });

    testWidgets('timeout', (tester) async {
      final server = FakeServer()..once('POST', ApiPaths.login, neverAnswers);
      await pumpApp(tester, server: server);
      await _fill(tester);
      await tester.tap(_submit);
      await tester.pump();
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(_passwordText(tester), _password);
    });

    testWidgets('a retry is the user\'s and sends again', (tester) async {
      final server = FakeServer()
        ..once('POST', ApiPaths.login, networkFailure)
        ..once(
          'POST',
          ApiPaths.login,
          (_) =>
              jsonResponse(200, tokenBody(accessToken('1'), refreshToken('1'))),
        )
        ..always('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()));
      await pumpApp(tester, server: server);
      await _fill(tester);
      await tapAndSettle(tester, _submit);
      expect(find.text(l10n.errorNetwork), findsOneWidget);
      await tapAndSettle(tester, _submit);
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(server.count(ApiPaths.login), 2);
    });
  });

  group('422 field errors', () {
    testWidgets('land on their fields, focus the first, clear on edit', (
      tester,
    ) async {
      final server = FakeServer()
        ..once(
          'POST',
          ApiPaths.login,
          (_) => jsonResponse(422, {
            'error': {
              'code': 'validation_failed',
              'fields': [
                {'field': 'email', 'code': 'invalid'},
              ],
            },
          }),
        );
      await pumpApp(tester, server: server);
      await _fill(tester, email: 'not-an-address');
      await tapAndSettle(tester, _submit);

      expect(errorOf(tester, l10n.emailLabel), l10n.errorEmailInvalid);
      expect(errorOf(tester, l10n.passwordLabel), isNull);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(hasFocus(tester, l10n.emailLabel), isTrue);
      expect(_passwordText(tester), _password);

      await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
      await tester.pump();
      expect(errorOf(tester, l10n.emailLabel), isNull);
    });

    testWidgets('an unknown field shows the generic banner', (tester) async {
      final server = FakeServer()
        ..once(
          'POST',
          ApiPaths.login,
          (_) => jsonResponse(422, {
            'error': {
              'code': 'validation_failed',
              'fields': [
                {'field': 'nickname', 'code': 'invalid'},
              ],
            },
          }),
        );
      await pumpApp(tester, server: server);
      await _fill(tester);
      await tapAndSettle(tester, _submit);
      expect(find.text(l10n.errorCheckInput), findsOneWidget);
    });
  });

  testWidgets('links open register and forgot password, and back returns', (
    tester,
  ) async {
    final app = await pumpApp(tester);
    await tapAndSettle(tester, find.text(l10n.createAccountLink));
    expect(app.location(tester), '/register');
    // Android's back button.
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.byType(LoginScreen), findsOneWidget);

    await tapAndSettle(tester, find.text(l10n.forgotPasswordLink));
    expect(app.location(tester), '/forgot-password');
  });
}

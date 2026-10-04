import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/auth/google_identity_exception.dart';
import 'package:vocatogether/ui/widgets/google_sign_in_button.dart';

import '../support/fakes.dart';
import 'harness.dart';

/// Secrets with distinctive markers, so any copy can be found.
const _email = 'leak.email@example.com';
const _password = 'LEAK-password-123';
final _access = accessToken('LEAKaccess');
final _refresh = refreshToken('LEAKrefresh');
const _idToken = 'LEAKidtoken.payload.signature';
const _idToken2 = 'LEAKidtokenTwo.payload.signature';

/// An error body that echoes secrets, as a broken proxy might.
http.Response _echo(int status) => http.Response(
  '{"error":{"code":"invalid_credentials","detail":"$_password $_email '
  '$_access $_idToken"}}',
  status,
  headers: {'content-type': 'application/json'},
);

void main() {
  testWidgets(
    'UI flows print nothing and keep secrets out of routes and text',
    (tester) async {
      final printed = <String>[];
      final locations = <String>[];
      final originalDebugPrint = debugPrint;
      debugPrint = (String? message, {int? wrapWidth}) {
        if (message != null) printed.add(message);
      };

      try {
        await runZoned(
          () => _runFlows(tester, locations),
          zoneSpecification: ZoneSpecification(
            print: (self, parent, zone, line) => printed.add(line),
          ),
        );
      } finally {
        debugPrint = originalDebugPrint;
      }

      expect(printed, isEmpty);
      expect(locations, isNotEmpty);
      for (final location in locations) {
        expect(location, isNot(contains('LEAK')), reason: location);
        expect(location, isNot(contains('leak.email')), reason: location);
        expect(location, isNot(contains('@')), reason: location);
        expect(Uri.parse(location).hasQuery, isFalse, reason: location);
      }
    },
  );
}

/// Every text on screen.
Iterable<String> _texts(WidgetTester tester) => [
  for (final t in tester.widgetList<Text>(find.byType(Text))) t.data ?? '',
  for (final t in tester.widgetList<RichText>(find.byType(RichText)))
    t.text.toPlainText(),
];

/// No token, no password and no server text is ever rendered; the email
/// appears only where the screen deliberately echoes the user's own input.
void _checkScreen(WidgetTester tester, {bool emailAllowed = false}) {
  for (final text in _texts(tester)) {
    expect(text, isNot(contains(_password)), reason: text);
    expect(text, isNot(contains('LEAK')), reason: text);
    expect(text, isNot(contains('vt_')), reason: text);
    expect(text, isNot(contains('detail')), reason: text);
    if (!emailAllowed) {
      expect(text, isNot(contains(_email)), reason: text);
    }
  }
}

Future<void> _runFlows(WidgetTester tester, List<String> locations) async {
  final server = FakeServer()
    ..once('POST', ApiPaths.login, (_) => _echo(401))
    ..once(
      'POST',
      ApiPaths.login,
      (_) => errorResponse(403, 'email_not_verified'),
    )
    ..once('POST', ApiPaths.resendVerification, (_) => _echo(500))
    ..once('POST', ApiPaths.register, (_) => _echo(422))
    ..once('POST', ApiPaths.register, (_) => accepted())
    ..once('POST', ApiPaths.forgotPassword, (_) => _echo(503))
    ..once('POST', ApiPaths.forgotPassword, (_) => accepted())
    ..once(
      'POST',
      ApiPaths.login,
      (_) => jsonResponse(200, tokenBody(_access, _refresh)),
    )
    ..once('GET', ApiPaths.me, (_) => _echo(500))
    ..once('GET', ApiPaths.me, (_) => jsonResponse(200, meBody(email: _email)))
    ..once('POST', ApiPaths.logout, (_) => _echo(500))
    // Google: an echoing 409, an echoing 500, then a session.
    ..once(
      'POST',
      ApiPaths.google,
      (_) => http.Response(
        '{"error":{"code":"account_exists","detail":"$_email $_idToken"}}',
        409,
        headers: {'content-type': 'application/json'},
      ),
    )
    ..once('POST', ApiPaths.google, (_) => _echo(500))
    ..once(
      'POST',
      ApiPaths.google,
      (_) => jsonResponse(200, tokenBody(_access, _refresh)),
    )
    ..once('GET', ApiPaths.me, (_) => jsonResponse(200, meBody(email: _email)))
    ..once('POST', ApiPaths.logout, (_) => _echo(500));
  final google = FakeGoogleIdentity()
    ..next(_idToken)
    ..fail(const GoogleIdentityException(GoogleIdentityFailure.misconfigured))
    ..fail(const GoogleIdentityException(GoogleIdentityFailure.cancelled))
    ..next(_idToken2)
    ..next(_idToken);
  final app = await pumpApp(tester, server: server, google: google);
  void record() => locations.add(app.location(tester));
  final logIn = find.widgetWithText(FilledButton, l10n.logInButton);

  Future<void> attemptLogIn() async {
    await tester.enterText(field(l10n.emailLabel), _email);
    await tester.enterText(field(l10n.passwordLabel), _password);
    await tapAndSettle(tester, logIn);
    record();
  }

  // Log in: refused with an echoing body, then not verified + resend.
  await attemptLogIn();
  _checkScreen(tester);
  await attemptLogIn();
  _checkScreen(tester, emailAllowed: true);
  await tapAndSettle(tester, find.text(l10n.resendVerificationButton));
  _checkScreen(tester, emailAllowed: true);

  // Register: an echoing 422, then accepted.
  await tester.enterText(field(l10n.emailLabel), '');
  await tapAndSettle(tester, find.text(l10n.createAccountLink));
  record();
  await tester.enterText(field(l10n.emailLabel), _email);
  await tester.enterText(field(l10n.passwordLabel), _password);
  await tester.enterText(field(l10n.confirmPasswordLabel), _password);
  final register = find.widgetWithText(FilledButton, l10n.registerButton);
  await tapAndSettle(tester, register);
  _checkScreen(tester);
  await tapAndSettle(tester, register);
  record();
  _checkScreen(tester, emailAllowed: true);
  await tapAndSettle(tester, find.text(l10n.backToLogIn));
  record();

  // Forgot password: an echoing 503, then accepted.
  await tapAndSettle(tester, find.text(l10n.forgotPasswordLink));
  record();
  await tester.enterText(field(l10n.emailLabel), _email);
  final send = find.widgetWithText(FilledButton, l10n.forgotPasswordButton);
  await tapAndSettle(tester, send);
  _checkScreen(tester);
  await tapAndSettle(tester, send);
  _checkScreen(tester, emailAllowed: true);
  await tapAndSettle(tester, find.text(l10n.backToLogIn));
  record();

  // Sign in, home fails with an echoing body, retry, log out.
  await attemptLogIn();
  _checkScreen(tester);
  await tapAndSettle(tester, find.widgetWithText(FilledButton, l10n.tryAgain));
  record();
  _checkScreen(tester, emailAllowed: true);
  await tapAndSettle(
    tester,
    find.widgetWithText(OutlinedButton, l10n.logOutButton),
  );
  record();
  _checkScreen(tester);
  expect(app.store.raw, isNull);

  // Google sign-in: refused with an echoing body, Google failing, the
  // chooser closed, a server error, then a session and log out.
  Future<void> continueWithGoogle() async {
    await tapAndSettle(tester, find.byType(GoogleSignInButton));
    record();
    _checkScreen(tester);
  }

  await continueWithGoogle();
  expect(find.text(l10n.errorAccountExists), findsOneWidget);
  await continueWithGoogle();
  expect(find.text(l10n.errorGoogleUnavailable), findsOneWidget);
  await continueWithGoogle();
  await continueWithGoogle();
  expect(server.count(ApiPaths.google), 2);
  await tapAndSettle(tester, find.byType(GoogleSignInButton));
  record();
  _checkScreen(tester, emailAllowed: true);
  expect(app.location(tester), '/home');
  expect(app.store.raw, isNot(contains('LEAKidtoken')));
  await tapAndSettle(
    tester,
    find.widgetWithText(OutlinedButton, l10n.logOutButton),
  );
  record();
  _checkScreen(tester);
  expect(app.store.raw, isNull);
  // The ID tokens went to the Google endpoint's body and nowhere else.
  for (final r in server.requests) {
    final carries =
        r.body.contains('LEAKidtoken') ||
        r.url.toString().contains('LEAKidtoken') ||
        r.headers.values.any((v) => v.contains('LEAKidtoken'));
    if (r.url.path == ApiPaths.google) {
      expect(r.headers.containsKey('Authorization'), isFalse);
    } else {
      expect(carries, isFalse, reason: r.url.path);
    }
  }
}

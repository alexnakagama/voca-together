import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/auth/google_identity_exception.dart';
import 'package:vocatogether/auth/token_store.dart';
import 'package:vocatogether/screens/home_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/register_screen.dart';
import 'package:vocatogether/session.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/google_sign_in_button.dart';

import '../support/fakes.dart';
import 'harness.dart';

Finder get _google => find.byType(GoogleSignInButton);
Finder get _logIn => find.widgetWithText(FilledButton, l10n.logInButton);
Finder get _progress => find.bySemanticsLabel(l10n.googleSignInProgress);

bool _googleEnabled(WidgetTester tester) =>
    tester.widget<GoogleSignInButton>(_google).onPressed != null;

http.Response _ok([String marker = '1']) =>
    jsonResponse(200, tokenBody(accessToken(marker), refreshToken(marker)));

FakeServer _backend() =>
    FakeServer()
      ..always('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()));

/// Taps the Google button and lets everything finish.
Future<void> _tapGoogle(WidgetTester tester) => tapAndSettle(tester, _google);

/// The two screens that offer Google sign-in, and how to reach each.
final _screens = <String, Future<void> Function(WidgetTester)>{
  'log in': (_) async {},
  'register': (t) => tapAndSettle(t, find.text(l10n.createAccountLink)),
};

void main() {
  group('without Google configuration', () {
    _screens.forEach((name, open) {
      testWidgets('$name shows no Google button', (tester) async {
        await pumpApp(tester);
        await open(tester);
        expect(_google, findsNothing);
        expect(find.text(l10n.googleSignInDivider), findsNothing);
      });
    });
  });

  _screens.forEach((name, open) {
    group(name, () {
      testWidgets('a Google sign-in opens home', (tester) async {
        final google = FakeGoogleIdentity()..next(googleIdToken('one'));
        final server = _backend()..once('POST', ApiPaths.google, (_) => _ok());
        final app = await pumpApp(tester, server: server, google: google);
        await open(tester);
        expect(find.text(l10n.continueWithGoogle), findsOneWidget);

        await _tapGoogle(tester);

        expect(find.byType(HomeScreen), findsOneWidget);
        expect(app.location(tester), '/home');
        expect(google.calls, 1);
        expect(
          server.to(ApiPaths.google).single.body,
          '{"id_token":"${googleIdToken('one')}"}',
        );
        expect(app.store.raw, isNot(contains('GID')));
        expect(tester.takeException(), isNull);
      });

      testWidgets('closing the chooser shows nothing and leaves the form '
          'usable', (tester) async {
        final google = FakeGoogleIdentity()
          ..fail(
            const GoogleIdentityException(GoogleIdentityFailure.cancelled),
          );
        final app = await pumpApp(tester, google: google);
        await open(tester);

        await _tapGoogle(tester);

        expect(app.server.requests, isEmpty);
        expect(find.byType(FormErrorBanner), findsNothing);
        expect(_googleEnabled(tester), isTrue);
        expect(_progress, findsNothing);
        expect(textFieldOf(tester, field(l10n.emailLabel)).enabled, isTrue);
        expect(app.session.status, SessionStatus.signedOut);
      });

      final refusals = <String, (int, String)>{
        'invalid_google_token': (401, l10n.errorGoogleRejected),
        'google_email_unusable': (403, l10n.errorGoogleEmailUnusable),
        'account_exists': (409, l10n.errorAccountExists),
      };
      refusals.forEach((code, value) {
        final (status, message) = value;
        testWidgets('$status $code: its message, and nothing else changes', (
          tester,
        ) async {
          final google = FakeGoogleIdentity()..next(googleIdToken('one'));
          final server = FakeServer()
            ..once('POST', ApiPaths.google, (_) => errorResponse(status, code));
          final app = await pumpApp(tester, server: server, google: google);
          await open(tester);
          await tester.enterText(field(l10n.emailLabel), 'ana@example.com');

          await _tapGoogle(tester);

          expect(find.text(message), findsOneWidget);
          expect(app.session.status, SessionStatus.signedOut);
          expect(app.store.writes, 0);
          expect(server.requests, hasLength(1), reason: 'no retry, no linking');
          expect(_googleEnabled(tester), isTrue);
          // The form is as the user left it.
          expect(
            textFieldOf(tester, field(l10n.emailLabel)).controller!.text,
            'ana@example.com',
          );
          expect(
            find.byType(name == 'log in' ? LoginScreen : RegisterScreen),
            findsOneWidget,
          );
        });
      });
    });
  });

  group('provider failures', () {
    for (final failure in GoogleIdentityFailure.values) {
      if (failure == GoogleIdentityFailure.cancelled) continue;
      testWidgets('${failure.name}: one message, no request', (tester) async {
        final google = FakeGoogleIdentity()
          ..fail(GoogleIdentityException(failure));
        final app = await pumpApp(tester, google: google);

        await _tapGoogle(tester);

        expect(find.text(l10n.errorGoogleUnavailable), findsOneWidget);
        expect(app.server.requests, isEmpty);
        expect(_googleEnabled(tester), isTrue);
      });
    }

    testWidgets('a programming error is shown as the generic message, not '
        'thrown', (tester) async {
      final google = FakeGoogleIdentity()..fail(StateError('boom'));
      await pumpApp(tester, google: google);

      await _tapGoogle(tester);

      expect(tester.takeException(), isNull);
      expect(find.text(l10n.errorUnexpected), findsOneWidget);
      expect(_googleEnabled(tester), isTrue);
    });
  });

  group('backend failures keep the user on the screen, able to retry', () {
    final cases = <String, (Responder, String)>{
      '429 with a wait': (
        (_) =>
            errorResponse(429, 'rate_limited', headers: {'retry-after': '30'}),
        l10n.errorRateLimited(l10n.waitSeconds(30)),
      ),
      '429 without a wait': (
        (_) => errorResponse(429, 'rate_limited'),
        l10n.errorRateLimitedNoWait,
      ),
      '503': (
        (_) => errorResponse(
          503,
          'service_unavailable',
          headers: {'retry-after': '5'},
        ),
        l10n.errorUnavailable(l10n.waitSeconds(5)),
      ),
      '500': (
        (_) => errorResponse(500, 'internal_error'),
        l10n.errorUnexpected,
      ),
      'a proxy error page': (
        (_) => http.Response('<html>502</html>', 502),
        l10n.errorUnexpected,
      ),
      'a network error': (networkFailure, l10n.errorNetwork),
      'a 200 without tokens': (
        (_) => jsonResponse(200, {'token_type': 'Bearer'}),
        l10n.errorUnexpected,
      ),
    };
    cases.forEach((name, value) {
      final (responder, message) = value;
      testWidgets(name, (tester) async {
        final google = FakeGoogleIdentity()
          ..next(googleIdToken('first'))
          ..next(googleIdToken('second'));
        final server = _backend()..once('POST', ApiPaths.google, responder);
        final app = await pumpApp(tester, server: server, google: google);

        await _tapGoogle(tester);

        expect(find.text(message), findsOneWidget);
        expect(server.count(ApiPaths.google), 1, reason: 'no automatic retry');
        expect(google.calls, 1);
        expect(app.session.status, SessionStatus.signedOut);

        // The retry is the user's, and it asks Google again.
        server.once('POST', ApiPaths.google, (_) => _ok());
        await _tapGoogle(tester);
        expect(
          server.to(ApiPaths.google).last.body,
          '{"id_token":"${googleIdToken('second')}"}',
        );
        expect(find.byType(HomeScreen), findsOneWidget);
      });
    });

    testWidgets('a timeout', (tester) async {
      final google = FakeGoogleIdentity()..next(googleIdToken('one'));
      final server = FakeServer()..once('POST', ApiPaths.google, neverAnswers);
      await pumpApp(tester, server: server, google: google);

      await tester.tap(_google);
      await tester.pump();
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();

      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(server.count(ApiPaths.google), 1);
      expect(_googleEnabled(tester), isTrue);
    });

    testWidgets('tokens that cannot be stored', (tester) async {
      final google = FakeGoogleIdentity()..next(googleIdToken('one'));
      final server = FakeServer()..once('POST', ApiPaths.google, (_) => _ok());
      final app = await pumpApp(tester, server: server, google: google);
      app.store.writeError = const TokenStoreException('write');

      await _tapGoogle(tester);

      expect(find.text(l10n.errorUnexpected), findsOneWidget);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(app.session.status, SessionStatus.signedOut);
    });
  });

  group('while Google sign-in runs', () {
    testWidgets(
      'repeated taps start one attempt and the whole form is locked',
      (tester) async {
        final chooser = Completer<String>();
        final google = FakeGoogleIdentity()..wait(chooser);
        final server = _backend()..once('POST', ApiPaths.google, (_) => _ok());
        final app = await pumpApp(tester, server: server, google: google);
        await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
        await tester.enterText(field(l10n.passwordLabel), 'correct horse');

        // Twice in one frame, then again after the rebuild.
        await tester.tap(_google);
        await tester.tap(_google, warnIfMissed: false);
        await tester.pump();
        await tester.tap(_google, warnIfMissed: false);
        await tester.pump();

        expect(google.calls, 1);
        expect(_googleEnabled(tester), isFalse);
        expect(_progress, findsOneWidget);
        expect(textFieldOf(tester, field(l10n.emailLabel)).enabled, isFalse);
        expect(textFieldOf(tester, field(l10n.passwordLabel)).enabled, isFalse);
        expect(tester.widget<FilledButton>(_logIn).onPressed, isNull);
        expect(
          find.descendant(
            of: _logIn,
            matching: find.byType(CircularProgressIndicator),
          ),
          findsNothing,
          reason: 'the email button is unavailable, not working',
        );
        for (final link in [l10n.forgotPasswordLink, l10n.createAccountLink]) {
          expect(
            tester
                .widget<TextButton>(find.widgetWithText(TextButton, link))
                .onPressed,
            isNull,
          );
        }
        expect(server.requests, isEmpty);

        chooser.complete(googleIdToken('one'));
        await tester.pumpAndSettle();
        expect(find.byType(HomeScreen), findsOneWidget);
        expect(server.count(ApiPaths.google), 1);
        expect(server.count(ApiPaths.login), 0);
        expect(app.session.status, SessionStatus.signedIn);
      },
    );

    testWidgets('the keyboard cannot submit the email form', (tester) async {
      final chooser = Completer<String>();
      final google = FakeGoogleIdentity()..wait(chooser);
      final app = await pumpApp(tester, google: google);
      await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
      await tester.enterText(field(l10n.passwordLabel), 'correct horse');
      await tester.showKeyboard(field(l10n.passwordLabel));

      // In the same frame as the tap, before the rebuild that disables the
      // field: only the submit handler's own guard is in the way.
      await tester.tap(_google);
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pump();
      expect(tester.takeException(), isNull);

      expect(app.server.requests, isEmpty);
      chooser.completeError(
        const GoogleIdentityException(GoogleIdentityFailure.cancelled),
      );
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(tester.widget<FilledButton>(_logIn).onPressed, isNotNull);
    });

    testWidgets('the form unlocks after a failure, and the error clears on '
        'the next attempt', (tester) async {
      final chooser = Completer<String>();
      final google = FakeGoogleIdentity()
        ..fail(const GoogleIdentityException(GoogleIdentityFailure.unknown))
        ..wait(chooser);
      await pumpApp(tester, google: google);

      await _tapGoogle(tester);
      expect(find.text(l10n.errorGoogleUnavailable), findsOneWidget);
      expect(tester.widget<FilledButton>(_logIn).onPressed, isNotNull);

      await tester.ensureVisible(_google);
      await tester.tap(_google);
      await tester.pump();
      expect(find.text(l10n.errorGoogleUnavailable), findsNothing);

      chooser.completeError(
        const GoogleIdentityException(GoogleIdentityFailure.cancelled),
      );
      await tester.pumpAndSettle();
    });

    testWidgets("starting it clears the form's own error", (tester) async {
      final google = FakeGoogleIdentity()
        ..fail(const GoogleIdentityException(GoogleIdentityFailure.cancelled));
      final server = FakeServer()
        ..once(
          'POST',
          ApiPaths.login,
          (_) => errorResponse(401, 'invalid_credentials'),
        );
      await pumpApp(tester, server: server, google: google);
      await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
      await tester.enterText(field(l10n.passwordLabel), 'wrong');
      await tapAndSettle(tester, _logIn);
      expect(find.text(l10n.errorInvalidCredentials), findsOneWidget);

      await _tapGoogle(tester);

      expect(find.text(l10n.errorInvalidCredentials), findsNothing);
    });
  });

  testWidgets('the Google button is off while an email log in is running', (
    tester,
  ) async {
    final reply = Completer<http.Response>();
    final google = FakeGoogleIdentity();
    final server = FakeServer()
      ..once('POST', ApiPaths.login, (_) => reply.future);
    await pumpApp(tester, server: server, google: google);
    await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
    await tester.enterText(field(l10n.passwordLabel), 'correct horse');
    await tester.tap(_logIn);
    await tester.pump();

    expect(_googleEnabled(tester), isFalse);
    await tester.tap(_google, warnIfMissed: false);
    await tester.pump();
    expect(google.calls, 0);

    reply.complete(errorResponse(401, 'invalid_credentials'));
    await tester.pumpAndSettle();
    expect(_googleEnabled(tester), isTrue);
  });

  testWidgets('starting a new email attempt clears the Google error', (
    tester,
  ) async {
    final google = FakeGoogleIdentity()..next(googleIdToken('one'));
    final server = FakeServer()
      ..once(
        'POST',
        ApiPaths.google,
        (_) => errorResponse(409, 'account_exists'),
      )
      ..once(
        'POST',
        ApiPaths.login,
        (_) => errorResponse(401, 'invalid_credentials'),
      );
    await pumpApp(tester, server: server, google: google);
    await _tapGoogle(tester);
    expect(find.text(l10n.errorAccountExists), findsOneWidget);

    await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
    await tester.enterText(field(l10n.passwordLabel), 'wrong');
    await tapAndSettle(tester, _logIn);

    expect(find.text(l10n.errorAccountExists), findsNothing);
    expect(find.text(l10n.errorInvalidCredentials), findsOneWidget);
    expect(find.byType(FormErrorBanner), findsOneWidget);
  });

  group('leaving the screen while Google sign-in runs', () {
    Future<(TestApp, Completer<String>, FakeServer)> startOnRegister(
      WidgetTester tester,
    ) async {
      final chooser = Completer<String>();
      final google = FakeGoogleIdentity()..wait(chooser);
      final server = _backend();
      final app = await pumpApp(tester, server: server, google: google);
      await tapAndSettle(tester, find.text(l10n.createAccountLink));
      await tester.ensureVisible(_google);
      await tester.tap(_google);
      await tester.pump();
      // Android's back button pops the register screen.
      await tester.binding.handlePopRoute();
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(find.byType(RegisterScreen), findsNothing);
      expect(find.byType(LoginScreen), findsOneWidget);
      return (app, chooser, server);
    }

    testWidgets('a late failure touches nothing', (tester) async {
      final (app, chooser, server) = await startOnRegister(tester);

      chooser.completeError(
        const GoogleIdentityException(GoogleIdentityFailure.unknown),
      );
      await tester.pumpAndSettle();

      expect(tester.takeException(), isNull);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(server.requests, isEmpty);
      expect(app.session.status, SessionStatus.signedOut);
    });

    testWidgets('a late success still signs in, through the router', (
      tester,
    ) async {
      final (app, chooser, server) = await startOnRegister(tester);
      server.once('POST', ApiPaths.google, (_) => _ok());

      chooser.complete(googleIdToken('one'));
      await tester.pumpAndSettle();

      expect(tester.takeException(), isNull);
      expect(app.location(tester), '/home');
      expect(find.byType(HomeScreen), findsOneWidget);
    });

    testWidgets('a second attempt from the screen below is refused without a '
        'second chooser', (tester) async {
      final (app, chooser, server) = await startOnRegister(tester);

      await tester.ensureVisible(_google);
      await tester.tap(_google);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));

      expect(app.google!.calls, 1);
      expect(tester.takeException(), isNull);
      expect(find.text(l10n.errorUnexpected), findsOneWidget);

      server.once('POST', ApiPaths.google, (_) => _ok());
      chooser.complete(googleIdToken('one'));
      await tester.pumpAndSettle();
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(server.count(ApiPaths.google), 1);
    });
  });

  testWidgets('register: the check-your-email view has no Google button', (
    tester,
  ) async {
    final server = FakeServer()
      ..once('POST', ApiPaths.register, (_) => accepted());
    await pumpApp(tester, server: server, google: FakeGoogleIdentity());
    await tapAndSettle(tester, find.text(l10n.createAccountLink));
    await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
    await tester.enterText(field(l10n.passwordLabel), 'correct horse');
    await tester.enterText(field(l10n.confirmPasswordLabel), 'correct horse');
    await tapAndSettle(
      tester,
      find.widgetWithText(FilledButton, l10n.registerButton),
    );
    expect(find.text(l10n.checkEmailTitle), findsOneWidget);
    expect(_google, findsNothing);
  });

  testWidgets('sign in with Google, log out, sign in again: two tokens, and '
      "Google's state is cleared at logout", (tester) async {
    final google = FakeGoogleIdentity()
      ..next(googleIdToken('first'))
      ..next(googleIdToken('second'));
    final server = _backend()
      ..once('POST', ApiPaths.google, (_) => _ok('1'))
      ..once('POST', ApiPaths.logout, (_) => noContent())
      ..once('POST', ApiPaths.google, (_) => _ok('2'));
    final app = await pumpApp(tester, server: server, google: google);

    await _tapGoogle(tester);
    expect(find.byType(HomeScreen), findsOneWidget);
    await tapAndSettle(
      tester,
      find.widgetWithText(OutlinedButton, l10n.logOutButton),
    );
    expect(find.byType(LoginScreen), findsOneWidget);
    expect(google.clears, 1);
    await _tapGoogle(tester);

    expect(find.byType(HomeScreen), findsOneWidget);
    expect(server.to(ApiPaths.google).map((r) => r.body), [
      '{"id_token":"${googleIdToken('first')}"}',
      '{"id_token":"${googleIdToken('second')}"}',
    ]);
    expect(app.store.session!.accessToken, accessToken('2'));
  });
}

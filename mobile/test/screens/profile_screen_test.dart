import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/screens/home_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/profile_screen.dart';
import 'package:vocatogether/session.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/form_notice_banner.dart';

import '../support/fakes.dart';
import 'harness.dart';

Finder get _open => find.widgetWithText(OutlinedButton, l10n.profileButton);
Finder get _save => find.widgetWithText(FilledButton, l10n.profileSaveButton);
Finder get _retry => find.widgetWithText(FilledButton, l10n.tryAgain);
Finder get _name => field(l10n.displayNameLabel);
Finder get _bio => field(l10n.bioLabel);

String _text(WidgetTester tester, Finder f) =>
    textFieldOf(tester, f).controller!.text;

/// A backend for a signed-in user on home, with no languages chosen (the
/// profile's languages section loads them); the profile calls are scripted
/// by each test.
FakeServer _backend() => FakeServer()
  ..always('GET', ApiPaths.languages, (_) => jsonResponse(200, catalogBody()))
  ..always(
    'GET',
    ApiPaths.myLanguages,
    (_) => jsonResponse(200, languagesBody()),
  )
  ..always('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()))
  ..always('GET', ApiPaths.healthz, (_) => healthy())
  ..always('POST', ApiPaths.logout, (_) => noContent());

http.Response _validation(List<(String, String)> fields) => jsonResponse(422, {
  'error': {
    'code': 'validation_failed',
    'fields': [
      for (final (field, code) in fields) {'field': field, 'code': code},
    ],
  },
});

/// Signs in on home and opens the profile, whose load must be scripted.
Future<TestApp> _openProfile(
  WidgetTester tester,
  FakeServer server, {
  bool settle = true,
}) async {
  final app = await pumpApp(tester, server: server, signedIn: true);
  await tester.ensureVisible(_open);
  await tester.tap(_open);
  if (settle) {
    await tester.pumpAndSettle();
  } else {
    await tester.pump();
    await tester.pump();
  }
  expect(find.byType(ProfileScreen), findsOneWidget);
  return app;
}

/// Opens the profile of a user who hasn't saved one: an empty form.
Future<TestApp> _openEmpty(WidgetTester tester, FakeServer server) {
  server.once('GET', ApiPaths.profile, (_) => noProfile());
  return _openProfile(tester, server);
}

void main() {
  group('loading', () {
    testWidgets('a labelled spinner, then an empty form for a new profile', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.profile, (_) => reply.future);
      final handle = tester.ensureSemantics();
      final app = await _openProfile(tester, server, settle: false);

      expect(app.location(tester), '/profile');
      expect(find.bySemanticsLabel(l10n.profileLoading), findsOneWidget);
      expect(_save, findsNothing);

      reply.complete(noProfile());
      await tester.pumpAndSettle();
      expect(find.bySemanticsLabel(l10n.profileLoading), findsNothing);
      expect(find.text(l10n.profileCreateHeading), findsOneWidget);
      // The user is told what others will see before writing anything.
      expect(find.text(l10n.profileVisibilityNotice), findsOneWidget);
      expect(_text(tester, _name), '');
      expect(_text(tester, _bio), '');
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(find.byType(FormNoticeBanner), findsNothing);
      handle.dispose();
    });

    testWidgets('a saved profile fills the form', (tester) async {
      final server = _backend()
        ..once(
          'GET',
          ApiPaths.profile,
          (_) => jsonResponse(
            200,
            profileBody(displayName: 'Ana López', bio: 'Line one\nLine two'),
          ),
        );
      await _openProfile(tester, server);

      expect(find.text(l10n.profileEditHeading), findsOneWidget);
      expect(find.text(l10n.profileCreateHeading), findsNothing);
      expect(_text(tester, _name), 'Ana López');
      expect(_text(tester, _bio), 'Line one\nLine two');
      expect(find.text(l10n.profileVisibilityNotice), findsOneWidget);
    });

    group('a failed load keeps the session and offers a retry', () {
      final cases = <String, (Responder, String)>{
        '500': (
          (_) => errorResponse(500, 'internal_error'),
          l10n.errorUnexpected,
        ),
        'network': (networkFailure, l10n.errorNetwork),
        '429': (
          (_) => errorResponse(
            429,
            'rate_limited',
            headers: {'retry-after': '10'},
          ),
          'Too many attempts. Try again in 10 seconds.',
        ),
        'malformed 200': (
          (_) => jsonResponse(200, {'display_name': 'Ana'}),
          l10n.errorUnexpected,
        ),
        // Not the backend's "none saved": never shown as an empty profile.
        'a 404 without the code': (
          (_) => http.Response('Not Found', 404),
          l10n.errorUnexpected,
        ),
      };
      cases.forEach((name, c) {
        final (responder, message) = c;
        testWidgets(name, (tester) async {
          final server = _backend()
            ..once('GET', ApiPaths.profile, responder)
            ..once(
              'GET',
              ApiPaths.profile,
              (_) => jsonResponse(200, profileBody(displayName: 'Ana')),
            );
          final app = await _openProfile(tester, server);

          expect(find.text(message), findsOneWidget);
          expect(_save, findsNothing);
          expect(_name, findsNothing);
          expect(app.session.status, SessionStatus.signedIn);
          expect(app.location(tester), '/profile');

          await tapAndSettle(tester, _retry);
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(_text(tester, _name), 'Ana');
          expect(server.count(ApiPaths.profile), 2);
        });
      });
    });

    testWidgets('a session that ended while loading shows no error', (
      tester,
    ) async {
      final server = _backend()
        ..once(
          'GET',
          ApiPaths.profile,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..once(
          'POST',
          ApiPaths.refresh,
          (_) => errorResponse(401, 'invalid_refresh_token'),
        );
      final app = await pumpApp(tester, server: server, signedIn: true);
      await tester.ensureVisible(_open);
      await tester.tap(_open);
      await tester.pumpAndSettle();

      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(find.byType(FormErrorBanner), findsNothing);
    });
  });

  group('saving', () {
    testWidgets('an empty name is caught here and nothing is sent', (
      tester,
    ) async {
      final server = _backend();
      await _openEmpty(tester, server);

      await tester.enterText(_name, '   ');
      await tester.enterText(_bio, 'Something');
      await tapAndSettle(tester, _save);

      expect(errorOf(tester, l10n.displayNameLabel), l10n.displayNameRequired);
      expect(hasFocus(tester, l10n.displayNameLabel), isTrue);
      expect(
        server.to(ApiPaths.profile).where((r) => r.method == 'PUT'),
        isEmpty,
      );
      // Typing clears the error.
      await tester.enterText(_name, 'A');
      await tester.pump();
      expect(errorOf(tester, l10n.displayNameLabel), isNull);
    });

    testWidgets('sends the text as typed and shows what the server stored', (
      tester,
    ) async {
      final server = _backend()
        ..once(
          'PUT',
          ApiPaths.profile,
          (_) => jsonResponse(
            200,
            profileBody(displayName: 'Ana López', bio: 'Hi'),
          ),
        );
      await _openEmpty(tester, server);

      await tester.enterText(_name, '  Ana   López ');
      await tester.enterText(_bio, 'Hi \n');
      await tapAndSettle(tester, _save);

      final put = server.requests.singleWhere((r) => r.method == 'PUT');
      expect(jsonDecode(put.body), {
        'display_name': '  Ana   López ',
        'bio': 'Hi \n',
      });
      // The form now holds the stored, normalized text.
      expect(_text(tester, _name), 'Ana López');
      expect(_text(tester, _bio), 'Hi');
      expect(find.text(l10n.profileSaved), findsOneWidget);
      expect(find.text(l10n.profileEditHeading), findsOneWidget);
      expect(find.byType(FormErrorBanner), findsNothing);

      // The confirmation goes once the user edits again.
      await tester.enterText(_bio, 'Hi again');
      await tester.pump();
      expect(find.text(l10n.profileSaved), findsNothing);
    });

    testWidgets('an existing profile is saved the same way', (tester) async {
      final server = _backend()
        ..once(
          'GET',
          ApiPaths.profile,
          (_) => jsonResponse(200, profileBody(displayName: 'Ana', bio: 'Old')),
        )
        ..once(
          'PUT',
          ApiPaths.profile,
          (_) => jsonResponse(200, profileBody(displayName: 'Ana', bio: '')),
        );
      await _openProfile(tester, server);

      await tester.enterText(_bio, '');
      await tapAndSettle(tester, _save);

      final put = server.requests.singleWhere((r) => r.method == 'PUT');
      expect(jsonDecode(put.body), {'display_name': 'Ana', 'bio': ''});
      expect(find.text(l10n.profileSaved), findsOneWidget);
    });

    testWidgets('while saving the form is locked and a second tap is ignored', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('PUT', ApiPaths.profile, (_) => reply.future);
      await _openEmpty(tester, server);
      await tester.enterText(_name, 'Ana');

      await tester.ensureVisible(_save);
      await tester.tap(_save);
      await tester.pump();
      expect(textFieldOf(tester, _name).enabled, isFalse);
      expect(textFieldOf(tester, _bio).enabled, isFalse);
      await tester.tap(_save, warnIfMissed: false);
      await tester.pump();
      await tester.tap(_save, warnIfMissed: false);
      await tester.pump();
      expect(server.requests.where((r) => r.method == 'PUT'), hasLength(1));

      reply.complete(jsonResponse(200, profileBody(displayName: 'Ana')));
      await tester.pumpAndSettle();
      expect(textFieldOf(tester, _name).enabled, isTrue);
      expect(find.text(l10n.profileSaved), findsOneWidget);
    });

    testWidgets('the name\'s keyboard action moves on to the bio, not a save', (
      tester,
    ) async {
      final server = _backend();
      await _openEmpty(tester, server);

      await tester.tap(_name);
      await tester.enterText(_name, 'Ana');
      await tester.testTextInput.receiveAction(TextInputAction.next);
      await tester.pump();
      expect(hasFocus(tester, l10n.bioLabel), isTrue);
      expect(server.requests.where((r) => r.method == 'PUT'), isEmpty);
    });

    testWidgets('server validation goes on the fields and keeps the text', (
      tester,
    ) async {
      final server = _backend()
        ..once(
          'PUT',
          ApiPaths.profile,
          (_) =>
              _validation([('display_name', 'too_long'), ('bio', 'invalid')]),
        )
        ..once(
          'PUT',
          ApiPaths.profile,
          (_) => _validation([('bio', 'too_long')]),
        );
      await _openEmpty(tester, server);
      await tester.enterText(_name, 'A very long name');
      await tester.enterText(_bio, 'Some text');

      await tapAndSettle(tester, _save);
      expect(
        errorOf(tester, l10n.displayNameLabel),
        l10n.errorDisplayNameTooLong,
      );
      expect(errorOf(tester, l10n.bioLabel), l10n.errorBioInvalid);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(find.byType(FormNoticeBanner), findsNothing);
      expect(hasFocus(tester, l10n.displayNameLabel), isTrue);
      expect(_text(tester, _name), 'A very long name');
      expect(_text(tester, _bio), 'Some text');

      // Only the bio is wrong the second time: it gets the focus.
      await tapAndSettle(tester, _save);
      expect(errorOf(tester, l10n.displayNameLabel), isNull);
      expect(errorOf(tester, l10n.bioLabel), l10n.errorBioTooLong);
      expect(hasFocus(tester, l10n.bioLabel), isTrue);
    });

    // The app sets no limit of its own (the server owns the rules, 027): a
    // long text goes out whole and the server's answer names the field.
    testWidgets('a pasted text far over the limit is sent whole and gets the '
        'too-long error on its field', (tester) async {
      final server = _backend()
        ..once(
          'PUT',
          ApiPaths.profile,
          (_) => _validation([('bio', 'too_long')]),
        );
      await _openEmpty(tester, server);
      final pasted = List.filled(1000, 'a line of text').join('\n');
      expect(pasted.length, greaterThan(10000));
      await tester.enterText(_name, 'Ana');
      await tester.enterText(_bio, pasted);
      await tester.pumpAndSettle();
      await tapAndSettle(tester, _save);

      final put = server.requests.singleWhere((r) => r.method == 'PUT');
      expect((jsonDecode(put.body) as Map<String, Object?>)['bio'], pasted);
      expect(errorOf(tester, l10n.bioLabel), l10n.errorBioTooLong);
      expect(find.text(l10n.errorUnexpected), findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(_text(tester, _bio), pasted);
    });

    group('other failures keep the text and can be retried', () {
      final cases = <String, (Responder, String)>{
        '429': (
          (_) =>
              errorResponse(429, 'rate_limited', headers: {'retry-after': '6'}),
          'Too many attempts. Try again in 6 seconds.',
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
        '400': (
          (_) => errorResponse(400, 'invalid_request'),
          l10n.errorUnexpected,
        ),
        'network': (networkFailure, l10n.errorNetwork),
        'a 201': (
          (_) => jsonResponse(201, profileBody()),
          l10n.errorUnexpected,
        ),
      };
      cases.forEach((name, c) {
        final (responder, message) = c;
        testWidgets(name, (tester) async {
          final server = _backend()
            ..once('PUT', ApiPaths.profile, responder)
            ..once(
              'PUT',
              ApiPaths.profile,
              (_) =>
                  jsonResponse(200, profileBody(displayName: 'Ana', bio: 'Hi')),
            );
          final app = await _openEmpty(tester, server);
          await tester.enterText(_name, 'Ana');
          await tester.enterText(_bio, 'Hi');

          await tapAndSettle(tester, _save);
          expect(find.text(message), findsOneWidget);
          expect(find.byType(FormNoticeBanner), findsNothing);
          expect(_text(tester, _name), 'Ana');
          expect(_text(tester, _bio), 'Hi');
          expect(app.session.status, SessionStatus.signedIn);
          // Nothing was retried on its own.
          expect(server.requests.where((r) => r.method == 'PUT'), hasLength(1));

          // The user's retry sends the same save, which is safe (027).
          await tapAndSettle(tester, _save);
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(find.text(l10n.profileSaved), findsOneWidget);
        });
      });
    });

    testWidgets('a save that gets no answer times out and keeps the text', (
      tester,
    ) async {
      final server = _backend()..once('PUT', ApiPaths.profile, neverAnswers);
      await _openEmpty(tester, server);
      await tester.enterText(_name, 'Ana');

      await tester.ensureVisible(_save);
      await tester.tap(_save);
      await tester.pump();
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();

      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(_text(tester, _name), 'Ana');
      expect(textFieldOf(tester, _name).enabled, isTrue);
    });

    testWidgets('a session that ends during a save leaves for log in', (
      tester,
    ) async {
      final server = _backend()
        ..once(
          'PUT',
          ApiPaths.profile,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..once(
          'POST',
          ApiPaths.refresh,
          (_) => errorResponse(401, 'invalid_refresh_token'),
        );
      final app = await _openEmpty(tester, server);
      await tester.enterText(_name, 'Ana');
      await tapAndSettle(tester, _save);

      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(find.byType(ProfileScreen), findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
    });
  });

  group('navigation', () {
    testWidgets('back returns to home', (tester) async {
      final app = await _openEmpty(tester, _backend());
      await tester.pageBack();
      await tester.pumpAndSettle();

      expect(find.byType(HomeScreen), findsOneWidget);
      expect(find.byType(ProfileScreen), findsNothing);
      expect(app.location(tester), '/home');
    });

    testWidgets('logging out elsewhere leaves the profile for log in', (
      tester,
    ) async {
      final app = await _openEmpty(tester, _backend());
      await app.session.logout();
      await tester.pumpAndSettle();

      expect(find.byType(LoginScreen), findsOneWidget);
      expect(find.byType(ProfileScreen), findsNothing);
      expect(app.location(tester), '/login');
    });

    testWidgets('an answer that arrives after leaving is dropped', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('PUT', ApiPaths.profile, (_) => reply.future);
      await _openEmpty(tester, server);
      await tester.enterText(_name, 'Ana');
      await tester.ensureVisible(_save);
      await tester.tap(_save);
      await tester.pump();

      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(find.byType(HomeScreen), findsOneWidget);

      reply.complete(jsonResponse(200, profileBody(displayName: 'Ana')));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.byType(HomeScreen), findsOneWidget);
    });

    testWidgets('a load answered after leaving is dropped', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.profile, (_) => reply.future);
      await _openProfile(tester, server, settle: false);

      await tester.pageBack();
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      reply.complete(errorResponse(500, 'internal_error'));
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(tester.takeException(), isNull);
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(find.byType(FormErrorBanner), findsNothing);
    });
  });

  testWidgets('home does not load the profile by itself', (tester) async {
    final server = _backend();
    await pumpApp(tester, server: server, signedIn: true);
    expect(_open, findsOneWidget);
    expect(server.count(ApiPaths.profile), 0);
  });
}

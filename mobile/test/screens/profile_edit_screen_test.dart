import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/screens/languages_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/profile_avatar_editor.dart';
import 'package:vocatogether/screens/profile_edit_screen.dart';
import 'package:vocatogether/screens/profile_screen.dart';
import 'package:vocatogether/session.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/primary_button.dart';
import 'package:vocatogether/ui/widgets/profile_avatar.dart';
import 'package:vocatogether/ui/widgets/secondary_button.dart';

import '../support/fakes.dart';
import '../support/pictures.dart';
import 'harness.dart';

Finder get _openProfile =>
    find.widgetWithText(OutlinedButton, l10n.profileButton);
Finder get _editProfile =>
    find.widgetWithText(OutlinedButton, l10n.profileEditButton);
Finder get _createProfile =>
    find.widgetWithText(FilledButton, l10n.profileEmptyButton);
Finder get _page => find.byType(ProfileScreen);
Finder get _form => find.byType(ProfileEditScreen);
Finder get _save => find.widgetWithText(FilledButton, l10n.profileSaveButton);
Finder get _cancel =>
    find.widgetWithText(OutlinedButton, l10n.profileCancelButton);
Finder get _languages =>
    find.widgetWithText(ListTile, l10n.profileEditLanguagesButton);
Finder get _retry => find.widgetWithText(FilledButton, l10n.tryAgain);
Finder get _name => field(l10n.displayNameLabel);
Finder get _bio => field(l10n.bioLabel);
Finder get _dialog => find.byType(AlertDialog);
Finder get _editor => find.byType(LanguagesScreen);

Finder get _picture => find.byType(ProfileAvatarEditor);
Finder get _add =>
    find.widgetWithText(OutlinedButton, l10n.profileAvatarAddButton);
Finder get _change =>
    find.widgetWithText(OutlinedButton, l10n.profileAvatarChangeButton);
Finder get _remove =>
    find.widgetWithText(OutlinedButton, l10n.profileAvatarRemoveButton);
Finder get _pictureError =>
    find.descendant(of: _picture, matching: find.byType(FormErrorBanner));

/// The picture the control shows, or null for the placeholder.
Uint8List? _shown(WidgetTester tester) => tester
    .widget<ProfileAvatar>(
      find.descendant(of: _picture, matching: find.byType(ProfileAvatar)),
    )
    .image;

/// The picture control's button labelled [label], as the control built it.
SecondaryButton _pictureButton(WidgetTester tester, String label) =>
    tester.widget<SecondaryButton>(
      find.descendant(
        of: _picture,
        matching: find.widgetWithText(SecondaryButton, label),
      ),
    );

/// A photo as a device might give it. Nothing in the app reads it, so it
/// need not be an image.
final _photo = Uint8List.fromList([0x89, 0x50, 0x4E, 0x47, 1, 2, 3, 4, 5]);

String _text(WidgetTester tester, Finder f) =>
    textFieldOf(tester, f).controller!.text;

/// A backend for a signed-in user on home, with no languages chosen (the
/// profile page's languages section loads them); the profile calls are
/// scripted by each test.
FakeServer _backend() => FakeServer()
  ..always('GET', ApiPaths.languages, (_) => jsonResponse(200, catalogBody()))
  ..always(
    'GET',
    ApiPaths.myLanguages,
    (_) => jsonResponse(200, languagesBody()),
  )
  ..always('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()))
  // No picture, unless a test scripts one.
  ..always('GET', ApiPaths.myAvatar, (_) => noAvatar())
  ..always('GET', ApiPaths.healthz, (_) => healthy())
  ..always('POST', ApiPaths.logout, (_) => noContent());

/// Every unscripted load answers that no profile is saved.
void _noneStored(FakeServer server) =>
    server.always('GET', ApiPaths.profile, (_) => noProfile());

/// Every unscripted load answers this stored profile.
void _stored(FakeServer server, {String name = 'Ana', String bio = ''}) =>
    server.always(
      'GET',
      ApiPaths.profile,
      (_) => jsonResponse(200, profileBody(displayName: name, bio: bio)),
    );

http.Response _validation(List<(String, String)> fields) => jsonResponse(422, {
  'error': {
    'code': 'validation_failed',
    'fields': [
      for (final (field, code) in fields) {'field': field, 'code': code},
    ],
  },
});

List<http.Request> _puts(FakeServer server) => [
  for (final request in server.to(ApiPaths.profile))
    if (request.method == 'PUT') request,
];

/// The member has this picture stored: every unscripted read answers it.
void _hasPicture(FakeServer server) =>
    server.always('GET', ApiPaths.myAvatar, (_) => imageResponse(testPicture));

/// The uploads and the removals sent, in order.
List<http.Request> _pictureWrites(FakeServer server) => [
  for (final request in server.to(ApiPaths.myAvatar))
    if (request.method != 'GET') request,
];

/// Signs in on home and opens the profile page, whose load must be
/// scripted.
Future<TestApp> _openPage(WidgetTester tester, FakeServer server) async {
  final app = await pumpApp(tester, server: server, signedIn: true);
  await tapAndSettle(tester, _openProfile);
  expect(_page, findsOneWidget);
  return app;
}

/// From the profile page, opens the form, whose load must be scripted.
Future<void> _openForm(WidgetTester tester, {bool settle = true}) async {
  final entry = _editProfile.evaluate().isNotEmpty
      ? _editProfile
      : _createProfile;
  await tester.ensureVisible(entry);
  await tester.pumpAndSettle();
  await tester.tap(entry);
  if (settle) {
    await tester.pumpAndSettle();
  } else {
    await tester.pump();
    await tester.pump(const Duration(seconds: 1));
  }
  expect(_form, findsOneWidget);
}

/// Opens the form of a user who hasn't saved a profile: empty.
Future<TestApp> _openEmpty(WidgetTester tester, FakeServer server) async {
  _noneStored(server);
  final app = await _openPage(tester, server);
  await _openForm(tester);
  return app;
}

/// Opens the form of a user whose stored profile is [name] and [bio].
Future<TestApp> _openSaved(
  WidgetTester tester,
  FakeServer server, {
  String name = 'Ana',
  String bio = '',
}) async {
  _stored(server, name: name, bio: bio);
  final app = await _openPage(tester, server);
  await _openForm(tester);
  return app;
}

/// The system's back, as the Android button or gesture sends it.
Future<void> _systemBack(WidgetTester tester) async {
  await tester.binding.handlePopRoute();
  await tester.pumpAndSettle();
}

final _waysOut = <String, Future<void> Function(WidgetTester)>{
  'Cancel': (tester) => tapAndSettle(tester, _cancel),
  'the app bar\'s back': (tester) async {
    await tester.pageBack();
    await tester.pumpAndSettle();
  },
  'the system\'s back': _systemBack,
};

void main() {
  group('loading the form', () {
    testWidgets('a labelled spinner, then an empty form to create a profile', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.profile, (_) => noProfile());
      final handle = tester.ensureSemantics();
      final app = await _openPage(tester, server);
      server.once('GET', ApiPaths.profile, (_) => reply.future);
      await _openForm(tester, settle: false);

      expect(app.location(tester), '/profile/edit');
      expect(find.bySemanticsLabel(l10n.profileLoading), findsOneWidget);
      expect(_save, findsNothing);
      expect(_name, findsNothing);
      // No way in to the languages editor until the form is there.
      expect(_languages, findsNothing);

      reply.complete(noProfile());
      await tester.pumpAndSettle();
      expect(find.bySemanticsLabel(l10n.profileLoading), findsNothing);
      expect(find.text(l10n.profileCreateHeading), findsOneWidget);
      expect(find.text(l10n.profileEditHeading), findsNothing);
      expect(
        tester.getSemantics(find.text(l10n.profileCreateHeading)),
        isSemantics(label: l10n.profileCreateHeading, isHeader: true),
      );
      expect(_text(tester, _name), '');
      expect(_text(tester, _bio), '');
      expect(find.byType(FormErrorBanner), findsNothing);
      // The user is told what others will see before writing anything.
      expect(l10n.profileVisibilityNotice, contains('picture'));
      expect(
        tester.getTopLeft(find.text(l10n.profileVisibilityNotice)).dy,
        lessThan(tester.getTopLeft(_name).dy),
      );
      // Also before a profile exists.
      expect(_languages, findsOneWidget);
      expect(_cancel, findsOneWidget);
      handle.dispose();
    });

    testWidgets('a saved profile fills the form', (tester) async {
      final server = _backend();
      await _openSaved(
        tester,
        server,
        name: 'Ana López',
        bio: 'Line one\nLine two',
      );

      expect(find.text(l10n.profileEditHeading), findsOneWidget);
      expect(find.text(l10n.profileCreateHeading), findsNothing);
      expect(_text(tester, _name), 'Ana López');
      expect(_text(tester, _bio), 'Line one\nLine two');
      expect(find.text(l10n.profileVisibilityNotice), findsOneWidget);
      expect(_languages, findsOneWidget);
      // The page asked once and the form once.
      expect(server.count(ApiPaths.profile), 2);
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
        // Not the backend's "none saved": never shown as an empty form.
        'a 404 without the code': (
          (_) => http.Response('Not Found', 404),
          l10n.errorUnexpected,
        ),
      };
      cases.forEach((name, c) {
        final (responder, message) = c;
        testWidgets(name, (tester) async {
          final server = _backend();
          _stored(server);
          final app = await _openPage(tester, server);
          server.once('GET', ApiPaths.profile, responder);
          await _openForm(tester);

          expect(find.text(message), findsOneWidget);
          expect(_save, findsNothing);
          expect(_name, findsNothing);
          expect(_languages, findsNothing);
          expect(app.session.status, SessionStatus.signedIn);
          expect(app.location(tester), '/profile/edit');

          await tapAndSettle(tester, _retry);
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(_text(tester, _name), 'Ana');
          expect(_languages, findsOneWidget);
          expect(server.count(ApiPaths.profile), 3);
        });
      });
    });

    testWidgets('leaving from the load error asks nothing', (tester) async {
      final server = _backend();
      _stored(server);
      await _openPage(tester, server);
      server.once('GET', ApiPaths.profile, networkFailure);
      await _openForm(tester);
      expect(find.text(l10n.errorNetwork), findsOneWidget);

      await _systemBack(tester);
      expect(_dialog, findsNothing);
      expect(_form, findsNothing);
      expect(_page, findsOneWidget);
    });

    testWidgets('a load answered after leaving is dropped', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend();
      _stored(server);
      await _openPage(tester, server);
      server.once('GET', ApiPaths.profile, (_) => reply.future);
      await _openForm(tester, settle: false);

      // Nothing was loaded, so nothing is asked.
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(_dialog, findsNothing);
      expect(_form, findsNothing);

      reply.complete(errorResponse(500, 'internal_error'));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(_page, findsOneWidget);
    });

    testWidgets('a session that ended while loading shows no error', (
      tester,
    ) async {
      final server = _backend();
      _stored(server);
      final app = await _openPage(tester, server);
      server
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
      await tester.tap(_editProfile);
      await tester.pumpAndSettle();

      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_form, findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(tester.takeException(), isNull);
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
      expect(_puts(server), isEmpty);
      expect(_form, findsOneWidget);
      // Typing clears the error.
      await tester.enterText(_name, 'A');
      await tester.pump();
      expect(errorOf(tester, l10n.displayNameLabel), isNull);
    });

    testWidgets('sends the text as typed and returns to the page, which shows '
        'what the server stored', (tester) async {
      final server = _backend();
      final app = await _openEmpty(tester, server);
      server
        ..once(
          'PUT',
          ApiPaths.profile,
          (_) => jsonResponse(
            200,
            profileBody(displayName: 'Ana López', bio: 'Hi'),
          ),
        )
        ..once(
          'GET',
          ApiPaths.profile,
          (_) => jsonResponse(
            200,
            profileBody(displayName: 'Ana López', bio: 'Hi'),
          ),
        );

      await tester.enterText(_name, '  Ana   López ');
      await tester.enterText(_bio, 'Hi \n');
      expect(app.location(tester), '/profile/edit');
      await tapAndSettle(tester, _save);

      final put = _puts(server).single;
      expect(put.url.hasQuery, isFalse);
      // One request replaces both, exactly as typed.
      expect(put.body, r'{"display_name":"  Ana   López ","bio":"Hi \n"}');
      // No question about unsaved changes on the way out.
      expect(_dialog, findsNothing);
      expect(_form, findsNothing);
      expect(_page, findsOneWidget);
      expect(app.location(tester), '/profile');
      // The page holds the stored, normalized text.
      expect(find.text('Ana López'), findsOneWidget);
      expect(find.text('Hi'), findsOneWidget);
      expect(find.byType(FormErrorBanner), findsNothing);
    });

    testWidgets('clearing the text of an existing profile is saved', (
      tester,
    ) async {
      final server = _backend();
      await _openSaved(tester, server, bio: 'Old');
      server.once(
        'PUT',
        ApiPaths.profile,
        (_) => jsonResponse(200, profileBody(displayName: 'Ana', bio: '')),
      );
      _stored(server);

      await tester.enterText(_bio, '');
      await tapAndSettle(tester, _save);

      expect(jsonDecode(_puts(server).single.body), {
        'display_name': 'Ana',
        'bio': '',
      });
      expect(_form, findsNothing);
      expect(find.text('Old'), findsNothing);
    });

    testWidgets('saving with nothing changed sends the save and returns', (
      tester,
    ) async {
      final server = _backend();
      final app = await _openSaved(tester, server, bio: 'Hi');
      server.once(
        'PUT',
        ApiPaths.profile,
        (_) => jsonResponse(200, profileBody(displayName: 'Ana', bio: 'Hi')),
      );

      await tapAndSettle(tester, _save);

      expect(jsonDecode(_puts(server).single.body), {
        'display_name': 'Ana',
        'bio': 'Hi',
      });
      expect(_form, findsNothing);
      expect(app.location(tester), '/profile');
    });

    testWidgets('while saving everything is locked, a second tap is ignored '
        'and back does nothing', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend();
      final app = await _openEmpty(tester, server);
      server.once('PUT', ApiPaths.profile, (_) => reply.future);
      await tester.enterText(_name, 'Ana');

      await tester.ensureVisible(_save);
      await tester.pumpAndSettle();
      await tester.tap(_save);
      await tester.pump();
      expect(textFieldOf(tester, _name).enabled, isFalse);
      expect(textFieldOf(tester, _bio).enabled, isFalse);
      expect(tester.widget<ListTile>(_languages).enabled, isFalse);
      expect(
        tester
            .widget<SecondaryButton>(
              find.widgetWithText(SecondaryButton, l10n.profileCancelButton),
            )
            .onPressed,
        isNull,
      );
      expect(
        tester.widget<PrimaryButton>(find.byType(PrimaryButton)).busy,
        isTrue,
      );
      await tester.tap(_save, warnIfMissed: false);
      await tester.pump();
      await tester.tap(_languages, warnIfMissed: false);
      await tester.pump();
      expect(_puts(server), hasLength(1));
      expect(_editor, findsNothing);

      // Neither back leaves, or asks, until the save answers.
      await tester.binding.handlePopRoute();
      await tester.pump();
      await tester.pageBack();
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(_form, findsOneWidget);
      expect(_dialog, findsNothing);
      expect(app.location(tester), '/profile/edit');

      server.once(
        'GET',
        ApiPaths.profile,
        (_) => jsonResponse(200, profileBody(displayName: 'Ana')),
      );
      reply.complete(jsonResponse(200, profileBody(displayName: 'Ana')));
      await tester.pumpAndSettle();
      expect(_form, findsNothing);
      expect(_page, findsOneWidget);
      expect(_puts(server), hasLength(1));
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
      expect(_puts(server), isEmpty);
    });

    testWidgets('server validation goes on the fields and keeps the text', (
      tester,
    ) async {
      final server = _backend();
      final app = await _openEmpty(tester, server);
      server
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
      await tester.enterText(_name, 'A very long name');
      await tester.enterText(_bio, 'Some text');

      await tapAndSettle(tester, _save);
      expect(
        errorOf(tester, l10n.displayNameLabel),
        l10n.errorDisplayNameTooLong,
      );
      expect(errorOf(tester, l10n.bioLabel), l10n.errorBioInvalid);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(hasFocus(tester, l10n.displayNameLabel), isTrue);
      expect(_text(tester, _name), 'A very long name');
      expect(_text(tester, _bio), 'Some text');
      expect(_form, findsOneWidget);
      expect(app.location(tester), '/profile/edit');

      // Only the bio is wrong the second time: it gets the focus.
      await tapAndSettle(tester, _save);
      expect(errorOf(tester, l10n.displayNameLabel), isNull);
      expect(errorOf(tester, l10n.bioLabel), l10n.errorBioTooLong);
      expect(hasFocus(tester, l10n.bioLabel), isTrue);
      // Editing a field clears its error.
      await tester.enterText(_bio, 'Some');
      await tester.pump();
      expect(errorOf(tester, l10n.bioLabel), isNull);
    });

    // The app sets no limit of its own (the server owns the rules, 027): a
    // long text goes out whole and the server's answer names the field.
    testWidgets('a pasted text far over the limit is sent whole and gets the '
        'too-long error on its field', (tester) async {
      final server = _backend();
      await _openEmpty(tester, server);
      server.once(
        'PUT',
        ApiPaths.profile,
        (_) => _validation([('bio', 'too_long')]),
      );
      final pasted = List.filled(1000, 'a line of text').join('\n');
      expect(pasted.length, greaterThan(10000));
      await tester.enterText(_name, 'Ana');
      await tester.enterText(_bio, pasted);
      await tester.pumpAndSettle();
      await tapAndSettle(tester, _save);

      expect(
        (jsonDecode(_puts(server).single.body) as Map<String, Object?>)['bio'],
        pasted,
      );
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
        // Whatever a body says, the text shown is the app's own.
        'a body with text of its own': (
          (_) => http.Response(
            '{"error":{"code":"internal_error","message":"SERVERTEXT"}}',
            500,
            headers: {'content-type': 'application/json'},
          ),
          l10n.errorUnexpected,
        ),
      };
      cases.forEach((name, c) {
        final (responder, message) = c;
        testWidgets(name, (tester) async {
          final server = _backend();
          final app = await _openEmpty(tester, server);
          server
            ..once('PUT', ApiPaths.profile, responder)
            ..once(
              'PUT',
              ApiPaths.profile,
              (_) =>
                  jsonResponse(200, profileBody(displayName: 'Ana', bio: 'Hi')),
            );
          await tester.enterText(_name, 'Ana');
          await tester.enterText(_bio, 'Hi');

          await tapAndSettle(tester, _save);
          expect(
            find.descendant(
              of: find.byType(FormErrorBanner),
              matching: find.text(message),
            ),
            findsOneWidget,
          );
          expect(find.textContaining('SERVERTEXT'), findsNothing);
          // The message is above the fields.
          expect(
            tester.getTopLeft(find.byType(FormErrorBanner)).dy,
            lessThan(tester.getTopLeft(_name).dy),
          );
          expect(_text(tester, _name), 'Ana');
          expect(_text(tester, _bio), 'Hi');
          expect(_form, findsOneWidget);
          expect(app.session.status, SessionStatus.signedIn);
          expect(app.location(tester), '/profile/edit');
          // Nothing was retried on its own.
          expect(_puts(server), hasLength(1));

          // The user's retry sends the same save, which is safe (027).
          await tapAndSettle(tester, _save);
          expect(_puts(server), hasLength(2));
          expect(_puts(server).last.body, _puts(server).first.body);
          expect(_form, findsNothing);
          expect(_page, findsOneWidget);
        });
      });
    });

    testWidgets('a save that gets no answer times out and keeps the text', (
      tester,
    ) async {
      final server = _backend();
      await _openEmpty(tester, server);
      server.once('PUT', ApiPaths.profile, neverAnswers);
      await tester.enterText(_name, 'Ana');

      await tester.ensureVisible(_save);
      await tester.pumpAndSettle();
      await tester.tap(_save);
      await tester.pump();
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();

      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(_text(tester, _name), 'Ana');
      expect(textFieldOf(tester, _name).enabled, isTrue);
      expect(_form, findsOneWidget);
    });

    testWidgets('a session that ends during a save leaves for log in', (
      tester,
    ) async {
      final server = _backend();
      final app = await _openEmpty(tester, server);
      server
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
      await tester.enterText(_name, 'Ana');
      await tapAndSettle(tester, _save);

      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_form, findsNothing);
      expect(_page, findsNothing);
      expect(_dialog, findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
    });
  });

  group('leaving', () {
    _waysOut.forEach((name, leave) {
      testWidgets('$name with nothing changed leaves at once', (tester) async {
        final server = _backend();
        final app = await _openSaved(tester, server, bio: 'Hi');
        await leave(tester);

        expect(_dialog, findsNothing);
        expect(_form, findsNothing);
        expect(_page, findsOneWidget);
        expect(app.location(tester), '/profile');
        expect(_puts(server), isEmpty);
      });

      testWidgets('$name with a change asks, and "Keep editing" stays', (
        tester,
      ) async {
        final server = _backend();
        final app = await _openSaved(tester, server, bio: 'Hi');
        await tester.enterText(_bio, 'Hi there');
        await tester.pumpAndSettle();
        await leave(tester);

        expect(_dialog, findsOneWidget);
        expect(find.text(l10n.languagesDiscardTitle), findsOneWidget);
        expect(find.text(l10n.profileDiscardMessage), findsOneWidget);
        expect(find.text(l10n.languagesDiscardConfirm), findsOneWidget);
        await tapAndSettle(tester, find.text(l10n.languagesDiscardKeep));
        expect(_dialog, findsNothing);
        expect(_form, findsOneWidget);
        expect(_text(tester, _name), 'Ana');
        expect(_text(tester, _bio), 'Hi there');
        expect(app.location(tester), '/profile/edit');
      });

      testWidgets('$name with a change, then "Discard", leaves without '
          'saving', (tester) async {
        final server = _backend();
        final app = await _openSaved(tester, server, bio: 'Hi');
        await tester.enterText(_name, 'Someone else');
        await tester.pumpAndSettle();
        await leave(tester);
        await tapAndSettle(tester, find.text(l10n.languagesDiscardConfirm));

        expect(_dialog, findsNothing);
        expect(_form, findsNothing);
        expect(app.location(tester), '/profile');
        expect(_puts(server), isEmpty);
        // The page shows what is stored.
        expect(find.text('Someone else'), findsNothing);
        expect(find.text('Ana'), findsOneWidget);
        expect(find.text('Hi'), findsOneWidget);
      });
    });

    testWidgets('text typed into the empty form is a change to ask about', (
      tester,
    ) async {
      await _openEmpty(tester, _backend());
      await tester.enterText(_bio, 'A');
      await tester.pumpAndSettle();
      await _systemBack(tester);
      expect(_dialog, findsOneWidget);
    });

    testWidgets('typing the loaded text back is no change', (tester) async {
      final server = _backend();
      await _openSaved(tester, server, bio: 'Hi');
      await tester.enterText(_name, 'Ana María');
      await tester.enterText(_bio, '');
      await tester.pumpAndSettle();
      await tester.enterText(_name, 'Ana');
      await tester.enterText(_bio, 'Hi');
      await tester.pumpAndSettle();

      await _systemBack(tester);
      expect(_dialog, findsNothing);
      expect(_form, findsNothing);
      expect(_puts(server), isEmpty);
    });

    testWidgets('dismissing the question keeps editing', (tester) async {
      await _openSaved(tester, _backend());
      await tester.enterText(_name, 'Ana María');
      await tester.pumpAndSettle();
      await _systemBack(tester);
      expect(_dialog, findsOneWidget);
      // Back again closes only the dialog.
      await _systemBack(tester);
      expect(_dialog, findsNothing);
      expect(_form, findsOneWidget);
      expect(_text(tester, _name), 'Ana María');

      await _systemBack(tester);
      await tester.tapAt(const Offset(5, 5));
      await tester.pumpAndSettle();
      expect(_dialog, findsNothing);
      expect(_form, findsOneWidget);
      expect(_text(tester, _name), 'Ana María');
    });

    testWidgets('a failed save still counts as unsaved', (tester) async {
      final server = _backend();
      await _openSaved(tester, server);
      server.once('PUT', ApiPaths.profile, networkFailure);
      await tester.enterText(_name, 'Ana María');
      await tapAndSettle(tester, _save);
      await _systemBack(tester);
      expect(_dialog, findsOneWidget);
    });
  });

  group('the session ending with unsaved changes', () {
    final states = <String, Future<void> Function(WidgetTester)>{
      'nothing else open': (tester) async {},
      'the discard question open': _systemBack,
    };
    states.forEach((name, open) {
      testWidgets('with $name: log in, and nothing of the form', (
        tester,
      ) async {
        final server = _backend();
        final app = await _openSaved(tester, server);
        await tester.enterText(_name, 'Ana María');
        await tester.pumpAndSettle();
        await open(tester);

        await app.session.logout();
        await tester.pumpAndSettle();

        expect(app.location(tester), '/login');
        expect(find.byType(LoginScreen), findsOneWidget);
        expect(_form, findsNothing);
        expect(_page, findsNothing);
        expect(_dialog, findsNothing);
        expect(find.text(l10n.languagesDiscardTitle), findsNothing);
        expect(find.text('Ana María'), findsNothing);
        expect(tester.takeException(), isNull);
        expect(_puts(server), isEmpty);
      });
    });
  });

  group('the way in to the languages editor', () {
    testWidgets('"Languages" opens the editor on top, and the typed text is '
        'kept and not saved by the visit', (tester) async {
      final server = _backend();
      final app = await _openSaved(tester, server);
      await tester.enterText(_name, 'Ana López');
      await tester.enterText(_bio, 'Not saved yet');
      await tester.pumpAndSettle();

      await tapAndSettle(tester, _languages);
      expect(_editor, findsOneWidget);
      expect(app.location(tester), '/profile/languages');

      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(_editor, findsNothing);
      expect(_form, findsOneWidget);
      expect(app.location(tester), '/profile/edit');
      expect(_text(tester, _name), 'Ana López');
      expect(_text(tester, _bio), 'Not saved yet');
      expect(_puts(server), isEmpty);
      // The form was not loaded again either: page, then form.
      expect(server.count(ApiPaths.profile), 2);
      // And it still asks before the typed text is lost.
      await _systemBack(tester);
      expect(_dialog, findsOneWidget);
    });

    testWidgets('a rapid double tap opens the editor once', (tester) async {
      final server = _backend();
      final app = await _openSaved(tester, server);
      await tester.ensureVisible(_languages);
      await tester.pumpAndSettle();
      // Twice in the same frame, before the editor covers the row.
      final row = tester.getCenter(_languages);
      await tester.tapAt(row);
      await tester.tapAt(row);
      await tester.pumpAndSettle();
      expect(_editor, findsOneWidget);

      // One step back is the form: no second editor was underneath.
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(_editor, findsNothing);
      expect(_form, findsOneWidget);
      expect(app.location(tester), '/profile/edit');

      // And the row opens it again afterwards.
      await tapAndSettle(tester, _languages);
      expect(_editor, findsOneWidget);
    });

    testWidgets('the form requests no catalog and no languages: opening, a '
        'failed save and a save', (tester) async {
      final server = _backend();
      _stored(server);
      await _openPage(tester, server);
      // The page's languages section asked once for each.
      (int, int) asked() => (
        server.count(ApiPaths.languages),
        server.count(ApiPaths.myLanguages),
      );
      expect(asked(), (1, 1));

      await _openForm(tester);
      expect(asked(), (1, 1));
      server
        ..once('PUT', ApiPaths.profile, networkFailure)
        ..once(
          'PUT',
          ApiPaths.profile,
          (_) => jsonResponse(200, profileBody(displayName: 'Ana López')),
        );
      await tester.enterText(_name, 'Ana López');
      await tapAndSettle(tester, _save);
      expect(_form, findsOneWidget);
      expect(asked(), (1, 1));

      await tapAndSettle(tester, _save);
      expect(_form, findsNothing);
      // Only the page asks again, when it is shown.
      expect(asked(), (2, 2));
      for (final put in _puts(server)) {
        expect(put.body, isNot(contains('spoken')));
        expect(put.body, isNot(contains('learning')));
      }
    });
  });

  group('the picture control', () {
    testWidgets('with a picture: shown, with "Change photo" and "Remove '
        'photo"', (tester) async {
      final server = _backend();
      _hasPicture(server);
      final handle = tester.ensureSemantics();
      await _openSaved(tester, server);

      expect(_shown(tester), testPicture);
      expect(_change, findsOneWidget);
      expect(_remove, findsOneWidget);
      expect(_add, findsNothing);
      expect(_pictureError, findsNothing);
      expect(
        tester.getSemantics(
          find.descendant(of: _picture, matching: find.byType(ProfileAvatar)),
        ),
        isSemantics(isImage: true, label: l10n.profileAvatarLabel),
      );
      handle.dispose();
    });

    testWidgets('without a picture: the placeholder with "Add photo", and no '
        '"Remove photo"', (tester) async {
      final server = _backend();
      final handle = tester.ensureSemantics();
      await _openSaved(tester, server);

      expect(_shown(tester), isNull);
      expect(
        find.descendant(of: _picture, matching: find.text('A')),
        findsOneWidget,
      );
      expect(_add, findsOneWidget);
      expect(_change, findsNothing);
      expect(_remove, findsNothing);
      expect(
        tester.getSemantics(
          find.descendant(of: _picture, matching: find.byType(ProfileAvatar)),
        ),
        isSemantics(isImage: true, label: l10n.profileAvatarPlaceholderLabel),
      );
      handle.dispose();
    });

    testWidgets('before a profile exists the control is there', (tester) async {
      final server = _backend();
      await _openEmpty(tester, server);

      expect(_add, findsOneWidget);
      expect(
        _pictureButton(tester, l10n.profileAvatarAddButton).onPressed,
        isNotNull,
      );
      // No name yet, so no initial.
      expect(
        find.descendant(of: _picture, matching: find.byIcon(Icons.person)),
        findsOneWidget,
      );
      // The page asked for nothing: nothing is shown of a member with no
      // profile. The form asked once.
      expect(server.count(ApiPaths.myAvatar), 1);
    });

    testWidgets('the placeholder\'s initial is the stored name\'s, not the '
        'typed one', (tester) async {
      final server = _backend();
      await _openSaved(tester, server);
      await tester.enterText(_name, 'Zoe');
      await tester.pumpAndSettle();

      expect(
        find.descendant(of: _picture, matching: find.text('A')),
        findsOneWidget,
      );
    });

    testWidgets('it loads on its own: a labelled spinner, and the form can '
        'be edited and saved meanwhile', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend();
      _stored(server);
      await _openPage(tester, server);
      server
        ..once('GET', ApiPaths.myAvatar, (_) => reply.future)
        ..once(
          'PUT',
          ApiPaths.profile,
          (_) => jsonResponse(200, profileBody(displayName: 'Ana María')),
        );
      await _openForm(tester, settle: false);

      expect(
        find.descendant(
          of: _picture,
          matching: find.bySemanticsLabel(l10n.profileAvatarLoading),
        ),
        findsOneWidget,
      );
      // What is offered depends on the answer.
      expect(_add, findsNothing);
      expect(_change, findsNothing);
      expect(_remove, findsNothing);
      expect(textFieldOf(tester, _name).enabled, isTrue);
      expect(textFieldOf(tester, _bio).enabled, isTrue);

      await tester.enterText(_name, 'Ana María');
      await tester.ensureVisible(_save);
      await tester.pump();
      await tester.tap(_save);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(_puts(server), hasLength(1));
      expect(_form, findsNothing);

      // Answered after the form was left: dropped.
      reply.complete(imageResponse(testPicture));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(_page, findsOneWidget);
    });

    group('a picture that fails to load is the placeholder, with the way to '
        'choose one', () {
      final cases = <String, Responder>{
        'network': networkFailure,
        '500': (_) => errorResponse(500, 'internal_error'),
        '429': (_) => errorResponse(429, 'rate_limited'),
        'a 404 without the code': (_) => http.Response('Not Found', 404),
        'a body with text of its own': (_) => http.Response(
          '{"error":{"code":"internal_error","message":"SERVERTEXT"}}',
          500,
          headers: {'content-type': 'application/json'},
        ),
      };
      cases.forEach((name, responder) {
        testWidgets(name, (tester) async {
          final server = _backend();
          _stored(server);
          await _openPage(tester, server);
          server
            ..once('GET', ApiPaths.myAvatar, responder)
            ..once(
              'PUT',
              ApiPaths.profile,
              (_) => jsonResponse(200, profileBody(displayName: 'Ana María')),
            );
          await _openForm(tester);

          expect(_shown(tester), isNull);
          expect(_add, findsOneWidget);
          expect(_remove, findsNothing);
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(find.textContaining('SERVERTEXT'), findsNothing);
          // Nothing retried it.
          expect(server.count(ApiPaths.myAvatar), 2);

          // The form is as usable as ever.
          await tester.enterText(_name, 'Ana María');
          await tapAndSettle(tester, _save);
          expect(_puts(server), hasLength(1));
          expect(_form, findsNothing);
          expect(_page, findsOneWidget);
        });
      });
    });

    testWidgets('a form that failed to load shows no picture control and '
        'asks for no picture', (tester) async {
      final server = _backend();
      _stored(server);
      await _openPage(tester, server);
      final asked = server.count(ApiPaths.myAvatar);
      server.once('GET', ApiPaths.profile, networkFailure);
      await _openForm(tester);

      expect(_retry, findsOneWidget);
      expect(_picture, findsNothing);
      expect(server.count(ApiPaths.myAvatar), asked);
    });

    testWidgets('a session that ends while the picture loads shows no error', (
      tester,
    ) async {
      final server = _backend();
      _stored(server);
      final app = await _openPage(tester, server);
      server
        ..once(
          'GET',
          ApiPaths.myAvatar,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..once(
          'POST',
          ApiPaths.refresh,
          (_) => errorResponse(401, 'invalid_refresh_token'),
        );
      final edit = _editProfile;
      await tester.ensureVisible(edit);
      await tester.pumpAndSettle();
      await tester.tap(edit);
      await tester.pumpAndSettle();

      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_form, findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(tester.takeException(), isNull);
    });
  });

  group('choosing a picture', () {
    testWidgets('uploads the photo as the device gave it, at once, and shows '
        'what the server stored', (tester) async {
      final server = _backend();
      final app = await _openSaved(tester, server, bio: 'Hi');
      app.photos.next(_photo);
      server.once('PUT', ApiPaths.myAvatar, (_) => imageResponse(testPicture));
      await tester.enterText(_name, 'Ana María');
      await tester.pumpAndSettle();

      await tapAndSettle(tester, _add);

      expect(app.photos.calls, 1);
      final upload = _pictureWrites(server).single;
      expect(upload.method, 'PUT');
      expect(upload.url.path, ApiPaths.myAvatar);
      expect(upload.url.hasQuery, isFalse);
      expect(upload.bodyBytes, _photo);
      expect(upload.headers['content-type'], 'application/octet-stream');
      // The stored picture, not the photo that was sent.
      expect(_shown(tester), testPicture);
      expect(_change, findsOneWidget);
      expect(_remove, findsOneWidget);
      expect(_add, findsNothing);
      expect(_pictureError, findsNothing);
      // Apart from Save: the text is as typed and was sent nowhere.
      expect(_puts(server), isEmpty);
      expect(_text(tester, _name), 'Ana María');
      expect(_text(tester, _bio), 'Hi');
      expect(_form, findsOneWidget);
      expect(app.location(tester), '/profile/edit');
      // Nothing asked for the picture again.
      expect(server.count(ApiPaths.myAvatar), 3);
    });

    testWidgets('"Change photo" replaces the picture', (tester) async {
      final stored = Uint8List.fromList([...testPicture, 0]);
      final server = _backend();
      _hasPicture(server);
      final app = await _openSaved(tester, server);
      app.photos.next(_photo);
      server.once('PUT', ApiPaths.myAvatar, (_) => imageResponse(stored));

      await tapAndSettle(tester, _change);

      expect(_pictureWrites(server).single.bodyBytes, _photo);
      expect(_shown(tester), stored);
      expect(_change, findsOneWidget);
      expect(_remove, findsOneWidget);
    });

    testWidgets('the app applies no rule of its own: an empty photo is sent', (
      tester,
    ) async {
      final server = _backend();
      final app = await _openSaved(tester, server);
      app.photos.next(const []);
      server.once(
        'PUT',
        ApiPaths.myAvatar,
        (_) => _validation([('avatar', 'required')]),
      );

      await tapAndSettle(tester, _add);

      expect(_pictureWrites(server).single.bodyBytes, isEmpty);
      expect(
        find.descendant(
          of: _pictureError,
          matching: find.text(l10n.errorAvatarRequired),
        ),
        findsOneWidget,
      );
    });

    testWidgets('closing the chooser without choosing sends nothing and '
        'changes nothing', (tester) async {
      final server = _backend();
      _hasPicture(server);
      final app = await _openSaved(tester, server);
      app.photos.cancel();

      await tapAndSettle(tester, _change);

      expect(app.photos.calls, 1);
      expect(_pictureWrites(server), isEmpty);
      expect(_shown(tester), testPicture);
      expect(_pictureError, findsNothing);
      // Everything is available again.
      expect(
        _pictureButton(tester, l10n.profileAvatarChangeButton).onPressed,
        isNotNull,
      );
      expect(
        _pictureButton(tester, l10n.profileAvatarRemoveButton).onPressed,
        isNotNull,
      );
      expect(textFieldOf(tester, _name).enabled, isTrue);
    });

    for (final MapEntry(key: name, value: leave) in _waysOut.entries) {
      testWidgets('after only a new picture, $name leaves with no question', (
        tester,
      ) async {
        final server = _backend();
        final app = await _openSaved(tester, server);
        app.photos.next(_photo);
        server.once(
          'PUT',
          ApiPaths.myAvatar,
          (_) => imageResponse(testPicture),
        );
        await tapAndSettle(tester, _add);
        _hasPicture(server);

        await leave(tester);

        expect(_dialog, findsNothing);
        expect(_form, findsNothing);
        expect(_page, findsOneWidget);
        expect(_puts(server), isEmpty);
      });
    }

    testWidgets('a new picture with typed text still asks about the text', (
      tester,
    ) async {
      final server = _backend();
      final app = await _openSaved(tester, server);
      app.photos.next(_photo);
      server.once('PUT', ApiPaths.myAvatar, (_) => imageResponse(testPicture));
      await tester.enterText(_name, 'Ana María');
      await tapAndSettle(tester, _add);

      await tapAndSettle(tester, _cancel);

      expect(_dialog, findsOneWidget);
      expect(find.text(l10n.profileDiscardMessage), findsOneWidget);
    });
  });

  group('removing the picture', () {
    testWidgets('asks first, then removes it at once', (tester) async {
      final server = _backend();
      _hasPicture(server);
      await _openSaved(tester, server);
      server.once('DELETE', ApiPaths.myAvatar, (_) => noContent());
      await tester.enterText(_name, 'Ana María');
      await tester.pumpAndSettle();

      await tapAndSettle(tester, _remove);
      expect(_dialog, findsOneWidget);
      expect(find.text(l10n.profileAvatarRemoveTitle), findsOneWidget);
      expect(find.text(l10n.profileAvatarRemoveMessage), findsOneWidget);
      // Nothing is sent before the answer.
      expect(_pictureWrites(server), isEmpty);

      await tapAndSettle(tester, find.text(l10n.profileAvatarRemoveConfirm));

      expect(_dialog, findsNothing);
      final removal = _pictureWrites(server).single;
      expect(removal.method, 'DELETE');
      expect(removal.url.path, ApiPaths.myAvatar);
      expect(removal.url.hasQuery, isFalse);
      expect(removal.bodyBytes, isEmpty);
      expect(_shown(tester), isNull);
      expect(_add, findsOneWidget);
      expect(_change, findsNothing);
      expect(_remove, findsNothing);
      expect(_pictureError, findsNothing);
      expect(_puts(server), isEmpty);
      expect(_text(tester, _name), 'Ana María');
      expect(_form, findsOneWidget);
    });

    final refusals = <String, Future<void> Function(WidgetTester)>{
      '"Keep photo"': (tester) =>
          tapAndSettle(tester, find.text(l10n.profileAvatarRemoveKeep)),
      'a tap outside the question': (tester) async {
        await tester.tapAt(const Offset(4, 4));
        await tester.pumpAndSettle();
      },
      'the system\'s back': _systemBack,
    };
    refusals.forEach((name, refuse) {
      testWidgets('$name sends nothing and keeps the picture', (tester) async {
        final server = _backend();
        _hasPicture(server);
        await _openSaved(tester, server);

        await tapAndSettle(tester, _remove);
        expect(_dialog, findsOneWidget);
        await refuse(tester);

        expect(_dialog, findsNothing);
        expect(_form, findsOneWidget);
        expect(_pictureWrites(server), isEmpty);
        expect(_shown(tester), testPicture);
        expect(_remove, findsOneWidget);
        expect(
          _pictureButton(tester, l10n.profileAvatarRemoveButton).onPressed,
          isNotNull,
        );
      });
    });

    testWidgets('after only a removal, leaving asks nothing', (tester) async {
      final server = _backend();
      _hasPicture(server);
      await _openSaved(tester, server);
      server
        ..once('DELETE', ApiPaths.myAvatar, (_) => noContent())
        ..always('GET', ApiPaths.myAvatar, (_) => noAvatar());
      await tapAndSettle(tester, _remove);
      await tapAndSettle(tester, find.text(l10n.profileAvatarRemoveConfirm));

      await tapAndSettle(tester, _cancel);

      expect(_dialog, findsNothing);
      expect(_form, findsNothing);
      expect(_page, findsOneWidget);
    });

    testWidgets('the session ending with the question open: log in, and '
        'nothing removed', (tester) async {
      final server = _backend();
      _hasPicture(server);
      final app = await _openSaved(tester, server);
      await tapAndSettle(tester, _remove);
      expect(_dialog, findsOneWidget);

      await app.session.logout();
      await tester.pumpAndSettle();

      expect(app.location(tester), '/login');
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_dialog, findsNothing);
      expect(_form, findsNothing);
      expect(_pictureWrites(server), isEmpty);
      expect(tester.takeException(), isNull);
    });
  });

  group('picture failures', () {
    /// A member with a picture, on the form, about to upload [_photo].
    Future<TestApp> aboutToUpload(
      WidgetTester tester,
      FakeServer server,
    ) async {
      _hasPicture(server);
      final app = await _openSaved(tester, server);
      app.photos.next(_photo);
      return app;
    }

    /// The control shows [message] and nothing else of the failure, the
    /// earlier picture is still there, and the form saves.
    Future<void> expectFailed(
      WidgetTester tester,
      FakeServer server,
      String message,
    ) async {
      expect(
        find.descendant(of: _pictureError, matching: find.text(message)),
        findsOneWidget,
      );
      // By the control, and the only message on the screen.
      expect(find.byType(FormErrorBanner), findsOneWidget);
      expect(find.textContaining('SERVERTEXT'), findsNothing);
      expect(_shown(tester), testPicture);
      expect(_change, findsOneWidget);
      expect(_remove, findsOneWidget);
      expect(_form, findsOneWidget);
      // Nothing was retried on its own.
      expect(_pictureWrites(server), hasLength(1));
      // The failure blocks nothing of the form.
      expect(textFieldOf(tester, _name).enabled, isTrue);
      expect(
        _pictureButton(tester, l10n.profileAvatarChangeButton).onPressed,
        isNotNull,
      );
      server.once(
        'PUT',
        ApiPaths.profile,
        (_) => jsonResponse(200, profileBody(displayName: 'Ana María')),
      );
      await tester.enterText(_name, 'Ana María');
      await tapAndSettle(tester, _save);
      expect(_puts(server), hasLength(1));
      expect(_form, findsNothing);
      expect(_page, findsOneWidget);
    }

    group('a photo the server refuses says why', () {
      final codes = <String, String>{
        'required': l10n.errorAvatarRequired,
        'too_large': l10n.errorAvatarTooLarge,
        'unsupported_type': l10n.errorAvatarUnsupportedType,
        'invalid_image': l10n.errorAvatarInvalidImage,
        'dimensions_too_large': l10n.errorAvatarDimensionsTooLarge,
      };
      codes.forEach((code, message) {
        testWidgets(code, (tester) async {
          final server = _backend();
          await aboutToUpload(tester, server);
          server.once(
            'PUT',
            ApiPaths.myAvatar,
            (_) => _validation([('avatar', code)]),
          );

          await tapAndSettle(tester, _change);

          await expectFailed(tester, server, message);
        });
      });

      testWidgets('a code this app doesn\'t know gets the general text', (
        tester,
      ) async {
        final server = _backend();
        await aboutToUpload(tester, server);
        server.once(
          'PUT',
          ApiPaths.myAvatar,
          (_) => _validation([('avatar', 'too_blurry')]),
        );

        await tapAndSettle(tester, _change);

        await expectFailed(tester, server, l10n.errorCheckInput);
      });
    });

    group('an upload that fails otherwise can be tried again', () {
      final cases = <String, (Responder, String)>{
        '429 with a wait': (
          (_) =>
              errorResponse(429, 'rate_limited', headers: {'retry-after': '6'}),
          'Too many attempts. Try again in 6 seconds.',
        ),
        '429 without a wait': (
          (_) => errorResponse(429, 'rate_limited'),
          l10n.errorRateLimitedNoWait,
        ),
        '503 with a wait': (
          (_) => errorResponse(
            503,
            'service_unavailable',
            headers: {'retry-after': '5'},
          ),
          'VocaTogether is busy right now. Try again in 5 seconds.',
        ),
        '503 without a wait': (
          (_) => errorResponse(503, 'service_unavailable'),
          l10n.errorUnavailableNoWait,
        ),
        'network': (networkFailure, l10n.errorNetwork),
        // The server's answer to an upload cut short: nothing about the
        // photo, so nothing that tells the member to choose another.
        '400 invalid_request': (
          (_) => errorResponse(400, 'invalid_request'),
          l10n.errorUnexpected,
        ),
        '500': (
          (_) => errorResponse(500, 'internal_error'),
          l10n.errorUnexpected,
        ),
        'a body with text of its own': (
          (_) => http.Response(
            '{"error":{"code":"validation_failed","message":"SERVERTEXT",'
            '"fields":[{"field":"avatar","code":"too_large",'
            '"message":"SERVERTEXT"}]}}',
            422,
            headers: {'content-type': 'application/json'},
          ),
          l10n.errorAvatarTooLarge,
        ),
      };
      cases.forEach((name, c) {
        final (responder, message) = c;
        testWidgets(name, (tester) async {
          final server = _backend();
          await aboutToUpload(tester, server);
          server.once('PUT', ApiPaths.myAvatar, responder);

          await tapAndSettle(tester, _change);

          await expectFailed(tester, server, message);
        });
      });

      testWidgets('no answer in time', (tester) async {
        final server = _backend();
        await aboutToUpload(tester, server);
        server.once('PUT', ApiPaths.myAvatar, neverAnswers);

        await tester.ensureVisible(_change);
        await tester.pumpAndSettle();
        await tester.tap(_change);
        await tester.pump();
        await tester.pump(const Duration(seconds: 16));
        await tester.pumpAndSettle();

        await expectFailed(tester, server, l10n.errorTimeout);
      });

      testWidgets('a 400 is not shown as a refusal of the photo', (
        tester,
      ) async {
        final server = _backend();
        await aboutToUpload(tester, server);
        server.once(
          'PUT',
          ApiPaths.myAvatar,
          (_) => errorResponse(400, 'invalid_request'),
        );

        await tapAndSettle(tester, _change);

        for (final refusal in [
          l10n.errorAvatarRequired,
          l10n.errorAvatarTooLarge,
          l10n.errorAvatarUnsupportedType,
          l10n.errorAvatarInvalidImage,
          l10n.errorAvatarDimensionsTooLarge,
          l10n.errorPhotoUnusable,
          l10n.errorCheckInput,
        ]) {
          expect(find.text(refusal), findsNothing);
        }
        expect(
          find.descendant(
            of: _pictureError,
            matching: find.text(l10n.errorUnexpected),
          ),
          findsOneWidget,
        );
      });
    });

    group('a removal that fails keeps the picture', () {
      final cases = <String, (Responder, String)>{
        '429': (
          (_) =>
              errorResponse(429, 'rate_limited', headers: {'retry-after': '6'}),
          'Too many attempts. Try again in 6 seconds.',
        ),
        '503': (
          (_) => errorResponse(503, 'service_unavailable'),
          l10n.errorUnavailableNoWait,
        ),
        'network': (networkFailure, l10n.errorNetwork),
        'a body with text of its own': (
          (_) => http.Response(
            '{"error":{"code":"internal_error","message":"SERVERTEXT"}}',
            500,
            headers: {'content-type': 'application/json'},
          ),
          l10n.errorUnexpected,
        ),
      };
      cases.forEach((name, c) {
        final (responder, message) = c;
        testWidgets(name, (tester) async {
          final server = _backend();
          _hasPicture(server);
          await _openSaved(tester, server);
          server.once('DELETE', ApiPaths.myAvatar, responder);

          await tapAndSettle(tester, _remove);
          await tapAndSettle(
            tester,
            find.text(l10n.profileAvatarRemoveConfirm),
          );

          await expectFailed(tester, server, message);
        });
      });

      testWidgets('no answer in time', (tester) async {
        final server = _backend();
        _hasPicture(server);
        await _openSaved(tester, server);
        server.once('DELETE', ApiPaths.myAvatar, neverAnswers);

        await tapAndSettle(tester, _remove);
        await tester.tap(find.text(l10n.profileAvatarRemoveConfirm));
        await tester.pump();
        await tester.pump(const Duration(seconds: 16));
        await tester.pumpAndSettle();

        await expectFailed(tester, server, l10n.errorTimeout);
      });
    });

    testWidgets('a photo that can\'t be read says so and sends nothing', (
      tester,
    ) async {
      final server = _backend();
      _hasPicture(server);
      final app = await _openSaved(tester, server);
      app.photos.fail();

      await tapAndSettle(tester, _change);

      expect(
        find.descendant(
          of: _pictureError,
          matching: find.text(l10n.errorPhotoUnusable),
        ),
        findsOneWidget,
      );
      expect(_pictureWrites(server), isEmpty);
      expect(_shown(tester), testPicture);
      expect(textFieldOf(tester, _name).enabled, isTrue);
      expect(
        _pictureButton(tester, l10n.profileAvatarChangeButton).onPressed,
        isNotNull,
      );
    });

    testWidgets('the next picture action clears the message', (tester) async {
      final server = _backend();
      final app = await _openSaved(tester, server);
      app.photos
        ..fail()
        ..cancel()
        ..next(_photo)
        ..next(_photo);
      server
        ..once('PUT', ApiPaths.myAvatar, networkFailure)
        ..once('PUT', ApiPaths.myAvatar, (_) => imageResponse(testPicture))
        ..once('DELETE', ApiPaths.myAvatar, networkFailure);

      await tapAndSettle(tester, _add);
      expect(_pictureError, findsOneWidget);
      // Opening the chooser is an action, also when nothing is chosen.
      await tapAndSettle(tester, _add);
      expect(_pictureError, findsNothing);

      await tapAndSettle(tester, _add);
      expect(find.text(l10n.errorNetwork), findsOneWidget);
      // The retry sends the photo again, which is safe (031).
      await tapAndSettle(tester, _add);
      expect(_pictureError, findsNothing);
      expect(_shown(tester), testPicture);
      expect(_pictureWrites(server), hasLength(2));

      await tapAndSettle(tester, _remove);
      await tapAndSettle(tester, find.text(l10n.profileAvatarRemoveConfirm));
      expect(find.text(l10n.errorNetwork), findsOneWidget);
      // Asking again is an action too, also when it is then refused.
      await tapAndSettle(tester, _remove);
      await tapAndSettle(tester, find.text(l10n.profileAvatarRemoveKeep));
      expect(_pictureError, findsNothing);
      expect(_shown(tester), testPicture);
    });

    testWidgets('a picture failure and a save failure each show their own '
        'message', (tester) async {
      final server = _backend();
      await aboutToUpload(tester, server);
      server
        ..once('PUT', ApiPaths.myAvatar, networkFailure)
        ..once('PUT', ApiPaths.profile, (_) => errorResponse(503, 'x'));
      await tapAndSettle(tester, _change);
      await tapAndSettle(tester, _save);

      expect(find.byType(FormErrorBanner), findsNWidgets(2));
      expect(
        find.descendant(
          of: _pictureError,
          matching: find.text(l10n.errorNetwork),
        ),
        findsOneWidget,
      );
      expect(find.text(l10n.errorUnavailableNoWait), findsOneWidget);
    });

    for (final write in ['an upload', 'a removal']) {
      testWidgets('a session that ends during $write leaves for log in, with '
          'no error', (tester) async {
        final server = _backend();
        final app = await aboutToUpload(tester, server);
        server
          ..once(
            write == 'an upload' ? 'PUT' : 'DELETE',
            ApiPaths.myAvatar,
            (_) => errorResponse(401, 'invalid_access_token'),
          )
          ..once(
            'POST',
            ApiPaths.refresh,
            (_) => errorResponse(401, 'invalid_refresh_token'),
          );

        if (write == 'an upload') {
          await tapAndSettle(tester, _change);
        } else {
          await tapAndSettle(tester, _remove);
          await tapAndSettle(
            tester,
            find.text(l10n.profileAvatarRemoveConfirm),
          );
        }

        expect(app.session.status, SessionStatus.signedOut);
        expect(find.byType(LoginScreen), findsOneWidget);
        expect(_form, findsNothing);
        expect(_page, findsNothing);
        expect(find.byType(FormErrorBanner), findsNothing);
        expect(_pictureWrites(server), hasLength(1));
        expect(tester.takeException(), isNull);
      });
    }
  });

  group('a picture action in flight', () {
    /// Every control of the screen is off, and [working] shows it is busy.
    void expectLocked(WidgetTester tester, String working) {
      expect(textFieldOf(tester, _name).enabled, isFalse);
      expect(textFieldOf(tester, _bio).enabled, isFalse);
      expect(tester.widget<ListTile>(_languages).enabled, isFalse);
      expect(
        tester.widget<PrimaryButton>(find.byType(PrimaryButton)).onPressed,
        isNull,
      );
      // Save isn't what is working.
      expect(
        tester.widget<PrimaryButton>(find.byType(PrimaryButton)).busy,
        isFalse,
      );
      for (final button in tester.widgetList<SecondaryButton>(
        find.byType(SecondaryButton),
      )) {
        expect(button.onPressed, isNull, reason: button.label);
        expect(button.busy, button.label == working, reason: button.label);
      }
    }

    /// Nothing leaves, asks, opens or sends while the action runs.
    Future<void> expectHeld(WidgetTester tester, TestApp app) async {
      await tester.tap(_save, warnIfMissed: false);
      await tester.pump();
      await tester.tap(_languages, warnIfMissed: false);
      await tester.pump();
      await tester.tap(_cancel, warnIfMissed: false);
      await tester.pump();
      await tester.binding.handlePopRoute();
      await tester.pump();
      await tester.pageBack();
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(_form, findsOneWidget);
      expect(_dialog, findsNothing);
      expect(_editor, findsNothing);
      expect(_puts(app.server), isEmpty);
      expect(app.location(tester), '/profile/edit');
    }

    testWidgets('an upload that hasn\'t answered: everything is locked, back '
        'does nothing, and a second tap sends nothing', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend();
      _hasPicture(server);
      final app = await _openSaved(tester, server);
      app.photos.next(_photo);
      server.once('PUT', ApiPaths.myAvatar, (_) => reply.future);
      await tester.pumpAndSettle();

      await tester.ensureVisible(_change);
      await tester.pumpAndSettle();
      // Twice before a frame: the second finds the action started.
      await tester.tap(_change);
      await tester.tap(_change, warnIfMissed: false);
      await tester.pump();
      await tester.pump();

      expectLocked(tester, l10n.profileAvatarChangeButton);
      await tester.tap(_change, warnIfMissed: false);
      await tester.tap(_remove, warnIfMissed: false);
      await tester.pump();
      await expectHeld(tester, app);
      expect(app.photos.calls, 1);
      expect(_pictureWrites(server), hasLength(1));
      // The earlier picture until the answer.
      expect(_shown(tester), testPicture);

      final stored = Uint8List.fromList([...testPicture, 0]);
      reply.complete(imageResponse(stored));
      await tester.pumpAndSettle();
      expect(_shown(tester), stored);
      expect(_pictureWrites(server), hasLength(1));
      expect(textFieldOf(tester, _name).enabled, isTrue);
      expect(
        tester.widget<PrimaryButton>(find.byType(PrimaryButton)).onPressed,
        isNotNull,
      );
      expect(
        _pictureButton(tester, l10n.profileAvatarChangeButton).busy,
        isFalse,
      );
      // And leaving works again.
      await tapAndSettle(tester, _cancel);
      expect(_form, findsNothing);
    });

    testWidgets('a removal that hasn\'t answered locks the screen the same '
        'way', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend();
      _hasPicture(server);
      final app = await _openSaved(tester, server);
      server.once('DELETE', ApiPaths.myAvatar, (_) => reply.future);

      await tapAndSettle(tester, _remove);
      await tester.tap(find.text(l10n.profileAvatarRemoveConfirm));
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));

      expect(_dialog, findsNothing);
      expectLocked(tester, l10n.profileAvatarRemoveButton);
      await tester.tap(_remove, warnIfMissed: false);
      await tester.tap(_change, warnIfMissed: false);
      await tester.pump();
      await expectHeld(tester, app);
      expect(app.photos.calls, 0);
      expect(_pictureWrites(server), hasLength(1));
      expect(_shown(tester), testPicture);

      reply.complete(noContent());
      await tester.pumpAndSettle();
      expect(_shown(tester), isNull);
      expect(_add, findsOneWidget);
      expect(textFieldOf(tester, _name).enabled, isTrue);
    });

    testWidgets('while the chooser is open the screen is held too', (
      tester,
    ) async {
      final chooser = Completer<Uint8List?>();
      final server = _backend();
      final app = await _openSaved(tester, server);
      app.photos.wait(chooser);

      await tester.ensureVisible(_add);
      await tester.pumpAndSettle();
      await tester.tap(_add);
      await tester.pump();

      expectLocked(tester, l10n.profileAvatarAddButton);
      await expectHeld(tester, app);
      expect(_pictureWrites(server), isEmpty);

      chooser.complete(null);
      await tester.pumpAndSettle();
      expect(_pictureWrites(server), isEmpty);
      expect(textFieldOf(tester, _name).enabled, isTrue);
      expect(
        _pictureButton(tester, l10n.profileAvatarAddButton).onPressed,
        isNotNull,
      );
    });

    testWidgets('a save in flight disables the picture controls', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend();
      _hasPicture(server);
      final app = await _openSaved(tester, server);
      server.once('PUT', ApiPaths.profile, (_) => reply.future);

      await tester.ensureVisible(_save);
      await tester.pumpAndSettle();
      await tester.tap(_save);
      await tester.pump();

      expect(
        _pictureButton(tester, l10n.profileAvatarChangeButton).onPressed,
        isNull,
      );
      expect(
        _pictureButton(tester, l10n.profileAvatarRemoveButton).onPressed,
        isNull,
      );
      await tester.tap(_change, warnIfMissed: false);
      await tester.tap(_remove, warnIfMissed: false);
      await tester.pump();
      expect(app.photos.calls, 0);
      expect(_dialog, findsNothing);
      expect(_pictureWrites(server), isEmpty);

      reply.complete(jsonResponse(200, profileBody()));
      await tester.pumpAndSettle();
      expect(_page, findsOneWidget);
    });

    testWidgets('a photo chosen after the form was left is not sent', (
      tester,
    ) async {
      final chooser = Completer<Uint8List?>();
      final server = _backend();
      final app = await _openSaved(tester, server);
      app.photos.wait(chooser);
      await tester.ensureVisible(_add);
      await tester.pumpAndSettle();
      await tester.tap(_add);
      await tester.pump();

      // Only the session ending can take the form away meanwhile.
      await app.session.logout();
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);

      chooser.complete(_photo);
      await tester.pumpAndSettle();
      expect(_pictureWrites(server), isEmpty);
      expect(tester.takeException(), isNull);
    });
  });

  testWidgets('the location stays /profile/edit while editing, failing and '
      'asking', (tester) async {
    final server = _backend();
    final app = await _openSaved(tester, server);
    final seen = <String>{app.location(tester)};

    await tester.enterText(_name, 'Ana María');
    await tester.enterText(_bio, 'Evenings');
    await tester.pumpAndSettle();
    seen.add(app.location(tester));
    server.once(
      'PUT',
      ApiPaths.profile,
      (_) => _validation([('bio', 'too_long')]),
    );
    await tapAndSettle(tester, _save);
    seen.add(app.location(tester));
    await _systemBack(tester);
    seen.add(app.location(tester));
    await tapAndSettle(tester, find.text(l10n.languagesDiscardKeep));
    seen.add(app.location(tester));

    expect(seen, {'/profile/edit'});
  });
}

import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/auth/google_identity_exception.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/google_sign_in_button.dart';

import '../support/fakes.dart';
import '../support/pictures.dart';
import 'harness.dart';

/// Secrets with distinctive markers, so any copy can be found.
const _email = 'leak.email@example.com';
const _password = 'LEAK-password-123';
final _access = accessToken('LEAKaccess');
final _refresh = refreshToken('LEAKrefresh');
const _idToken = 'LEAKidtoken.payload.signature';
const _idToken2 = 'LEAKidtokenTwo.payload.signature';

/// A member's name and text, with markers of their own: shown on screen on
/// purpose, and found nowhere else.
const _name = 'PRIVname Ana';
const _bio = 'PRIVbio evenings';

/// A photo from the member's device, with a marker of its own: sent only in
/// the body of their own upload, and never shown as text.
final _photo = utf8.encode('PHOTOmark-bytes-of-a-photo');

/// An error body about a picture, with [code] and text of its own that
/// echoes secrets and the photo's marker.
http.Response _echoPicture(int status, String code) => http.Response(
  '{"error":{"code":"$code","detail":"$_password $_email $_access '
  'PHOTOmark","fields":[{"field":"avatar","code":"too_large",'
  '"detail":"PHOTOmark $_email"}]}}',
  status,
  headers: {'content-type': 'application/json'},
);

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
        expect(location, isNot(contains('PRIV')), reason: location);
        expect(location, isNot(contains('PHOTO')), reason: location);
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
    expect(text, isNot(contains('PHOTO')), reason: text);
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
    // The profile page: an echoing load failure, then none saved. The edit
    // screen: none saved, an echoing refusal of the save, then saved. Every
    // load after that answers the saved profile.
    ..once('GET', ApiPaths.profile, (_) => _echo(500))
    ..once('GET', ApiPaths.profile, (_) => noProfile())
    ..once('GET', ApiPaths.profile, (_) => noProfile())
    ..once('PUT', ApiPaths.profile, (_) => _echo(422))
    ..once(
      'PUT',
      ApiPaths.profile,
      (_) => jsonResponse(200, profileBody(displayName: _name, bio: _bio)),
    )
    ..always(
      'GET',
      ApiPaths.profile,
      (_) => jsonResponse(200, profileBody(displayName: _name, bio: _bio)),
    )
    // The picture: an echoing load failure on the edit screen, an echoing
    // refusal of the upload, then stored; an echoing failure of the removal,
    // then removed. Every other read answers that there is none.
    ..once('GET', ApiPaths.myAvatar, (_) => _echo(500))
    ..always('GET', ApiPaths.myAvatar, (_) => noAvatar())
    ..once(
      'PUT',
      ApiPaths.myAvatar,
      (_) => _echoPicture(422, 'validation_failed'),
    )
    ..once('PUT', ApiPaths.myAvatar, (_) => imageResponse(testPicture))
    ..once(
      'DELETE',
      ApiPaths.myAvatar,
      (_) => _echoPicture(503, 'service_unavailable'),
    )
    ..once('DELETE', ApiPaths.myAvatar, (_) => noContent())
    // The page's languages: an echoing load failure, then a selection with
    // a language the catalog doesn't name.
    ..always('GET', ApiPaths.languages, (_) => jsonResponse(200, catalogBody()))
    ..once('GET', ApiPaths.myLanguages, (_) => _echo(500))
    ..once(
      'GET',
      ApiPaths.myLanguages,
      (_) => jsonResponse(
        200,
        languagesBody(spoken: [('es', 'native')], learning: [('xx', 'a2')]),
      ),
    )
    // The languages editor: loaded, an echoing refusal of the save, then
    // saved, and the page's section loading what was stored.
    ..once(
      'GET',
      ApiPaths.myLanguages,
      (_) => jsonResponse(
        200,
        languagesBody(spoken: [('es', 'native')], learning: [('xx', 'a2')]),
      ),
    )
    ..once('PUT', ApiPaths.myLanguages, (_) => _echo(422))
    ..once(
      'PUT',
      ApiPaths.myLanguages,
      (request) => http.Response(
        request.body,
        200,
        headers: {'content-type': 'application/json'},
      ),
    )
    ..once(
      'GET',
      ApiPaths.myLanguages,
      (_) => jsonResponse(
        200,
        languagesBody(
          spoken: [('es', 'native'), ('ja', 'b1')],
          learning: [('xx', 'a2')],
        ),
      ),
    )
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
  final photos = FakePhotoSource()
    ..next(_photo)
    ..next(_photo);
  final app = await pumpApp(
    tester,
    server: server,
    google: google,
    photos: photos,
  );
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

  // The profile page: its route names nobody, and neither the account's
  // email nor anything a server echoes is shown on it.
  void onPage() {
    record();
    expect(app.location(tester), '/profile');
    _checkScreen(tester);
  }

  await tapAndSettle(
    tester,
    find.widgetWithText(OutlinedButton, l10n.profileButton),
  );
  onPage();
  expect(find.byType(FormErrorBanner), findsOneWidget);
  await tapAndSettle(tester, find.widgetWithText(FilledButton, l10n.tryAgain));
  onPage();
  expect(find.text(l10n.profileEmptyMessage), findsOneWidget);

  // The edit screen: its route names nobody either, whatever is typed,
  // refused or saved on it.
  void onForm() {
    record();
    expect(app.location(tester), '/profile/edit');
    _checkScreen(tester);
  }

  await tapAndSettle(
    tester,
    find.widgetWithText(FilledButton, l10n.profileEmptyButton),
  );
  onForm();

  // The picture control, whose load failed with an echoing body: a refused
  // photo, the photo stored, a failed removal, the removal. Each message is
  // the app's own, and nothing of the photo is ever text.
  final addPhoto = find.widgetWithText(
    OutlinedButton,
    l10n.profileAvatarAddButton,
  );
  final removePhoto = find.widgetWithText(
    OutlinedButton,
    l10n.profileAvatarRemoveButton,
  );
  expect(find.byType(FormErrorBanner), findsNothing);
  await tapAndSettle(tester, addPhoto);
  onForm();
  expect(find.text(l10n.errorAvatarTooLarge), findsOneWidget);
  await tapAndSettle(tester, addPhoto);
  onForm();
  expect(find.byType(FormErrorBanner), findsNothing);
  await tapAndSettle(tester, removePhoto);
  onForm();
  expect(find.text(l10n.profileAvatarRemoveTitle), findsOneWidget);
  await tapAndSettle(tester, find.text(l10n.profileAvatarRemoveConfirm));
  onForm();
  expect(find.byType(FormErrorBanner), findsOneWidget);
  await tapAndSettle(tester, removePhoto);
  onForm();
  await tapAndSettle(tester, find.text(l10n.profileAvatarRemoveConfirm));
  onForm();
  expect(find.byType(FormErrorBanner), findsNothing);
  expect(addPhoto, findsOneWidget);
  // The photo travels only in the bodies of the member's own uploads.
  final uploads = [
    for (final r in server.requests)
      if (latin1.decode(r.bodyBytes).contains('PHOTOmark') ||
          r.url.toString().contains('PHOTO') ||
          r.headers.values.any((v) => v.contains('PHOTO')))
        r,
  ];
  expect(uploads, hasLength(2));
  for (final r in uploads) {
    expect(r.method, 'PUT');
    expect(r.url.path, ApiPaths.myAvatar);
    expect(r.url.hasQuery, isFalse);
    expect(r.bodyBytes, _photo);
  }

  final save = find.widgetWithText(FilledButton, l10n.profileSaveButton);
  await tester.enterText(field(l10n.displayNameLabel), _name);
  await tester.enterText(field(l10n.bioLabel), _bio);
  await tester.pumpAndSettle();
  onForm();
  await tapAndSettle(tester, save);
  onForm();
  expect(find.byType(FormErrorBanner), findsOneWidget);
  await tapAndSettle(tester, save);

  // Saved: the page shows the member's own name and text. Its languages
  // failed with an echoing body; their retry shows them, on the screen and
  // never in the route.
  onPage();
  expect(find.text(_name), findsOneWidget);
  expect(find.text(_bio), findsOneWidget);
  expect(find.widgetWithText(OutlinedButton, l10n.tryAgain), findsOneWidget);
  await tapAndSettle(
    tester,
    find.widgetWithText(OutlinedButton, l10n.tryAgain),
  );
  onPage();
  expect(find.text('Spanish'), findsOneWidget);
  expect(find.text('xx'), findsOneWidget);

  // The edit screen again, and its question about unsaved text.
  await tapAndSettle(
    tester,
    find.widgetWithText(OutlinedButton, l10n.profileEditButton),
  );
  onForm();
  await tester.enterText(field(l10n.bioLabel), '$_bio, weekends');
  await tester.pumpAndSettle();
  await tapAndSettle(
    tester,
    find.widgetWithText(OutlinedButton, l10n.profileCancelButton),
  );
  onForm();
  expect(find.text(l10n.profileDiscardMessage), findsOneWidget);
  await tapAndSettle(tester, find.text(l10n.languagesDiscardKeep));
  onForm();

  // The languages editor: its route names nobody and holds no language,
  // whatever is picked, refused or saved on it.
  void onEditor() {
    record();
    expect(app.location(tester), '/profile/languages');
    _checkScreen(tester);
  }

  await tapAndSettle(
    tester,
    find.widgetWithText(ListTile, l10n.profileEditLanguagesButton),
  );
  onEditor();
  expect(find.text(l10n.languagesVisibilityNotice), findsOneWidget);
  await tapAndSettle(tester, find.text(l10n.languagesAddButton).first);
  onEditor();
  await tapAndSettle(tester, find.text('Japanese'));
  onEditor();
  await tapAndSettle(tester, find.text(l10n.languageLevelB1));
  onEditor();
  final saveLanguages = find.widgetWithText(
    FilledButton,
    l10n.languagesSaveButton,
  );
  await tapAndSettle(tester, saveLanguages);
  onEditor();
  expect(find.byType(FormErrorBanner), findsOneWidget);
  await tester.pageBack();
  await tester.pumpAndSettle();
  onEditor();
  expect(find.text(l10n.languagesDiscardTitle), findsOneWidget);
  await tapAndSettle(tester, find.text(l10n.languagesDiscardKeep));
  await tapAndSettle(tester, saveLanguages);
  // Back on the edit screen, with the text as it was left.
  onForm();
  expect(find.text('Japanese'), findsNothing);
  // A member's languages travel only in the body of their own save.
  final saves = [
    for (final r in server.requests)
      if (r.body.contains('"language"')) r,
  ];
  expect(saves, hasLength(2));
  for (final r in saves) {
    expect(r.method, 'PUT');
    expect(r.url.path, ApiPaths.myLanguages);
    expect(r.url.hasQuery, isFalse);
    expect(r.body, contains('"ja"'));
  }

  // Leaving with the unsaved text, discarded: the page loads what is stored.
  await tester.pageBack();
  await tester.pumpAndSettle();
  onForm();
  expect(find.text(l10n.profileDiscardMessage), findsOneWidget);
  await tapAndSettle(tester, find.text(l10n.languagesDiscardConfirm));
  onPage();
  expect(find.text(_bio), findsOneWidget);
  expect(find.text('Japanese'), findsOneWidget);
  // The name and the text travel only in the bodies of the member's own
  // saves of the profile: the discarded text was sent nowhere.
  final carriers = [
    for (final r in server.requests)
      if (r.body.contains('PRIV') ||
          r.url.toString().contains('PRIV') ||
          r.headers.values.any((v) => v.contains('PRIV')))
        r,
  ];
  expect(carriers, hasLength(2));
  for (final r in carriers) {
    expect(r.method, 'PUT');
    expect(r.url.path, ApiPaths.profile);
    expect(r.url.hasQuery, isFalse);
    expect(r.body, isNot(contains('weekends')));
  }

  await tester.pageBack();
  await tester.pumpAndSettle();
  record();
  expect(app.location(tester), '/home');

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

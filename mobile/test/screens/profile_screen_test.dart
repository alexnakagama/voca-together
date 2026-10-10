import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/screens/home_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/member_profile_screen.dart';
import 'package:vocatogether/screens/profile_edit_screen.dart';
import 'package:vocatogether/screens/profile_languages_section.dart';
import 'package:vocatogether/screens/profile_screen.dart';
import 'package:vocatogether/session.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/language_chip.dart';
import 'package:vocatogether/ui/widgets/profile_avatar.dart';
import 'package:vocatogether/ui/widgets/profile_header.dart';

import '../support/fakes.dart';
import '../support/pictures.dart';
import 'harness.dart';

Finder get _open => find.widgetWithText(OutlinedButton, l10n.profileButton);
Finder get _page => find.byType(ProfileScreen);
Finder get _form => find.byType(ProfileEditScreen);
Finder get _edit => find.widgetWithText(OutlinedButton, l10n.profileEditButton);
Finder get _create =>
    find.widgetWithText(FilledButton, l10n.profileEmptyButton);
Finder get _retry => find.widgetWithText(FilledButton, l10n.tryAgain);
Finder get _save => find.widgetWithText(FilledButton, l10n.profileSaveButton);
Finder get _cancel =>
    find.widgetWithText(OutlinedButton, l10n.profileCancelButton);
Finder get _section => find.byType(ProfileLanguagesSection);
Finder get _seePublic => find.byTooltip(l10n.profileSeePublicButton);
Finder get _member => find.byType(MemberProfileScreen);

/// Anything that takes text, or that would change the profile from here.
Finder get _textFields =>
    find.byWidgetPredicate((w) => w is TextField || w is EditableText);

/// A backend for a signed-in user on home, with the three-language catalog
/// and no languages chosen; the profile calls are scripted by each test.
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

Responder _profile({String name = 'Ana', String bio = ''}) =>
    (_) => jsonResponse(200, profileBody(displayName: name, bio: bio));

/// Signs in on home and opens the profile page, whose load must be
/// scripted.
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
  expect(_page, findsOneWidget);
  return app;
}

Responder get _picture =>
    (_) => imageResponse(testPicture);

/// The picture the page's header shows, or null for the placeholder.
Uint8List? _shown(WidgetTester tester) =>
    tester.widget<ProfileHeader>(find.byType(ProfileHeader)).image;

/// The chips on screen as (name, level), in order.
List<(String, String)> _chips(WidgetTester tester) => [
  for (final chip in tester.widgetList<LanguageChip>(find.byType(LanguageChip)))
    (chip.name, chip.level),
];

void main() {
  group('the page is read-only', () {
    testWidgets('a profile with a name, a text and languages shows all of '
        'them, "Edit Profile", and nothing to type in', (tester) async {
      final server = _backend()
        ..always(
          'GET',
          ApiPaths.profile,
          _profile(name: 'Ana López', bio: 'Hi'),
        )
        ..always(
          'GET',
          ApiPaths.myLanguages,
          (_) => jsonResponse(
            200,
            languagesBody(
              spoken: [('es', 'native'), ('en', 'c1')],
              learning: [('ja', 'a2')],
            ),
          ),
        );
      final handle = tester.ensureSemantics();
      final app = await _openProfile(tester, server);

      expect(app.location(tester), '/profile');
      final header = tester.widget<ProfileHeader>(find.byType(ProfileHeader));
      expect(header.name, 'Ana López');
      expect(header.bio, 'Hi');
      expect(find.text('Ana López'), findsOneWidget);
      expect(find.text('Hi'), findsOneWidget);
      expect(
        tester.getSemantics(find.text('Ana López')),
        isSemantics(label: 'Ana López', isHeader: true),
      );
      // No picture yet: the placeholder, with the name's first character,
      // read as the profile picture.
      expect(header.image, isNull);
      expect(
        find.descendant(
          of: find.byType(ProfileAvatar),
          matching: find.text('A'),
        ),
        findsOneWidget,
      );
      expect(
        tester.getSemantics(find.byType(ProfileAvatar)),
        isSemantics(isImage: true, label: l10n.profileAvatarPlaceholderLabel),
      );
      expect(_chips(tester), [
        ('Spanish', 'Native'),
        ('English', 'C1'),
        ('Japanese', 'A2'),
      ]);
      expect(_edit, findsOneWidget);
      expect(_textFields, findsNothing);
      expect(_save, findsNothing);
      // The one control under the app bar opens the edit screen: nothing
      // here changes the profile, the picture or the languages.
      final controls = find.descendant(
        of: find.descendant(
          of: _page,
          matching: find.byType(SingleChildScrollView),
        ),
        matching: find.byWidgetPredicate(
          (w) => w is ButtonStyleButton || w is ListTile || w is Checkbox,
        ),
      );
      expect(controls, findsOneWidget);
      expect(tester.widget(controls), tester.widget(_edit));
      expect(find.text(l10n.profileEmptyMessage), findsNothing);
      handle.dispose();
    });

    testWidgets('with no text, the name and nothing in the text\'s place', (
      tester,
    ) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      await _openProfile(tester, server);

      final header = find.byType(ProfileHeader);
      expect(tester.widget<ProfileHeader>(header).bio, '');
      // The picture and the name, and no third text or label under them.
      expect(
        tester
            .widgetList<Text>(
              find.descendant(of: header, matching: find.byType(Text)),
            )
            .map((t) => t.data),
        ['A', 'Ana'],
      );
      expect(find.text(l10n.bioLabel), findsNothing);
    });

    testWidgets('the name and the text are the ones the server returned', (
      tester,
    ) async {
      // As stored: nothing is trimmed, cut or rewritten on the way.
      const name = 'Ana  María  O’Neill-López';
      const bio = 'Line one\n\nLine two   ';
      final server = _backend()
        ..always('GET', ApiPaths.profile, _profile(name: name, bio: bio));
      await _openProfile(tester, server);

      expect(find.text(name), findsOneWidget);
      expect(find.text(bio), findsOneWidget);
      expect(server.to(ApiPaths.profile).single.method, 'GET');
      expect(server.to(ApiPaths.profile).single.url.hasQuery, isFalse);
    });
  });

  group('the Friends placeholder', () {
    testWidgets('a heading and "Coming later", with no count', (tester) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      final handle = tester.ensureSemantics();
      await _openProfile(tester, server);

      expect(find.text(l10n.profileFriendsHeading), findsOneWidget);
      expect(
        tester.getSemantics(find.text(l10n.profileFriendsHeading)),
        isSemantics(label: l10n.profileFriendsHeading, isHeader: true),
      );
      expect(find.text(l10n.profileFriendsComingLater), findsOneWidget);
      for (final text in [
        l10n.profileFriendsHeading,
        l10n.profileFriendsComingLater,
      ]) {
        expect(text, isNot(contains(RegExp(r'\d'))));
      }
      // Between the name and the languages.
      final friends = tester.getTopLeft(find.text(l10n.profileFriendsHeading));
      expect(tester.getTopLeft(find.text('Ana')).dy, lessThan(friends.dy));
      expect(friends.dy, lessThan(tester.getTopLeft(_section).dy));
      handle.dispose();
    });

    testWidgets('tapping it does nothing and asks for nothing', (tester) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      final app = await _openProfile(tester, server);
      final requests = server.requests.length;

      for (final text in [
        l10n.profileFriendsHeading,
        l10n.profileFriendsComingLater,
      ]) {
        await tester.ensureVisible(find.text(text));
        await tester.pumpAndSettle();
        await tester.tap(find.text(text));
        await tester.pumpAndSettle();
        expect(
          find.ancestor(
            of: find.text(text),
            matching: find.byWidgetPredicate(
              (w) => w is ButtonStyleButton || w is InkResponse,
            ),
          ),
          findsNothing,
        );
      }
      expect(app.location(tester), '/profile');
      expect(_page, findsOneWidget);
      expect(_form, findsNothing);
      expect(server.requests, hasLength(requests));
    });
  });

  group('languages on the page', () {
    testWidgets('are asked for once the profile has loaded, and shown under '
        'it', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.profile, (_) => reply.future)
        ..always(
          'GET',
          ApiPaths.myLanguages,
          (_) => jsonResponse(200, languagesBody(spoken: [('es', 'native')])),
        );
      await _openProfile(tester, server, settle: false);

      expect(_section, findsNothing);
      expect(server.count(ApiPaths.languages), 0);
      expect(server.count(ApiPaths.myLanguages), 0);

      reply.complete(jsonResponse(200, profileBody(displayName: 'Ana')));
      await tester.pumpAndSettle();
      expect(find.text(l10n.languagesHeading), findsOneWidget);
      expect(_chips(tester), [('Spanish', 'Native')]);
      expect(server.count(ApiPaths.languages), 1);
      expect(server.count(ApiPaths.myLanguages), 1);
    });

    testWidgets('with none chosen, one text and neither heading', (
      tester,
    ) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      await _openProfile(tester, server);

      expect(find.text(l10n.languagesEmpty), findsOneWidget);
      expect(find.text(l10n.languagesSpokenHeading), findsNothing);
      expect(find.text(l10n.languagesLearningHeading), findsNothing);
    });

    testWidgets('a failure of theirs leaves the rest of the page as it is', (
      tester,
    ) async {
      final server = _backend()
        ..always('GET', ApiPaths.profile, _profile(bio: 'Hi'))
        ..once('GET', ApiPaths.myLanguages, networkFailure);
      await _openProfile(tester, server);

      expect(
        find.descendant(of: _section, matching: find.text(l10n.errorNetwork)),
        findsOneWidget,
      );
      expect(find.byType(FormErrorBanner), findsOneWidget);
      expect(find.text('Ana'), findsOneWidget);
      expect(find.text('Hi'), findsOneWidget);
      expect(find.byType(ProfileAvatar), findsOneWidget);
      expect(_edit, findsOneWidget);
      // The page's own retry is not shown: the profile loaded.
      expect(_retry, findsNothing);

      await tapAndSettle(
        tester,
        find.widgetWithText(OutlinedButton, l10n.tryAgain),
      );
      expect(find.text(l10n.languagesEmpty), findsOneWidget);
      // The section's retry asked for the languages, not for the profile.
      expect(server.count(ApiPaths.profile), 1);
    });
  });

  group('a member without a profile', () {
    testWidgets('is invited to create one, and sees nothing of a profile', (
      tester,
    ) async {
      final server = _backend()
        ..always('GET', ApiPaths.profile, (_) => noProfile());
      final app = await _openProfile(tester, server);

      expect(find.text(l10n.profileEmptyMessage), findsOneWidget);
      expect(_create, findsOneWidget);
      expect(find.byType(ProfileHeader), findsNothing);
      expect(find.byType(ProfileAvatar), findsNothing);
      expect(find.text(l10n.profileFriendsHeading), findsNothing);
      expect(_section, findsNothing);
      expect(find.text(l10n.languagesHeading), findsNothing);
      expect(_edit, findsNothing);
      expect(_textFields, findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      // Nothing of a profile was asked for either.
      expect(server.count(ApiPaths.languages), 0);
      expect(server.count(ApiPaths.myLanguages), 0);

      await tapAndSettle(tester, _create);
      expect(_form, findsOneWidget);
      expect(app.location(tester), '/profile/edit');
      expect(find.text(l10n.profileCreateHeading), findsOneWidget);
    });

    testWidgets('after creating one, the page shows it', (tester) async {
      final server = _backend()
        ..always('GET', ApiPaths.profile, (_) => noProfile());
      final app = await _openProfile(tester, server);
      await tapAndSettle(tester, _create);

      server
        ..once(
          'PUT',
          ApiPaths.profile,
          _profile(name: 'Ana López', bio: 'New here'),
        )
        ..always(
          'GET',
          ApiPaths.profile,
          _profile(name: 'Ana López', bio: 'New here'),
        );
      await tester.enterText(field(l10n.displayNameLabel), 'Ana López');
      await tester.enterText(field(l10n.bioLabel), 'New here');
      await tapAndSettle(tester, _save);

      expect(_form, findsNothing);
      expect(app.location(tester), '/profile');
      expect(find.text(l10n.profileEmptyMessage), findsNothing);
      expect(find.text('Ana López'), findsOneWidget);
      expect(find.text('New here'), findsOneWidget);
      expect(find.text(l10n.profileFriendsHeading), findsOneWidget);
      expect(find.text(l10n.languagesEmpty), findsOneWidget);
      expect(_edit, findsOneWidget);
    });
  });

  group('the edit screen and its result', () {
    testWidgets('"Edit Profile" opens /profile/edit on top of the page', (
      tester,
    ) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      final app = await _openProfile(tester, server);
      await tapAndSettle(tester, _edit);

      expect(_form, findsOneWidget);
      expect(app.location(tester), '/profile/edit');
      // One step back is the page, and another is home.
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(_form, findsNothing);
      expect(_page, findsOneWidget);
      expect(app.location(tester), '/profile');
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(find.byType(HomeScreen), findsOneWidget);
    });

    testWidgets('a rapid double tap opens the edit screen once', (
      tester,
    ) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      final app = await _openProfile(tester, server);
      // Twice in the same frame, before the screen covers the button.
      final button = tester.getCenter(_edit);
      await tester.tapAt(button);
      await tester.tapAt(button);
      await tester.pumpAndSettle();
      expect(_form, findsOneWidget);

      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(_form, findsNothing);
      expect(_page, findsOneWidget);
      expect(app.location(tester), '/profile');
      // The page, the form, and the page again.
      expect(server.count(ApiPaths.profile), 3);

      // And the button opens it again afterwards.
      await tapAndSettle(tester, _edit);
      expect(_form, findsOneWidget);
    });

    testWidgets('after a save, the page shows the new name without being '
        'opened again', (tester) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      final app = await _openProfile(tester, server);
      await tapAndSettle(tester, _edit);

      server
        ..once('PUT', ApiPaths.profile, _profile(name: 'Ana María'))
        ..always('GET', ApiPaths.profile, _profile(name: 'Ana María'));
      await tester.enterText(field(l10n.displayNameLabel), 'Ana María');
      await tapAndSettle(tester, _save);

      expect(app.location(tester), '/profile');
      expect(find.text('Ana María'), findsOneWidget);
      expect(find.text('Ana'), findsNothing);
      // The page, the form, and the page again, with its languages.
      expect(server.to(ApiPaths.profile).map((r) => r.method), [
        'GET',
        'GET',
        'PUT',
        'GET',
      ]);
      expect(server.count(ApiPaths.languages), 2);
      expect(server.count(ApiPaths.myLanguages), 2);
    });

    final waysBack = <String, Future<void> Function(WidgetTester)>{
      'Cancel': (tester) => tapAndSettle(tester, _cancel),
      'the app bar\'s back': (tester) async {
        await tester.pageBack();
        await tester.pumpAndSettle();
      },
      'the system\'s back': (tester) async {
        await tester.binding.handlePopRoute();
        await tester.pumpAndSettle();
      },
    };
    waysBack.forEach((name, leave) {
      testWidgets('after leaving by $name without saving, the page loads '
          'again and shows what is stored', (tester) async {
        final server = _backend()
          ..always('GET', ApiPaths.profile, _profile(bio: 'Old'));
        await _openProfile(tester, server);
        await tapAndSettle(tester, _edit);

        // What the server has by now, whoever stored it.
        server
          ..always('GET', ApiPaths.profile, _profile(bio: 'Newer'))
          ..always(
            'GET',
            ApiPaths.myLanguages,
            (_) => jsonResponse(200, languagesBody(learning: [('ja', 'b1')])),
          );
        await leave(tester);

        expect(_form, findsNothing);
        expect(find.text('Newer'), findsOneWidget);
        expect(find.text('Old'), findsNothing);
        expect(_chips(tester), [('Japanese', 'B1')]);
        expect(server.to(ApiPaths.profile).map((r) => r.method), [
          'GET',
          'GET',
          'GET',
        ]);
      });
    });

    testWidgets('the reload shows the spinner, not the profile it showed '
        'before', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      final handle = tester.ensureSemantics();
      await _openProfile(tester, server);
      await tapAndSettle(tester, _edit);

      server.once('GET', ApiPaths.profile, (_) => reply.future);
      await tester.pageBack();
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(find.bySemanticsLabel(l10n.profileLoading), findsOneWidget);
      expect(find.byType(ProfileHeader), findsNothing);
      expect(_section, findsNothing);
      expect(_edit, findsNothing);

      reply.complete(jsonResponse(200, profileBody(displayName: 'Ana')));
      await tester.pumpAndSettle();
      expect(find.text('Ana'), findsOneWidget);
      handle.dispose();
    });

    testWidgets('a failed reload shows the failure and a retry, and nothing '
        'of the earlier profile', (tester) async {
      final server = _backend()
        ..always('GET', ApiPaths.profile, _profile(bio: 'Old'))
        ..always(
          'GET',
          ApiPaths.myLanguages,
          (_) => jsonResponse(200, languagesBody(spoken: [('es', 'native')])),
        );
      final app = await _openProfile(tester, server);
      expect(_chips(tester), [('Spanish', 'Native')]);
      await tapAndSettle(tester, _edit);

      server.once('GET', ApiPaths.profile, networkFailure);
      await tester.pageBack();
      await tester.pumpAndSettle();

      expect(app.location(tester), '/profile');
      expect(find.text(l10n.errorNetwork), findsOneWidget);
      expect(_retry, findsOneWidget);
      expect(find.text('Ana'), findsNothing);
      expect(find.text('Old'), findsNothing);
      expect(find.byType(ProfileHeader), findsNothing);
      expect(find.text(l10n.profileFriendsHeading), findsNothing);
      expect(find.byType(LanguageChip), findsNothing);
      expect(_section, findsNothing);
      expect(_edit, findsNothing);
      expect(find.text(l10n.profileEmptyMessage), findsNothing);

      await tapAndSettle(tester, _retry);
      expect(find.text('Old'), findsOneWidget);
      expect(_chips(tester), [('Spanish', 'Native')]);
    });
  });

  group('loading and failure', () {
    testWidgets('a labelled spinner until the profile answers', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.profile, (_) => reply.future);
      final handle = tester.ensureSemantics();
      final app = await _openProfile(tester, server, settle: false);

      expect(app.location(tester), '/profile');
      expect(find.bySemanticsLabel(l10n.profileLoading), findsOneWidget);
      expect(find.byType(ProfileHeader), findsNothing);
      expect(find.text(l10n.profileFriendsHeading), findsNothing);
      expect(find.text(l10n.profileEmptyMessage), findsNothing);
      expect(_edit, findsNothing);
      expect(_create, findsNothing);

      reply.complete(jsonResponse(200, profileBody(displayName: 'Ana')));
      await tester.pumpAndSettle();
      expect(find.bySemanticsLabel(l10n.profileLoading), findsNothing);
      expect(find.text('Ana'), findsOneWidget);
      handle.dispose();
    });

    group('a failed load shows its message and a retry, and nothing of the '
        'profile', () {
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
        '503': (
          (_) => errorResponse(
            503,
            'service_unavailable',
            headers: {'retry-after': '5'},
          ),
          'VocaTogether is busy right now. Try again in 5 seconds.',
        ),
        'malformed 200': (
          (_) => jsonResponse(200, {'display_name': 'Ana'}),
          l10n.errorUnexpected,
        ),
        // Not the backend's "none saved": never shown as the empty state.
        'a 404 without the code': (
          (_) => http.Response('Not Found', 404),
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
          final reply = Completer<http.Response>();
          final server = _backend()
            ..once('GET', ApiPaths.profile, responder)
            ..once('GET', ApiPaths.profile, (_) => reply.future);
          final handle = tester.ensureSemantics();
          final app = await _openProfile(tester, server);

          expect(find.text(message), findsOneWidget);
          expect(find.textContaining('SERVERTEXT'), findsNothing);
          expect(find.byType(ProfileHeader), findsNothing);
          expect(find.text(l10n.profileFriendsHeading), findsNothing);
          expect(find.text(l10n.profileEmptyMessage), findsNothing);
          expect(_section, findsNothing);
          expect(_edit, findsNothing);
          expect(_create, findsNothing);
          expect(server.count(ApiPaths.myLanguages), 0);
          expect(app.session.status, SessionStatus.signedIn);
          expect(app.location(tester), '/profile');

          // The retry shows the spinner and asks again.
          await tester.tap(_retry);
          await tester.pump();
          expect(find.bySemanticsLabel(l10n.profileLoading), findsOneWidget);
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(_retry, findsNothing);

          reply.complete(jsonResponse(200, profileBody(displayName: 'Ana')));
          await tester.pumpAndSettle();
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(find.text('Ana'), findsOneWidget);
          expect(server.count(ApiPaths.profile), 2);
          handle.dispose();
        });
      });
    });

    testWidgets('a load that gets no answer times out', (tester) async {
      final server = _backend()..once('GET', ApiPaths.profile, neverAnswers);
      await _openProfile(tester, server, settle: false);

      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(_retry, findsOneWidget);
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
      expect(_page, findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
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

  group('the picture', () {
    testWidgets('a member with a picture sees it, read as their profile '
        'picture', (tester) async {
      final server = _backend()
        ..always('GET', ApiPaths.profile, _profile())
        ..always('GET', ApiPaths.myAvatar, _picture);
      final handle = tester.ensureSemantics();
      await _openProfile(tester, server);

      expect(_shown(tester), testPicture);
      expect(
        tester.getSemantics(find.byType(ProfileAvatar)),
        isSemantics(isImage: true, label: l10n.profileAvatarLabel),
      );
      expect(find.byType(FormErrorBanner), findsNothing);
      final request = server.to(ApiPaths.myAvatar).single;
      expect(request.method, 'GET');
      expect(request.url.hasQuery, isFalse);
      expect(request.body, isEmpty);
      handle.dispose();
    });

    testWidgets('a member without one sees the placeholder, asked for once', (
      tester,
    ) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      await _openProfile(tester, server);

      expect(_shown(tester), isNull);
      expect(
        find.descendant(
          of: find.byType(ProfileAvatar),
          matching: find.text('A'),
        ),
        findsOneWidget,
      );
      expect(server.count(ApiPaths.myAvatar), 1);
    });

    testWidgets('it loads on its own: the name and the languages are shown '
        'while it is still on its way', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..always('GET', ApiPaths.profile, _profile(bio: 'Hi'))
        ..once('GET', ApiPaths.myAvatar, (_) => reply.future);
      await _openProfile(tester, server);

      expect(find.text('Ana'), findsOneWidget);
      expect(find.text('Hi'), findsOneWidget);
      expect(find.text(l10n.languagesEmpty), findsOneWidget);
      expect(_edit, findsOneWidget);
      expect(_shown(tester), isNull);

      reply.complete(imageResponse(testPicture));
      await tester.pumpAndSettle();
      expect(_shown(tester), testPicture);
    });

    group('a picture that fails to load is the placeholder, with no error', () {
      final cases = <String, Responder>{
        'network': networkFailure,
        '429': (_) =>
            errorResponse(429, 'rate_limited', headers: {'retry-after': '6'}),
        '503': (_) => errorResponse(503, 'service_unavailable'),
        '500': (_) => errorResponse(500, 'internal_error'),
        // Not the backend's "no picture".
        'a 404 without the code': (_) => http.Response('Not Found', 404),
        'a body with text of its own': (_) => http.Response(
          '{"error":{"code":"internal_error","message":"SERVERTEXT"}}',
          500,
          headers: {'content-type': 'application/json'},
        ),
      };
      cases.forEach((name, responder) {
        testWidgets(name, (tester) async {
          final server = _backend()
            ..always('GET', ApiPaths.profile, _profile(bio: 'Hi'))
            ..once('GET', ApiPaths.myAvatar, responder)
            ..always(
              'GET',
              ApiPaths.myLanguages,
              (_) => jsonResponse(200, languagesBody(spoken: [('es', 'c1')])),
            );
          final app = await _openProfile(tester, server);

          expect(_shown(tester), isNull);
          expect(
            find.descendant(
              of: find.byType(ProfileAvatar),
              matching: find.text('A'),
            ),
            findsOneWidget,
          );
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(find.textContaining('SERVERTEXT'), findsNothing);
          expect(find.text('Ana'), findsOneWidget);
          expect(find.text('Hi'), findsOneWidget);
          expect(_chips(tester), [('Spanish', 'C1')]);
          expect(_edit, findsOneWidget);
          // Nothing retried it.
          expect(server.count(ApiPaths.myAvatar), 1);
          expect(app.session.status, SessionStatus.signedIn);
          expect(tester.takeException(), isNull);
        });
      });

      testWidgets('no answer in time', (tester) async {
        final server = _backend()
          ..always('GET', ApiPaths.profile, _profile())
          ..once('GET', ApiPaths.myAvatar, neverAnswers);
        await _openProfile(tester, server);
        await tester.pump(const Duration(seconds: 16));
        await tester.pumpAndSettle();

        expect(_shown(tester), isNull);
        expect(find.byType(FormErrorBanner), findsNothing);
        expect(find.text('Ana'), findsOneWidget);
        expect(tester.takeException(), isNull);
      });
    });

    testWidgets('a member with no profile is asked for no picture', (
      tester,
    ) async {
      final server = _backend()
        ..always('GET', ApiPaths.profile, (_) => noProfile());
      await _openProfile(tester, server);

      expect(_create, findsOneWidget);
      expect(find.byType(ProfileAvatar), findsNothing);
      expect(server.count(ApiPaths.myAvatar), 0);
    });

    testWidgets('a failed profile load asks for no picture', (tester) async {
      final server = _backend()..once('GET', ApiPaths.profile, networkFailure);
      await _openProfile(tester, server);

      expect(_retry, findsOneWidget);
      expect(server.count(ApiPaths.myAvatar), 0);
    });

    testWidgets('after a picture was set on the edit screen, the page shows '
        'it', (tester) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      await _openProfile(tester, server);
      expect(_shown(tester), isNull);

      // Stored from now on: the edit screen's own load, then the page's.
      server.always('GET', ApiPaths.myAvatar, _picture);
      await tapAndSettle(tester, _edit);
      expect(_form, findsOneWidget);
      await tapAndSettle(tester, _cancel);

      expect(_form, findsNothing);
      expect(_shown(tester), testPicture);
      // The page, the edit screen, the page again.
      expect(server.count(ApiPaths.myAvatar), 3);
    });

    testWidgets('after the picture was removed on the edit screen, the page '
        'shows the placeholder', (tester) async {
      final server = _backend()
        ..always('GET', ApiPaths.profile, _profile())
        ..once('GET', ApiPaths.myAvatar, _picture);
      await _openProfile(tester, server);
      expect(_shown(tester), testPicture);

      await tapAndSettle(tester, _edit);
      await tapAndSettle(tester, _cancel);

      expect(_shown(tester), isNull);
      expect(
        find.descendant(
          of: find.byType(ProfileAvatar),
          matching: find.text('A'),
        ),
        findsOneWidget,
      );
    });

    testWidgets('the reload drops the picture it showed, also when the new '
        'one fails', (tester) async {
      final server = _backend()
        ..always('GET', ApiPaths.profile, _profile())
        ..once('GET', ApiPaths.myAvatar, _picture);
      await _openProfile(tester, server);
      expect(_shown(tester), testPicture);

      server
        ..once('GET', ApiPaths.myAvatar, _picture)
        ..once('GET', ApiPaths.myAvatar, networkFailure);
      await tapAndSettle(tester, _edit);
      await tapAndSettle(tester, _cancel);

      expect(_shown(tester), isNull);
      expect(find.byType(FormErrorBanner), findsNothing);
    });

    testWidgets('a picture answered after a newer load started is dropped', (
      tester,
    ) async {
      final early = Completer<http.Response>();
      final server = _backend()
        ..always('GET', ApiPaths.profile, _profile())
        ..once('GET', ApiPaths.myAvatar, (_) => early.future);
      await _openProfile(tester, server);

      await tapAndSettle(tester, _edit);
      await tapAndSettle(tester, _cancel);
      // The reload's own answer: no picture.
      expect(_shown(tester), isNull);

      early.complete(imageResponse(testPicture));
      await tester.pumpAndSettle();
      expect(_shown(tester), isNull);
      expect(tester.takeException(), isNull);
    });

    testWidgets('a picture answered after leaving the page is dropped', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..always('GET', ApiPaths.profile, _profile())
        ..once('GET', ApiPaths.myAvatar, (_) => reply.future);
      await _openProfile(tester, server);
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(_page, findsNothing);

      reply.complete(imageResponse(testPicture));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.byType(HomeScreen), findsOneWidget);
    });

    testWidgets('a session that ends while the picture loads shows no error', (
      tester,
    ) async {
      final server = _backend()
        ..always('GET', ApiPaths.profile, _profile())
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
      final app = await pumpApp(tester, server: server, signedIn: true);
      await tester.ensureVisible(_open);
      await tester.tap(_open);
      await tester.pumpAndSettle();

      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_page, findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(tester.takeException(), isNull);
    });
  });

  group('"See public profile"', () {
    final memberPath = ApiPaths.memberProfile(testMemberId);
    final memberAvatarPath = ApiPaths.memberAvatar(testMemberId);
    final stored = languagesBody(
      spoken: [('es', 'native'), ('en', 'c1')],
      learning: [('ja', 'a2')],
    );

    /// A member with a name, a text, a picture and languages, read through
    /// their own routes by the page and through the member routes by the
    /// public profile.
    FakeServer complete() => _backend()
      ..always('GET', ApiPaths.profile, _profile(name: 'Ana López', bio: 'Hi'))
      ..always('GET', ApiPaths.myLanguages, (_) => jsonResponse(200, stored))
      ..always('GET', ApiPaths.myAvatar, _picture)
      ..always(
        'GET',
        memberPath,
        (_) => jsonResponse(
          200,
          memberProfileBody(
            displayName: 'Ana López',
            bio: 'Hi',
            hasAvatar: true,
            languages: stored,
          ),
        ),
      )
      ..always('GET', memberAvatarPath, _picture);

    testWidgets('is an action of the app bar, labelled for screen readers', (
      tester,
    ) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      final handle = tester.ensureSemantics();
      await _openProfile(tester, server);

      expect(_seePublic, findsOneWidget);
      expect(
        find.descendant(of: find.byType(AppBar), matching: _seePublic),
        findsOneWidget,
      );
      expect(
        tester.getSemantics(find.byIcon(Icons.visibility_outlined)),
        isSemantics(
          isButton: true,
          hasTapAction: true,
          tooltip: l10n.profileSeePublicButton,
        ),
      );
      // Showing the action asks nothing of the member routes.
      expect(
        server.requests.where((r) => r.url.path.startsWith('/v1/profiles')),
        isEmpty,
      );
      handle.dispose();
    });

    testWidgets('is absent without a profile', (tester) async {
      final server = _backend()
        ..always('GET', ApiPaths.profile, (_) => noProfile());
      await _openProfile(tester, server);

      expect(find.text(l10n.profileEmptyMessage), findsOneWidget);
      expect(_seePublic, findsNothing);
    });

    testWidgets('is absent while the profile loads, when the load failed and '
        'while it is retried', (tester) async {
      final first = Completer<http.Response>();
      final second = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.profile, (_) => first.future)
        ..once('GET', ApiPaths.profile, (_) => second.future);
      await _openProfile(tester, server, settle: false);
      expect(_seePublic, findsNothing);

      first.complete(errorResponse(500, 'internal_error'));
      await tester.pumpAndSettle();
      expect(_retry, findsOneWidget);
      expect(_seePublic, findsNothing);

      await tester.tap(_retry);
      await tester.pump();
      expect(_seePublic, findsNothing);

      second.complete(jsonResponse(200, profileBody()));
      await tester.pumpAndSettle();
      expect(_seePublic, findsOneWidget);
    });

    testWidgets('opens /members/<own id>, with the same name, text, picture '
        'and languages and no way to edit', (tester) async {
      final server = complete();
      final app = await _openProfile(tester, server);
      final pageChips = _chips(tester);
      expect(pageChips, hasLength(3));
      expect(_shown(tester), testPicture);

      await tester.tap(_seePublic);
      await tester.pumpAndSettle();

      expect(_member, findsOneWidget);
      expect(app.location(tester), '/members/$testMemberId');
      final header = tester.widget<ProfileHeader>(find.byType(ProfileHeader));
      expect(header.name, 'Ana López');
      expect(header.bio, 'Hi');
      expect(header.image, testPicture);
      expect(_chips(tester), pageChips);
      expect(_edit, findsNothing);
      expect(_seePublic, findsNothing);
      expect(_textFields, findsNothing);
      expect(find.text(l10n.profileFriendsHeading), findsNothing);
      // The public view is read through the member routes, by the id the
      // member's own profile carries.
      expect(server.to(memberPath).single.method, 'GET');
      expect(server.to(memberAvatarPath).single.method, 'GET');
    });

    testWidgets('going back shows the page as it was, without a reload', (
      tester,
    ) async {
      final server = complete();
      final app = await _openProfile(tester, server);
      await tester.tap(_seePublic);
      await tester.pumpAndSettle();
      expect(_member, findsOneWidget);

      // What is stored changes meanwhile: the page doesn't ask again.
      server
        ..always('GET', ApiPaths.profile, _profile(name: 'Changed'))
        ..always('GET', ApiPaths.myAvatar, (_) => noAvatar());
      await tester.pageBack();
      await tester.pump();
      // No spinner on the way back.
      expect(find.bySemanticsLabel(l10n.profileLoading), findsNothing);
      await tester.pumpAndSettle();

      expect(_member, findsNothing);
      expect(_page, findsOneWidget);
      expect(app.location(tester), '/profile');
      expect(find.text('Ana López'), findsOneWidget);
      expect(_shown(tester), testPicture);
      expect(_chips(tester), hasLength(3));
      expect(_seePublic, findsOneWidget);
      // The page's read, and the member screen's (to know whose profile it
      // showed): none on the way back.
      expect(server.count(ApiPaths.profile), 2);
      expect(server.count(ApiPaths.myAvatar), 1);
      expect(server.count(ApiPaths.myLanguages), 1);
      expect(server.count(ApiPaths.languages), 2);
    });

    testWidgets('a rapid double tap opens the public profile once, and it '
        'can be opened again after coming back', (tester) async {
      final server = complete();
      await _openProfile(tester, server);

      await tester.tap(_seePublic);
      await tester.tap(_seePublic, warnIfMissed: false);
      await tester.pumpAndSettle();
      expect(_member, findsOneWidget);
      expect(server.count(memberPath), 1);

      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(_page, findsOneWidget);
      expect(_member, findsNothing);

      await tester.tap(_seePublic);
      await tester.pumpAndSettle();
      expect(_member, findsOneWidget);
      expect(server.count(memberPath), 2);
    });
  });

  group('navigation', () {
    testWidgets('back returns to home', (tester) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      final app = await _openProfile(tester, server);
      await tester.pageBack();
      await tester.pumpAndSettle();

      expect(find.byType(HomeScreen), findsOneWidget);
      expect(_page, findsNothing);
      expect(app.location(tester), '/home');
    });

    testWidgets('logging out elsewhere leaves the profile for log in', (
      tester,
    ) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      final app = await _openProfile(tester, server);
      await app.session.logout();
      await tester.pumpAndSettle();

      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_page, findsNothing);
      expect(app.location(tester), '/login');
    });

    testWidgets('opening the profile again loads it again', (tester) async {
      final server = _backend()..always('GET', ApiPaths.profile, _profile());
      await _openProfile(tester, server);
      await tester.pageBack();
      await tester.pumpAndSettle();

      server.always('GET', ApiPaths.profile, _profile(name: 'Ana María'));
      await tapAndSettle(tester, _open);
      expect(find.text('Ana María'), findsOneWidget);
      expect(server.count(ApiPaths.profile), 2);
    });
  });

  testWidgets('home does not load the profile by itself', (tester) async {
    final server = _backend();
    await pumpApp(tester, server: server, signedIn: true);
    expect(_open, findsOneWidget);
    expect(server.count(ApiPaths.profile), 0);
  });
}

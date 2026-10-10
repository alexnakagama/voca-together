import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/router.dart';
import 'package:vocatogether/screens/home_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/member_profile_screen.dart';
import 'package:vocatogether/session.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/language_chip.dart';
import 'package:vocatogether/ui/widgets/profile_avatar.dart';
import 'package:vocatogether/ui/widgets/profile_header.dart';

import '../support/fakes.dart';
import '../support/pictures.dart';
import 'harness.dart';

/// The public identifier of another member's profile.
const _id = '7c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f';

final _profilePath = ApiPaths.memberProfile(_id);
final _avatarPath = ApiPaths.memberAvatar(_id);
final _blockPath = ApiPaths.myBlock(_id);

Finder get _screen => find.byType(MemberProfileScreen);
Finder get _retry => find.widgetWithText(FilledButton, l10n.tryAgain);

/// Anything that takes text.
Finder get _textFields =>
    find.byWidgetPredicate((w) => w is TextField || w is EditableText);

/// Every control under the app bar.
Finder get _controls => find.descendant(
  of: find.descendant(
    of: _screen,
    matching: find.byType(SingleChildScrollView),
  ),
  matching: find.byWidgetPredicate(
    (w) => w is ButtonStyleButton || w is ListTile || w is Checkbox,
  ),
);

/// The app bar's actions; the way back is not one of them.
List<Widget> _actions(WidgetTester tester) =>
    tester
        .widget<AppBar>(
          find.descendant(of: _screen, matching: find.byType(AppBar)),
        )
        .actions ??
    const [];

/// The app bar's menu button, and what it holds once open.
Finder get _menu => find.byTooltip(l10n.memberMenuTooltip);
Finder get _menuItems => find.byWidgetPredicate((w) => w is PopupMenuItem);
Finder get _confirmBlock =>
    find.widgetWithText(TextButton, l10n.memberBlockConfirm);
Finder get _cancelBlock =>
    find.widgetWithText(TextButton, l10n.memberBlockCancel);

/// The blocked state's "Unblock", and the answers of its confirmation.
Finder get _unblock => find.widgetWithText(OutlinedButton, l10n.unblockButton);
Finder get _confirmUnblock =>
    find.widgetWithText(TextButton, l10n.unblockButton);
Finder get _cancelUnblock =>
    find.widgetWithText(TextButton, l10n.unblockCancel);

/// Whether the blocked state's "Unblock" takes a tap.
bool _unblockEnabled(WidgetTester tester) =>
    tester.widget<OutlinedButton>(_unblock).enabled;

/// Whether the menu button takes a tap.
bool _menuEnabled(WidgetTester tester) => tester
    .widget<PopupMenuButton<Object?>>(
      find.byWidgetPredicate((w) => w is PopupMenuButton),
    )
    .enabled;

/// Opens the menu and chooses "Block": the confirmation is then open.
Future<void> _chooseBlock(WidgetTester tester) async {
  await tester.tap(_menu);
  await tester.pumpAndSettle();
  await tester.tap(find.text(l10n.memberMenuBlock));
  await tester.pumpAndSettle();
  expect(find.text(l10n.memberBlockTitle), findsOneWidget);
}

/// A backend for a signed-in user on home, who has a profile of their own,
/// with the three-language catalog; the member routes are scripted by each
/// test.
FakeServer _backend() => FakeServer()
  ..always('GET', ApiPaths.profile, (_) => jsonResponse(200, profileBody()))
  ..always('GET', ApiPaths.languages, (_) => jsonResponse(200, catalogBody()))
  ..always('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()))
  ..always('GET', ApiPaths.healthz, (_) => healthy())
  ..always('POST', ApiPaths.logout, (_) => noContent());

Responder _member({
  String name = 'Bea',
  String bio = '',
  bool hasAvatar = false,
  Map<String, Object?>? languages,
}) =>
    (_) => jsonResponse(
      200,
      memberProfileBody(
        id: _id,
        displayName: name,
        bio: bio,
        hasAvatar: hasAvatar,
        languages: languages,
      ),
    );

/// Navigates as a screen would: pushes [location] on top of where the app
/// is. The redirect decides where it ends up.
Future<void> _push(
  WidgetTester tester,
  String location, {
  bool settle = true,
}) async {
  unawaited(
    GoRouter.of(tester.element(find.byType(Navigator).first))
        .push<void>(location),
  );
  if (settle) {
    await tester.pumpAndSettle();
  } else {
    await tester.pump();
    await tester.pump();
  }
}

/// Signs in on home and opens the profile of the member [id], whose load
/// must be scripted.
Future<TestApp> _openMember(
  WidgetTester tester,
  FakeServer server, {
  String id = _id,
  bool settle = true,
}) async {
  final app = await pumpApp(tester, server: server, signedIn: true);
  await _push(tester, Routes.member(id), settle: settle);
  expect(_screen, findsOneWidget);
  return app;
}

/// The picture the header shows, or null for the placeholder.
Uint8List? _shown(WidgetTester tester) =>
    tester.widget<ProfileHeader>(find.byType(ProfileHeader)).image;

/// The chips on screen as (name, level), in order.
List<(String, String)> _chips(WidgetTester tester) => [
  for (final chip in tester.widgetList<LanguageChip>(find.byType(LanguageChip)))
    (chip.name, chip.level),
];

/// The requests to a route that names a member.
Iterable<http.Request> _memberRequests(FakeServer server) =>
    server.requests.where((r) => r.url.path.startsWith('/v1/profiles'));

void main() {
  group('the member profile screen', () {
    testWidgets('a member with a picture, a text and languages: all of them '
        'are shown, read from the member routes only', (tester) async {
      final server = _backend()
        ..always(
          'GET',
          _profilePath,
          _member(
            name: 'Bea Ito',
            bio: 'Evenings, mostly.',
            hasAvatar: true,
            languages: languagesBody(
              spoken: [('ja', 'native'), ('en', 'c1')],
              learning: [('es', 'a2')],
            ),
          ),
        )
        ..always('GET', _avatarPath, (_) => imageResponse(testPicture));
      final handle = tester.ensureSemantics();
      final app = await _openMember(tester, server);

      expect(app.location(tester), '/members/$_id');
      expect(find.text(l10n.memberProfileTitle), findsOneWidget);
      expect(find.text('Bea Ito'), findsOneWidget);
      expect(find.text('Evenings, mostly.'), findsOneWidget);
      expect(
        tester.getSemantics(find.text('Bea Ito')),
        isSemantics(label: 'Bea Ito', isHeader: true),
      );
      expect(_shown(tester), testPicture);
      expect(
        tester.getSemantics(find.byType(ProfileAvatar)),
        isSemantics(isImage: true, label: 'Bea Ito’s profile picture'),
      );
      // Both lists under their headings, in the member's order.
      expect(
        tester.getSemantics(find.text(l10n.languagesHeading)),
        isSemantics(label: l10n.languagesHeading, isHeader: true),
      );
      expect(find.text(l10n.languagesSpokenHeading), findsOneWidget);
      expect(find.text(l10n.languagesLearningHeading), findsOneWidget);
      expect(_chips(tester), [
        ('Japanese', 'Native'),
        ('English', 'C1'),
        ('Spanish', 'A2'),
      ]);

      // One read of each, with nothing but the id in the path. Of the
      // reader's own data only the profile is read, once, to know whose
      // profile this is: never their languages or picture.
      expect(server.to(_profilePath).single.method, 'GET');
      expect(server.to(_profilePath).single.url.hasQuery, isFalse);
      expect(server.to(_avatarPath).single.method, 'GET');
      expect(server.to(_avatarPath).single.url.hasQuery, isFalse);
      expect(server.count(ApiPaths.languages), 1);
      expect(server.to(ApiPaths.profile).single.method, 'GET');
      expect(server.count(ApiPaths.myLanguages), 0);
      expect(server.count(ApiPaths.myAvatar), 0);
      handle.dispose();
    });

    testWidgets('the languages are shown in the order the member gave them', (
      tester,
    ) async {
      final server = _backend()
        ..always(
          'GET',
          _profilePath,
          _member(
            languages: languagesBody(
              spoken: [('es', 'b2'), ('en', 'native'), ('ja', 'a1')],
            ),
          ),
        );
      await _openMember(tester, server);

      expect(_chips(tester), [
        ('Spanish', 'B2'),
        ('English', 'Native'),
        ('Japanese', 'A1'),
      ]);
      expect(find.text(l10n.languagesSpokenHeading), findsOneWidget);
      expect(find.text(l10n.languagesLearningHeading), findsNothing);
    });

    testWidgets('a member without a picture: the placeholder built from the '
        'name, and no picture request', (tester) async {
      final server = _backend()
        ..always('GET', _profilePath, _member(name: 'Bea', bio: 'Hi'));
      final handle = tester.ensureSemantics();
      await _openMember(tester, server);

      expect(_shown(tester), isNull);
      expect(
        find.descendant(
          of: find.byType(ProfileAvatar),
          matching: find.text('B'),
        ),
        findsOneWidget,
      );
      expect(
        tester.getSemantics(find.byType(ProfileAvatar)),
        isSemantics(isImage: true, label: 'Bea’s profile picture: no photo'),
      );
      expect(find.text('Hi'), findsOneWidget);
      expect(server.count(_avatarPath), 0);
      handle.dispose();
    });

    group('a picture that cannot be loaded is the placeholder, with no '
        'message, and the rest is shown', () {
      final cases = <String, Responder>{
        '500': (_) => errorResponse(500, 'internal_error'),
        'network': networkFailure,
        '429': (_) =>
            errorResponse(429, 'rate_limited', headers: {'retry-after': '10'}),
        // Removed between the two reads.
        'no picture any more': (_) => noAvatar(),
        'no profile any more': (_) => noProfile(),
        'a 404 without a code': (_) => http.Response('Not Found', 404),
      };
      cases.forEach((name, responder) {
        testWidgets(name, (tester) async {
          final server = _backend()
            ..always(
              'GET',
              _profilePath,
              _member(
                bio: 'Hi',
                hasAvatar: true,
                languages: languagesBody(spoken: [('ja', 'native')]),
              ),
            )
            ..once('GET', _avatarPath, responder);
          final app = await _openMember(tester, server);

          expect(_shown(tester), isNull);
          expect(
            find.descendant(
              of: find.byType(ProfileAvatar),
              matching: find.text('B'),
            ),
            findsOneWidget,
          );
          expect(find.text('Bea'), findsOneWidget);
          expect(find.text('Hi'), findsOneWidget);
          expect(_chips(tester), [('Japanese', 'Native')]);
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(_retry, findsNothing);
          // Asked once: nothing retries it.
          expect(server.count(_avatarPath), 1);
          expect(app.session.status, SessionStatus.signedIn);
        });
      });

      testWidgets('no answer in time', (tester) async {
        final server = _backend()
          ..always('GET', _profilePath, _member(hasAvatar: true))
          ..once('GET', _avatarPath, neverAnswers);
        await _openMember(tester, server, settle: false);
        await tester.pump();
        // The profile is there while the picture is still being asked for.
        expect(find.text('Bea'), findsOneWidget);
        expect(_shown(tester), isNull);

        await tester.pump(const Duration(seconds: 16));
        await tester.pumpAndSettle();
        expect(find.text('Bea'), findsOneWidget);
        expect(_shown(tester), isNull);
        expect(find.byType(FormErrorBanner), findsNothing);
      });
    });

    testWidgets('a member with no language: one text, and neither heading', (
      tester,
    ) async {
      final server = _backend()..always('GET', _profilePath, _member());
      await _openMember(tester, server);

      expect(find.text(l10n.memberLanguagesEmpty), findsOneWidget);
      // Not the text of the reader's own page, which speaks to "you".
      expect(find.text(l10n.languagesEmpty), findsNothing);
      expect(find.text(l10n.languagesSpokenHeading), findsNothing);
      expect(find.text(l10n.languagesLearningHeading), findsNothing);
      expect(find.byType(LanguageChip), findsNothing);
    });

    testWidgets('a language the catalog does not name is shown by its code', (
      tester,
    ) async {
      final server = _backend()
        ..always(
          'GET',
          _profilePath,
          _member(
            languages: languagesBody(
              spoken: [('es', 'native')],
              learning: [('xx', 'a2')],
            ),
          ),
        );
      await _openMember(tester, server);

      expect(_chips(tester), [('Spanish', 'Native'), ('xx', 'A2')]);
    });

    testWidgets('with no text, the name and nothing in the text\'s place', (
      tester,
    ) async {
      final server = _backend()..always('GET', _profilePath, _member());
      await _openMember(tester, server);

      expect(
        tester
            .widgetList<Text>(
              find.descendant(
                of: find.byType(ProfileHeader),
                matching: find.byType(Text),
              ),
            )
            .map((t) => t.data),
        ['B', 'Bea'],
      );
    });

    for (final (who, id) in [
      ('another member', _id),
      ('the member using the app', testMemberId),
    ]) {
      testWidgets('nothing to edit and no Friends area for $who', (
        tester,
      ) async {
        final server = _backend()
          ..always(
            'GET',
            ApiPaths.memberProfile(id),
            (_) => jsonResponse(
              200,
              memberProfileBody(
                id: id,
                bio: 'Hi',
                languages: languagesBody(spoken: [('es', 'native')]),
              ),
            ),
          );
        final app = await _openMember(tester, server, id: id);

        expect(app.location(tester), '/members/$id');
        expect(find.text('Ana'), findsOneWidget);
        expect(find.text(l10n.profileEditButton), findsNothing);
        expect(find.text(l10n.profileEmptyButton), findsNothing);
        expect(find.byTooltip(l10n.profileSeePublicButton), findsNothing);
        expect(find.text(l10n.profileFriendsHeading), findsNothing);
        expect(find.text(l10n.profileFriendsComingLater), findsNothing);
        expect(_textFields, findsNothing);
        // Under the app bar there is no control, and in it only the way
        // back and, on another member's profile, the menu.
        expect(_controls, findsNothing);
        expect(_actions(tester), hasLength(id == testMemberId ? 0 : 1));
        expect(find.byType(BackButton), findsOneWidget);
        // Nothing was written.
        expect(server.requests.map((r) => r.method).toSet(), {'GET'});
      });
    }
  });

  group('loading and failure', () {
    testWidgets('a labelled spinner until the profile answers', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()..once('GET', _profilePath, (_) => reply.future);
      final handle = tester.ensureSemantics();
      final app = await _openMember(tester, server, settle: false);

      expect(app.location(tester), '/members/$_id');
      expect(find.bySemanticsLabel(l10n.memberProfileLoading), findsOneWidget);
      expect(find.byType(ProfileHeader), findsNothing);
      expect(find.text(l10n.languagesHeading), findsNothing);
      expect(find.text(l10n.memberProfileUnavailable), findsNothing);
      expect(_controls, findsNothing);

      reply.complete(await _member()(http.Request('GET', Uri())));
      await tester.pumpAndSettle();
      expect(find.bySemanticsLabel(l10n.memberProfileLoading), findsNothing);
      expect(find.text('Bea'), findsOneWidget);
      handle.dispose();
    });

    testWidgets('nothing of the profile is shown while the catalog is still '
        'loading', (tester) async {
      final catalog = Completer<http.Response>();
      final server = _backend()
        ..always('GET', _profilePath, _member(bio: 'Hi'))
        ..once('GET', ApiPaths.languages, (_) => catalog.future);
      await _openMember(tester, server, settle: false);
      await tester.pump();

      expect(find.byType(CircularProgressIndicator), findsOneWidget);
      expect(find.byType(ProfileHeader), findsNothing);
      expect(find.text('Bea'), findsNothing);

      catalog.complete(jsonResponse(200, catalogBody()));
      await tester.pumpAndSettle();
      expect(find.text('Bea'), findsOneWidget);
    });

    group('a profile that does not exist is unavailable, with no retry', () {
      for (final hasCatalog in [true, false]) {
        testWidgets(
          hasCatalog
              ? '404 profile_not_found'
              : 'also for the '
                    'member\'s own id',
          (tester) async {
            final id = hasCatalog ? _id : testMemberId;
            final path = ApiPaths.memberProfile(id);
            final server = _backend()..always('GET', path, (_) => noProfile());
            final app = await _openMember(tester, server, id: id);

            expect(find.text(l10n.memberProfileUnavailable), findsOneWidget);
            expect(_retry, findsNothing);
            expect(find.text(l10n.tryAgain), findsNothing);
            expect(_controls, findsNothing);
            expect(find.byType(FormErrorBanner), findsNothing);
            expect(find.byType(ProfileHeader), findsNothing);
            expect(find.byType(ProfileAvatar), findsNothing);
            expect(find.text(l10n.languagesHeading), findsNothing);
            expect(find.text(l10n.memberLanguagesEmpty), findsNothing);
            // Asked once, and no picture asked for.
            expect(server.count(path), 1);
            expect(server.count(ApiPaths.memberAvatar(id)), 0);
            expect(app.session.status, SessionStatus.signedIn);
            expect(app.location(tester), '/members/$id');

            // The way back still works.
            await tester.pageBack();
            await tester.pumpAndSettle();
            expect(find.byType(HomeScreen), findsOneWidget);
          },
        );
      }
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
          (_) => jsonResponse(200, {'id': _id, 'display_name': 'Bea'}),
          l10n.errorUnexpected,
        ),
        'a level this app does not know': (
          _member(languages: languagesBody(spoken: [('es', 'c3')])),
          l10n.errorUnexpected,
        ),
        // Not the backend's "no such profile": never shown as unavailable.
        'a 404 without the code': (
          (_) => http.Response('Not Found', 404),
          l10n.errorUnexpected,
        ),
        'a 404 with another code': (
          (_) => errorResponse(404, 'avatar_not_found'),
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
            ..once('GET', _profilePath, responder)
            ..once('GET', _profilePath, (_) => reply.future);
          final handle = tester.ensureSemantics();
          final app = await _openMember(tester, server);

          expect(find.text(message), findsOneWidget);
          expect(find.textContaining('SERVERTEXT'), findsNothing);
          expect(find.byType(ProfileHeader), findsNothing);
          expect(find.text(l10n.languagesHeading), findsNothing);
          expect(find.text(l10n.memberProfileUnavailable), findsNothing);
          expect(server.count(_avatarPath), 0);
          expect(app.session.status, SessionStatus.signedIn);
          expect(app.location(tester), '/members/$_id');

          // The retry shows the spinner and asks again.
          await tester.tap(_retry);
          await tester.pump();
          expect(
            find.bySemanticsLabel(l10n.memberProfileLoading),
            findsOneWidget,
          );
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(_retry, findsNothing);

          reply.complete(await _member()(http.Request('GET', Uri())));
          await tester.pumpAndSettle();
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(find.text('Bea'), findsOneWidget);
          expect(server.count(_profilePath), 2);
          handle.dispose();
        });
      });
    });

    testWidgets('the load fails whole: with the catalog failing, nothing of '
        'the profile is shown, and the retry loads both again', (tester) async {
      final server = _backend()
        ..always(
          'GET',
          _profilePath,
          _member(
            hasAvatar: true,
            languages: languagesBody(spoken: [('es', 'native')]),
          ),
        )
        ..always('GET', _avatarPath, (_) => imageResponse(testPicture))
        ..once('GET', ApiPaths.languages, networkFailure);
      await _openMember(tester, server);

      expect(find.text(l10n.errorNetwork), findsOneWidget);
      expect(find.byType(ProfileHeader), findsNothing);
      expect(find.byType(LanguageChip), findsNothing);
      expect(server.count(_avatarPath), 0);

      await tester.tap(_retry);
      await tester.pumpAndSettle();
      expect(find.text('Bea'), findsOneWidget);
      expect(_chips(tester), [('Spanish', 'Native')]);
      expect(_shown(tester), testPicture);
      expect(server.count(_profilePath), 2);
      expect(server.count(ApiPaths.languages), 2);
    });

    testWidgets('a load that gets no answer times out', (tester) async {
      final server = _backend()..once('GET', _profilePath, neverAnswers);
      await _openMember(tester, server, settle: false);

      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(_retry, findsOneWidget);
    });

    for (final (what, path, script) in [
      ('loading', _profilePath, (FakeServer _) {}),
      (
        'the picture loads',
        _avatarPath,
        (FakeServer s) =>
            s.always('GET', _profilePath, _member(hasAvatar: true)),
      ),
    ]) {
      testWidgets('a session that ended while $what shows no error', (
        tester,
      ) async {
        final server = _backend();
        script(server);
        server
          ..once('GET', path, (_) => errorResponse(401, 'invalid_access_token'))
          ..once(
            'POST',
            ApiPaths.refresh,
            (_) => errorResponse(401, 'invalid_refresh_token'),
          );
        final app = await pumpApp(tester, server: server, signedIn: true);
        await _push(tester, Routes.member(_id));

        expect(app.session.status, SessionStatus.signedOut);
        expect(find.byType(LoginScreen), findsOneWidget);
        expect(_screen, findsNothing);
        expect(find.byType(FormErrorBanner), findsNothing);
        expect(app.location(tester), '/login');
        expect(tester.takeException(), isNull);
      });
    }

    testWidgets('a load answered after leaving is dropped', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()..once('GET', _profilePath, (_) => reply.future);
      await _openMember(tester, server, settle: false);

      await tester.pageBack();
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      reply.complete(errorResponse(500, 'internal_error'));
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(tester.takeException(), isNull);
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(server.count(_avatarPath), 0);
    });

    testWidgets('a picture answered after leaving is dropped', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..always('GET', _profilePath, _member(hasAvatar: true))
        ..once('GET', _avatarPath, (_) => reply.future);
      await _openMember(tester, server, settle: false);
      await tester.pump();
      expect(find.text('Bea'), findsOneWidget);

      await tester.pageBack();
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      reply.complete(imageResponse(testPicture));
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(tester.takeException(), isNull);
      expect(find.byType(HomeScreen), findsOneWidget);
    });
  });

  group('the menu', () {
    testWidgets('another member\'s profile: "Report" and "Block", as two '
        'separate items', (tester) async {
      final server = _backend()..always('GET', _profilePath, _member());
      final handle = tester.ensureSemantics();
      await _openMember(tester, server);

      expect(_actions(tester), hasLength(1));
      expect(_menu, findsOneWidget);
      expect(_menuEnabled(tester), isTrue);
      // Closed: neither item is on screen yet.
      expect(_menuItems, findsNothing);

      await tester.tap(_menu);
      await tester.pumpAndSettle();
      expect(_menuItems, findsNWidgets(2));
      expect(
        tester
            .widgetList<Text>(
              find.descendant(of: _menuItems, matching: find.byType(Text)),
            )
            .map((t) => t.data),
        [l10n.memberMenuReport, l10n.memberMenuBlock],
      );
      // Opening the menu sends nothing.
      expect(server.requests.map((r) => r.method).toSet(), {'GET'});
      handle.dispose();
    });

    testWidgets('one\'s own profile: no menu', (tester) async {
      final server = _backend()
        ..always(
          'GET',
          ApiPaths.memberProfile(testMemberId),
          (_) => jsonResponse(200, memberProfileBody(bio: 'Hi')),
        );
      await _openMember(tester, server, id: testMemberId);

      expect(find.text('Ana'), findsOneWidget);
      expect(_actions(tester), isEmpty);
      expect(_menu, findsNothing);
      expect(find.text(l10n.memberMenuBlock), findsNothing);
      expect(find.text(l10n.memberMenuReport), findsNothing);
    });

    testWidgets('a caller with no profile is never the owner: the menu is '
        'shown on another member\'s profile', (tester) async {
      final server = _backend()
        ..always('GET', ApiPaths.profile, (_) => noProfile())
        ..always('GET', _profilePath, _member());
      await _openMember(tester, server);

      expect(find.text('Bea'), findsOneWidget);
      expect(_menu, findsOneWidget);
    });

    testWidgets('no menu while loading', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()..once('GET', _profilePath, (_) => reply.future);
      await _openMember(tester, server, settle: false);

      expect(find.byType(CircularProgressIndicator), findsOneWidget);
      expect(_actions(tester), isEmpty);

      reply.complete(await _member()(http.Request('GET', Uri())));
      await tester.pumpAndSettle();
      expect(_menu, findsOneWidget);
    });

    testWidgets('no menu while the caller\'s own profile is still loading', (
      tester,
    ) async {
      final own = Completer<http.Response>();
      final server = _backend()
        ..always('GET', _profilePath, _member())
        ..once('GET', ApiPaths.profile, (_) => own.future);
      await _openMember(tester, server, settle: false);
      await tester.pump();

      expect(find.byType(CircularProgressIndicator), findsOneWidget);
      expect(find.byType(ProfileHeader), findsNothing);
      expect(_actions(tester), isEmpty);

      own.complete(jsonResponse(200, profileBody()));
      await tester.pumpAndSettle();
      expect(find.text('Bea'), findsOneWidget);
      expect(_menu, findsOneWidget);
    });

    testWidgets('no menu on a failed load', (tester) async {
      final server = _backend()..once('GET', _profilePath, networkFailure);
      await _openMember(tester, server);

      expect(find.text(l10n.errorNetwork), findsOneWidget);
      expect(_actions(tester), isEmpty);
    });

    testWidgets('no menu for an unavailable profile', (tester) async {
      final server = _backend()
        ..always('GET', _profilePath, (_) => noProfile());
      await _openMember(tester, server);

      expect(find.text(l10n.memberProfileUnavailable), findsOneWidget);
      expect(_actions(tester), isEmpty);
    });

    group('a failed request for the caller\'s own profile fails the load '
        'whole, with "Try again"', () {
      final cases = <String, (Responder, String)>{
        'network': (networkFailure, l10n.errorNetwork),
        '500': (
          (_) => errorResponse(500, 'internal_error'),
          l10n.errorUnexpected,
        ),
        // Not "no profile yet": never read as "not the owner".
        'a 404 without the code': (
          (_) => http.Response('Not Found', 404),
          l10n.errorUnexpected,
        ),
      };
      cases.forEach((name, c) {
        final (responder, message) = c;
        testWidgets(name, (tester) async {
          final server = _backend()
            ..always('GET', _profilePath, _member(hasAvatar: true))
            ..always('GET', _avatarPath, (_) => imageResponse(testPicture))
            ..once('GET', ApiPaths.profile, responder);
          await _openMember(tester, server);

          expect(find.text(message), findsOneWidget);
          expect(_retry, findsOneWidget);
          // Neither the profile nor a menu that might be the wrong one.
          expect(find.byType(ProfileHeader), findsNothing);
          expect(find.text('Bea'), findsNothing);
          expect(_actions(tester), isEmpty);
          expect(server.count(_avatarPath), 0);

          await tester.tap(_retry);
          await tester.pumpAndSettle();
          expect(find.text('Bea'), findsOneWidget);
          expect(_menu, findsOneWidget);
          expect(server.count(ApiPaths.profile), 2);
          expect(server.count(_profilePath), 2);
        });
      });
    });
  });

  group('blocking', () {
    /// Another member's full profile, with a picture.
    FakeServer shown() => _backend()
      ..always(
        'GET',
        _profilePath,
        _member(
          name: 'Bea Ito',
          bio: 'Evenings, mostly.',
          hasAvatar: true,
          languages: languagesBody(spoken: [('ja', 'native')]),
        ),
      )
      ..always('GET', _avatarPath, (_) => imageResponse(testPicture));

    void expectProfile(WidgetTester tester) {
      expect(find.text('Bea Ito'), findsOneWidget);
      expect(find.text('Evenings, mostly.'), findsOneWidget);
      expect(_shown(tester), testPicture);
      expect(_chips(tester), [('Japanese', 'Native')]);
      expect(find.text(l10n.memberBlocked), findsNothing);
    }

    testWidgets('"Block" asks first, saying that neither sees the other and '
        'that the member is not told; dismissing it sends nothing', (
      tester,
    ) async {
      final server = shown();
      await _openMember(tester, server);

      await _chooseBlock(tester);
      expect(find.text(l10n.memberBlockMessage), findsOneWidget);
      expect(l10n.memberBlockMessage, contains('each other’s profiles'));
      expect(l10n.memberBlockMessage, contains('won’t be told'));
      // Nothing is sent before the answer.
      expect(server.count(_blockPath), 0);

      await tester.tap(_cancelBlock);
      await tester.pumpAndSettle();
      expect(find.text(l10n.memberBlockTitle), findsNothing);
      expectProfile(tester);

      // Dismissed by a tap outside it, and by back.
      await _chooseBlock(tester);
      await tester.tapAt(const Offset(5, 5));
      await tester.pumpAndSettle();
      expect(find.text(l10n.memberBlockTitle), findsNothing);
      await _chooseBlock(tester);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.text(l10n.memberBlockTitle), findsNothing);

      expectProfile(tester);
      expect(_screen, findsOneWidget);
      expect(_menuEnabled(tester), isTrue);
      expect(server.count(_blockPath), 0);
      expect(server.requests.map((r) => r.method).toSet(), {'GET'});
    });

    testWidgets('confirming sends one block for that profile, and after the '
        '204 nothing of the profile is shown', (tester) async {
      final server = shown()..once('PUT', _blockPath, (_) => noContent());
      final app = await _openMember(tester, server);
      expectProfile(tester);

      await _chooseBlock(tester);
      await tester.tap(_confirmBlock);
      await tester.pumpAndSettle();

      final request = server.to(_blockPath).single;
      expect(request.method, 'PUT');
      expect(request.url.path, '/v1/me/blocks/$_id');
      expect(request.url.hasQuery, isFalse);
      expect(request.bodyBytes, isEmpty);

      // Blocked, with "Unblock" as the way to undo it: the text promises
      // nothing about the list.
      expect(find.text(l10n.memberBlocked), findsOneWidget);
      expect(l10n.memberBlocked, contains('blocked this member'));
      expect(l10n.memberBlocked, contains('“Unblock” undoes it'));
      expect(l10n.memberBlocked, isNot(contains('Blocked members')));
      expect(_unblock, findsOneWidget);
      expect(_unblockEnabled(tester), isTrue);
      expect(find.text('Bea Ito'), findsNothing);
      expect(find.text('Evenings, mostly.'), findsNothing);
      expect(find.byType(ProfileHeader), findsNothing);
      expect(find.byType(ProfileAvatar), findsNothing);
      expect(find.byType(LanguageChip), findsNothing);
      expect(find.text(l10n.languagesHeading), findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(find.byType(LinearProgressIndicator), findsNothing);
      // Nothing left to do here but undo it or leave.
      expect(_actions(tester), isEmpty);
      expect(_controls, findsOneWidget);
      expect(app.location(tester), '/members/$_id');
      // Nothing was read again, and nothing else written.
      expect(server.count(_profilePath), 1);
      expect(server.requests.where((r) => r.method != 'GET'), hasLength(1));

      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(find.byType(HomeScreen), findsOneWidget);
    });

    testWidgets('a picture answered after the block is not shown', (
      tester,
    ) async {
      final picture = Completer<http.Response>();
      final server = shown()
        ..once('GET', _avatarPath, (_) => picture.future)
        ..once('PUT', _blockPath, (_) => noContent());
      await _openMember(tester, server, settle: false);
      await tester.pump();

      await _chooseBlock(tester);
      await tester.tap(_confirmBlock);
      await tester.pumpAndSettle();
      expect(find.text(l10n.memberBlocked), findsOneWidget);

      picture.complete(imageResponse(testPicture));
      await tester.pumpAndSettle();
      expect(find.byType(ProfileAvatar), findsNothing);
      expect(find.text(l10n.memberBlocked), findsOneWidget);
      expect(tester.takeException(), isNull);
    });

    group('a failed block leaves the profile, with its message, and can be '
        'tried again', () {
      final cases = <String, (Responder, String)>{
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
        '500': (
          (_) => errorResponse(500, 'internal_error'),
          l10n.errorUnexpected,
        ),
        'a 200 where a 204 is due': (
          (_) => jsonResponse(200, {'blocked': true}),
          l10n.errorUnexpected,
        ),
        // The app never sends its own id; the code has no text of its own.
        'member: self': (
          (_) => fieldError('member', 'self'),
          l10n.errorCheckInput,
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
        'too many blocked members': (
          (_) => fieldError('blocks', 'too_many'),
          l10n.errorBlocksTooMany,
        ),
      };
      cases.forEach((name, c) {
        final (responder, message) = c;
        testWidgets(name, (tester) async {
          final server = shown()
            ..once('PUT', _blockPath, responder)
            ..once('PUT', _blockPath, (_) => noContent());
          final handle = tester.ensureSemantics();
          final app = await _openMember(tester, server);

          await _chooseBlock(tester);
          await tester.tap(_confirmBlock);
          await tester.pumpAndSettle();

          expect(find.text(message), findsOneWidget);
          expect(find.textContaining('SERVERTEXT'), findsNothing);
          expect(
            tester.getSemantics(find.byType(FormErrorBanner)),
            isSemantics(
              isLiveRegion: true,
              label: '${l10n.errorLabel}\n$message',
            ),
          );
          expectProfile(tester);
          expect(_menuEnabled(tester), isTrue);
          expect(find.byType(LinearProgressIndicator), findsNothing);
          expect(server.count(_blockPath), 1);
          expect(app.session.status, SessionStatus.signedIn);
          expect(app.location(tester), '/members/$_id');

          // Again: the message goes with the new attempt.
          await _chooseBlock(tester);
          await tester.tap(_confirmBlock);
          await tester.pumpAndSettle();
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(find.text(l10n.memberBlocked), findsOneWidget);
          expect(server.count(_blockPath), 2);
          handle.dispose();
        });
      });
    });

    testWidgets('a block that hasn\'t answered: the menu is disabled, back '
        'does nothing, and it ends as a timeout', (tester) async {
      final server = shown()..once('PUT', _blockPath, neverAnswers);
      final handle = tester.ensureSemantics();
      final app = await _openMember(tester, server);

      await _chooseBlock(tester);
      await tester.tap(_confirmBlock);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));

      expect(find.text(l10n.memberBlockTitle), findsNothing);
      expect(find.bySemanticsLabel(l10n.memberBlockProgress), findsOneWidget);
      expect(_menuEnabled(tester), isFalse);
      // The profile until the answer.
      expectProfile(tester);

      // The menu doesn't open, and neither back leaves.
      await tester.tap(_menu, warnIfMissed: false);
      await tester.pump(const Duration(seconds: 1));
      expect(_menuItems, findsNothing);
      await tester.pageBack();
      await tester.pump(const Duration(seconds: 1));
      expect(_screen, findsOneWidget);
      await tester.binding.handlePopRoute();
      await tester.pump(const Duration(seconds: 1));
      expect(_screen, findsOneWidget);
      expect(app.location(tester), '/members/$_id');
      expect(server.count(_blockPath), 1);

      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(find.bySemanticsLabel(l10n.memberBlockProgress), findsNothing);
      expectProfile(tester);
      expect(_menuEnabled(tester), isTrue);
      expect(server.count(_blockPath), 1);

      // And leaving works again.
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(find.byType(HomeScreen), findsOneWidget);
      handle.dispose();
    });

    testWidgets('confirming twice sends one block', (tester) async {
      final reply = Completer<http.Response>();
      final server = shown()..once('PUT', _blockPath, (_) => reply.future);
      await _openMember(tester, server);

      await _chooseBlock(tester);
      await tester.tap(_confirmBlock);
      await tester.pump();
      // The dialog is on its way out and the menu is disabled.
      await tester.tap(_confirmBlock, warnIfMissed: false);
      await tester.tap(_menu, warnIfMissed: false);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      await tester.tap(_menu, warnIfMissed: false);
      await tester.pump(const Duration(seconds: 1));

      expect(_screen, findsOneWidget);
      expect(_menuItems, findsNothing);
      expect(find.text(l10n.memberBlockTitle), findsNothing);
      expect(server.count(_blockPath), 1);

      reply.complete(noContent());
      await tester.pumpAndSettle();
      expect(find.text(l10n.memberBlocked), findsOneWidget);
      expect(server.count(_blockPath), 1);
    });

    testWidgets('a block answered after leaving for log in is dropped', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = shown()..once('PUT', _blockPath, (_) => reply.future);
      final app = await _openMember(tester, server);

      await _chooseBlock(tester);
      await tester.tap(_confirmBlock);
      await tester.pump();
      await app.session.logout();
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);

      reply.complete(errorResponse(500, 'internal_error'));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(app.location(tester), '/login');
    });

    testWidgets('the session ending with the confirmation open shows no '
        'error, and nothing is sent', (tester) async {
      final server = shown();
      final app = await _openMember(tester, server);

      await _chooseBlock(tester);
      await app.session.logout();
      await tester.pumpAndSettle();

      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(find.text(l10n.memberBlockTitle), findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(app.location(tester), '/login');
      expect(server.count(_blockPath), 0);
      expect(tester.takeException(), isNull);
    });

    testWidgets('a block refused because the session ended shows no error', (
      tester,
    ) async {
      final server = shown()
        ..once(
          'PUT',
          _blockPath,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..once(
          'POST',
          ApiPaths.refresh,
          (_) => errorResponse(401, 'invalid_refresh_token'),
        );
      final app = await _openMember(tester, server);

      await _chooseBlock(tester);
      await tester.tap(_confirmBlock);
      await tester.pumpAndSettle();

      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(app.location(tester), '/login');
      expect(server.count(_blockPath), 1);
      expect(tester.takeException(), isNull);
    });
  });

  group('unblocking from the blocked state', () {
    final unblockPath = _blockPath;

    /// Another member's full profile, with a picture, and their block
    /// stored.
    FakeServer blockable() => _backend()
      ..always(
        'GET',
        _profilePath,
        _member(
          name: 'Bea Ito',
          bio: 'Evenings, mostly.',
          hasAvatar: true,
          languages: languagesBody(spoken: [('ja', 'native')]),
        ),
      )
      ..always('GET', _avatarPath, (_) => imageResponse(testPicture))
      ..once('PUT', _blockPath, (_) => noContent());

    /// Opens the member's profile and blocks them: the blocked state.
    Future<TestApp> block(WidgetTester tester, FakeServer server) async {
      final app = await _openMember(tester, server);
      await _chooseBlock(tester);
      await tester.tap(_confirmBlock);
      await tester.pumpAndSettle();
      expect(find.text(l10n.memberBlocked), findsOneWidget);
      return app;
    }

    /// Opens the confirmation of the unblock.
    Future<void> chooseUnblock(WidgetTester tester) async {
      await tester.tap(_unblock);
      await tester.pumpAndSettle();
      expect(find.text(l10n.unblockTitle), findsOneWidget);
    }

    void expectBlocked(WidgetTester tester) {
      expect(find.text(l10n.memberBlocked), findsOneWidget);
      expect(_unblock, findsOneWidget);
      expect(find.byType(ProfileHeader), findsNothing);
      expect(find.text('Bea Ito'), findsNothing);
    }

    testWidgets('"Unblock" asks first, naming nobody; dismissing it sends '
        'nothing', (tester) async {
      final server = blockable();
      await block(tester, server);

      await chooseUnblock(tester);
      expect(find.text(l10n.memberUnblockMessage), findsOneWidget);
      expect(find.textContaining('Bea'), findsNothing);
      // Nothing is sent before the answer.
      expect(server.requests.where((r) => r.method == 'DELETE'), isEmpty);

      await tester.tap(_cancelUnblock);
      await tester.pumpAndSettle();
      expect(find.text(l10n.unblockTitle), findsNothing);

      // Dismissed by a tap outside it, and by back.
      await chooseUnblock(tester);
      await tester.tapAt(const Offset(5, 5));
      await tester.pumpAndSettle();
      expect(find.text(l10n.unblockTitle), findsNothing);
      await chooseUnblock(tester);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.text(l10n.unblockTitle), findsNothing);

      expect(_screen, findsOneWidget);
      expectBlocked(tester);
      expect(_unblockEnabled(tester), isTrue);
      expect(server.requests.where((r) => r.method == 'DELETE'), isEmpty);
      expect(server.count(_profilePath), 1);
    });

    testWidgets('confirming sends one unblock for the route\'s identifier, '
        'never reads the list, and shows the profile again', (tester) async {
      final server = blockable()
        ..once('DELETE', unblockPath, (_) => noContent());
      final app = await block(tester, server);

      await chooseUnblock(tester);
      await tester.tap(_confirmUnblock);
      await tester.pumpAndSettle();

      final request = server.requests.singleWhere((r) => r.method == 'DELETE');
      expect(request.url.path, '/v1/me/blocks/$_id');
      expect(request.url.hasQuery, isFalse);
      expect(request.bodyBytes, isEmpty);
      // By the id alone: the list of blocked members is never asked for.
      expect(server.count(ApiPaths.myBlocks), 0);

      // Loaded again, and shown as before the block.
      expect(server.count(_profilePath), 2);
      expect(find.text(l10n.memberBlocked), findsNothing);
      expect(_unblock, findsNothing);
      expect(find.text('Bea Ito'), findsOneWidget);
      expect(find.text('Evenings, mostly.'), findsOneWidget);
      expect(_shown(tester), testPicture);
      expect(_chips(tester), [('Japanese', 'Native')]);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(find.byType(LinearProgressIndicator), findsNothing);
      expect(_menuEnabled(tester), isTrue);
      expect(app.location(tester), '/members/$_id');
      expect(
        server.requests.where((r) => r.method != 'GET').map((r) => r.method),
        ['PUT', 'DELETE'],
      );

      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(find.byType(HomeScreen), findsOneWidget);
    });

    testWidgets('a profile that cannot be read after the unblock is '
        'unavailable, with no "Unblock"', (tester) async {
      // As when the other member blocked the caller meanwhile: the answer
      // of any id that names no profile.
      final server = _backend()
        ..once('GET', _profilePath, _member(name: 'Bea Ito'))
        ..once('PUT', _blockPath, (_) => noContent())
        ..once('DELETE', unblockPath, (_) => noContent())
        ..once('GET', _profilePath, (_) => noProfile());
      await block(tester, server);

      await chooseUnblock(tester);
      await tester.tap(_confirmUnblock);
      await tester.pumpAndSettle();

      expect(find.text(l10n.memberProfileUnavailable), findsOneWidget);
      expect(find.text(l10n.memberBlocked), findsNothing);
      expect(_unblock, findsNothing);
      expect(_retry, findsNothing);
      expect(_controls, findsNothing);
      expect(_actions(tester), isEmpty);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(server.count(ApiPaths.myBlocks), 0);
    });

    testWidgets('a reload that fails after the unblock shows its message and '
        'a retry, not the blocked state', (tester) async {
      final server = _backend()
        ..once('GET', _profilePath, _member(name: 'Bea Ito'))
        ..once('PUT', _blockPath, (_) => noContent())
        ..once('DELETE', unblockPath, (_) => noContent())
        ..once('GET', _profilePath, networkFailure)
        ..once('GET', _profilePath, _member(name: 'Bea Ito'));
      await block(tester, server);

      await chooseUnblock(tester);
      await tester.tap(_confirmUnblock);
      await tester.pumpAndSettle();

      expect(find.text(l10n.errorNetwork), findsOneWidget);
      expect(find.text(l10n.memberBlocked), findsNothing);
      expect(_unblock, findsNothing);

      await tapAndSettle(tester, _retry);
      expect(find.text('Bea Ito'), findsOneWidget);
      // The unblock was sent once: the retry only reads.
      expect(server.requests.where((r) => r.method == 'DELETE'), hasLength(1));
    });

    group('a failed unblock keeps the blocked state, with its message, and '
        'can be tried again', () {
      final cases = <String, (Responder, String)>{
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
        '500': (
          (_) => errorResponse(500, 'internal_error'),
          l10n.errorUnexpected,
        ),
        'a 200 where a 204 is due': (
          (_) => jsonResponse(200, {'unblocked': true}),
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
          final server = blockable()
            ..once('DELETE', unblockPath, responder)
            ..once('DELETE', unblockPath, (_) => noContent());
          final handle = tester.ensureSemantics();
          final app = await block(tester, server);

          await chooseUnblock(tester);
          await tester.tap(_confirmUnblock);
          await tester.pumpAndSettle();

          expect(find.text(message), findsOneWidget);
          expect(find.textContaining('SERVERTEXT'), findsNothing);
          expect(
            tester.getSemantics(find.byType(FormErrorBanner)),
            isSemantics(
              isLiveRegion: true,
              label: '${l10n.errorLabel}\n$message',
            ),
          );
          expectBlocked(tester);
          expect(_unblockEnabled(tester), isTrue);
          expect(find.byType(LinearProgressIndicator), findsNothing);
          expect(
            server.requests.where((r) => r.method == 'DELETE'),
            hasLength(1),
          );
          // Nothing was read again.
          expect(server.count(_profilePath), 1);
          expect(app.session.status, SessionStatus.signedIn);
          expect(app.location(tester), '/members/$_id');

          // Again: the message goes with the new attempt.
          await chooseUnblock(tester);
          await tester.tap(_confirmUnblock);
          await tester.pumpAndSettle();
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(find.text('Bea Ito'), findsOneWidget);
          expect(
            server.requests.where((r) => r.method == 'DELETE'),
            hasLength(2),
          );
          expect(server.count(ApiPaths.myBlocks), 0);
          handle.dispose();
        });
      });
    });

    testWidgets('an unblock that hasn\'t answered: the control is disabled, '
        'back does nothing, and it ends as a timeout', (tester) async {
      final server = blockable()..once('DELETE', unblockPath, neverAnswers);
      final handle = tester.ensureSemantics();
      final app = await block(tester, server);

      await chooseUnblock(tester);
      await tester.tap(_confirmUnblock);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));

      expect(find.text(l10n.unblockTitle), findsNothing);
      expect(find.bySemanticsLabel(l10n.unblockProgress), findsOneWidget);
      expectBlocked(tester);
      expect(_unblockEnabled(tester), isFalse);

      // The control doesn't open the question, and neither back leaves.
      await tester.tap(_unblock, warnIfMissed: false);
      await tester.pump(const Duration(seconds: 1));
      expect(find.byType(AlertDialog), findsNothing);
      await tester.pageBack();
      await tester.pump(const Duration(seconds: 1));
      expect(_screen, findsOneWidget);
      await tester.binding.handlePopRoute();
      await tester.pump(const Duration(seconds: 1));
      expect(_screen, findsOneWidget);
      expect(app.location(tester), '/members/$_id');
      expect(server.requests.where((r) => r.method == 'DELETE'), hasLength(1));

      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(find.bySemanticsLabel(l10n.unblockProgress), findsNothing);
      expectBlocked(tester);
      expect(_unblockEnabled(tester), isTrue);
      expect(server.requests.where((r) => r.method == 'DELETE'), hasLength(1));

      // And leaving works again.
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(find.byType(HomeScreen), findsOneWidget);
      handle.dispose();
    });

    testWidgets('confirming twice sends one unblock', (tester) async {
      final reply = Completer<http.Response>();
      final server = blockable()
        ..once('DELETE', unblockPath, (_) => reply.future);
      await block(tester, server);

      await chooseUnblock(tester);
      await tester.tap(_confirmUnblock);
      await tester.pump();
      // The dialog is on its way out and the control is disabled.
      await tester.tap(_confirmUnblock, warnIfMissed: false);
      await tester.tap(_unblock, warnIfMissed: false);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      await tester.tap(_unblock, warnIfMissed: false);
      await tester.pump(const Duration(seconds: 1));

      expect(_screen, findsOneWidget);
      expect(find.byType(AlertDialog), findsNothing);
      expect(server.requests.where((r) => r.method == 'DELETE'), hasLength(1));

      reply.complete(noContent());
      await tester.pumpAndSettle();
      expect(find.text('Bea Ito'), findsOneWidget);
      expect(server.requests.where((r) => r.method == 'DELETE'), hasLength(1));
    });

    testWidgets('an unblock answered after leaving for log in is dropped', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = blockable()
        ..once('DELETE', unblockPath, (_) => reply.future);
      final app = await block(tester, server);

      await chooseUnblock(tester);
      await tester.tap(_confirmUnblock);
      await tester.pump();
      await app.session.logout();
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);

      reply.complete(noContent());
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(app.location(tester), '/login');
      // No reload for a screen that is gone.
      expect(server.count(_profilePath), 1);
    });

    testWidgets('the session ending with the confirmation open shows no '
        'error, and nothing is sent', (tester) async {
      final server = blockable();
      final app = await block(tester, server);

      await chooseUnblock(tester);
      await app.session.logout();
      await tester.pumpAndSettle();

      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(find.text(l10n.unblockTitle), findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(app.location(tester), '/login');
      expect(server.requests.where((r) => r.method == 'DELETE'), isEmpty);
      expect(tester.takeException(), isNull);
    });

    testWidgets('an unblock refused because the session ended shows no '
        'error', (tester) async {
      final server = blockable()
        ..once(
          'DELETE',
          unblockPath,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..once(
          'POST',
          ApiPaths.refresh,
          (_) => errorResponse(401, 'invalid_refresh_token'),
        );
      final app = await block(tester, server);

      await chooseUnblock(tester);
      await tester.tap(_confirmUnblock);
      await tester.pumpAndSettle();

      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(app.location(tester), '/login');
      expect(server.requests.where((r) => r.method == 'DELETE'), hasLength(1));
      expect(tester.takeException(), isNull);
    });
  });

  group('the route', () {
    testWidgets('a signed-out user is shown log in, and nothing is asked', (
      tester,
    ) async {
      final server = _backend();
      final app = await pumpApp(tester, server: server);
      GoRouter.of(tester.element(find.byType(Navigator).first))
          .go(Routes.member(_id));
      await tester.pumpAndSettle();

      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(app.location(tester), '/login');
      expect(server.requests, isEmpty);
    });

    testWidgets('a malformed identifier is not a route: home, and no profile '
        'request', (tester) async {
      final server = _backend();
      final app = await pumpApp(tester, server: server, signedIn: true);

      for (final location in [
        '/members',
        '/members/',
        '/members/abc',
        '/members/${_id.toUpperCase()}',
        '/members/$_id/extra',
        '/members/$_id/avatar',
        '/members/${_id.replaceAll('-', '')}',
        Routes.member('abc'),
        Routes.member(''),
      ]) {
        GoRouter.of(tester.element(find.byType(Navigator).first)).go(location);
        await tester.pumpAndSettle();
        expect(find.byType(HomeScreen), findsOneWidget, reason: location);
        expect(_screen, findsNothing, reason: location);
        expect(app.location(tester), '/home', reason: location);
      }
      expect(_memberRequests(server), isEmpty);
    });

    testWidgets('Routes.member is the route and the id, and nothing else', (
      tester,
    ) async {
      expect(Routes.member(_id), '/members/$_id');
      expect(Routes.isMember(Routes.member(_id)), isTrue);
      expect(Uri.parse(Routes.member(_id)).hasQuery, isFalse);
    });

    testWidgets('back returns to the screen that opened it', (tester) async {
      final server = _backend()..always('GET', _profilePath, _member());
      final app = await _openMember(tester, server);
      await tester.pageBack();
      await tester.pumpAndSettle();

      expect(find.byType(HomeScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(app.location(tester), '/home');
    });

    testWidgets('logging out elsewhere leaves the screen for log in', (
      tester,
    ) async {
      final server = _backend()..always('GET', _profilePath, _member());
      final app = await _openMember(tester, server);
      await app.session.logout();
      await tester.pumpAndSettle();

      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(app.location(tester), '/login');
    });

    testWidgets('opening a profile again loads it again', (tester) async {
      final server = _backend()..always('GET', _profilePath, _member());
      await _openMember(tester, server);
      await tester.pageBack();
      await tester.pumpAndSettle();

      server.always('GET', _profilePath, _member(name: 'Bea Ito'));
      await _push(tester, Routes.member(_id));
      expect(find.text('Bea Ito'), findsOneWidget);
      expect(server.count(_profilePath), 2);
    });
  });
}

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

/// A backend for a signed-in user on home, with the three-language catalog;
/// the member routes are scripted by each test.
FakeServer _backend() => FakeServer()
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

      // One read of each, with nothing but the id in the path, and nothing
      // asked of the reader's own profile, languages or picture.
      expect(server.to(_profilePath).single.method, 'GET');
      expect(server.to(_profilePath).single.url.hasQuery, isFalse);
      expect(server.to(_avatarPath).single.method, 'GET');
      expect(server.to(_avatarPath).single.url.hasQuery, isFalse);
      expect(server.count(ApiPaths.languages), 1);
      expect(server.count(ApiPaths.profile), 0);
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
        // The way back is the screen's only control.
        expect(_controls, findsNothing);
        expect(_actions(tester), isEmpty);
        expect(find.byType(BackButton), findsOneWidget);
        // Nothing was written, and nothing asked of the reader's own data.
        expect(server.requests.map((r) => r.method).toSet(), {'GET'});
        expect(server.count(ApiPaths.profile), 0);
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

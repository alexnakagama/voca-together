import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/screens/home_screen.dart';
import 'package:vocatogether/screens/languages_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/profile_edit_screen.dart';
import 'package:vocatogether/screens/profile_languages_section.dart';
import 'package:vocatogether/screens/profile_screen.dart';
import 'package:vocatogether/session.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/language_chip.dart';

import '../support/fakes.dart';
import 'harness.dart';

Finder get _open => find.widgetWithText(OutlinedButton, l10n.profileButton);
Finder get _section => find.byType(ProfileLanguagesSection);
Finder get _sectionError =>
    find.descendant(of: _section, matching: find.byType(FormErrorBanner));

/// Anything in the section that can be activated.
Finder get _sectionControls => find.descendant(
  of: _section,
  matching: find.byWidgetPredicate(
    (w) => w is ButtonStyleButton || w is InkResponse,
  ),
);

/// The page's way to the edit screen, and that screen's way to the editor.
Finder get _editProfile =>
    find.widgetWithText(OutlinedButton, l10n.profileEditButton);
Finder get _form => find.byType(ProfileEditScreen);
Finder get _languagesRow =>
    find.widgetWithText(ListTile, l10n.profileEditLanguagesButton);
Finder get _editor => find.byType(LanguagesScreen);

/// The section's own retry; the profile's is a filled button.
Finder get _retry => find.widgetWithText(OutlinedButton, l10n.tryAgain);

/// From the page: the edit screen, then the languages editor.
Future<void> _openEditor(WidgetTester tester) async {
  await tapAndSettle(tester, _editProfile);
  expect(_form, findsOneWidget);
  await tapAndSettle(tester, _languagesRow);
  expect(_editor, findsOneWidget);
}

/// Back from the edit screen to the page, which loads again.
Future<void> _backToPage(WidgetTester tester) async {
  expect(_form, findsOneWidget);
  await tester.pageBack();
  await tester.pumpAndSettle();
  expect(_form, findsNothing);
  expect(find.byType(ProfileScreen), findsOneWidget);
}

/// A backend for a signed-in user on home whose profile is saved; the
/// language calls are scripted by each test.
FakeServer _backend() => FakeServer()
  ..always('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()))
  // No picture, unless a test scripts one.
  ..always('GET', ApiPaths.myAvatar, (_) => noAvatar())
  ..always('GET', ApiPaths.healthz, (_) => healthy())
  ..always('POST', ApiPaths.logout, (_) => noContent())
  ..always(
    'GET',
    ApiPaths.profile,
    (_) => jsonResponse(200, profileBody(displayName: 'Ana')),
  );

void _catalog(FakeServer server) => server.once(
  'GET',
  ApiPaths.languages,
  (_) => jsonResponse(200, catalogBody()),
);

void _languages(
  FakeServer server, {
  List<(String, String)> spoken = const [],
  List<(String, String)> learning = const [],
}) => server.once(
  'GET',
  ApiPaths.myLanguages,
  (_) => jsonResponse(200, languagesBody(spoken: spoken, learning: learning)),
);

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

/// The chips on screen as (name, level), in order.
List<(String, String)> _chips(WidgetTester tester) => [
  for (final chip in tester.widgetList<LanguageChip>(find.byType(LanguageChip)))
    (chip.name, chip.level),
];

void main() {
  group('loading', () {
    testWidgets('a labelled spinner under the profile, then "none yet"', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.myLanguages, (_) => reply.future);
      _catalog(server);
      final handle = tester.ensureSemantics();
      await _openProfile(tester, server, settle: false);

      // The profile doesn't wait for the languages.
      expect(find.text('Ana'), findsOneWidget);
      expect(_editProfile, findsOneWidget);
      expect(find.text(l10n.languagesHeading), findsOneWidget);
      expect(find.bySemanticsLabel(l10n.languagesLoading), findsOneWidget);
      expect(find.text(l10n.languagesEmpty), findsNothing);
      expect(_sectionControls, findsNothing);
      expect(
        tester.getSemantics(find.text(l10n.languagesHeading)),
        isSemantics(label: l10n.languagesHeading, isHeader: true),
      );

      reply.complete(jsonResponse(200, languagesBody()));
      await tester.pumpAndSettle();
      expect(find.bySemanticsLabel(l10n.languagesLoading), findsNothing);
      expect(find.text(l10n.languagesEmpty), findsOneWidget);
      expect(find.byType(LanguageChip), findsNothing);
      expect(find.text(l10n.languagesSpokenHeading), findsNothing);
      expect(find.text(l10n.languagesLearningHeading), findsNothing);
      expect(_sectionError, findsNothing);
      // Also with none chosen, the page offers no way in to the editor.
      expect(_sectionControls, findsNothing);
      expect(find.text('Edit languages'), findsNothing);
      handle.dispose();
    });

    testWidgets('asks for the catalog and the languages once each, when the '
        'profile has loaded', (tester) async {
      final profile = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.profile, (_) => profile.future);
      _catalog(server);
      _languages(server, spoken: [('es', 'native')]);
      await _openProfile(tester, server, settle: false);

      // Nothing of a profile is asked for until there is one to show.
      expect(server.count(ApiPaths.languages), 0);
      expect(server.count(ApiPaths.myLanguages), 0);
      expect(find.text(l10n.languagesHeading), findsNothing);

      profile.complete(jsonResponse(200, profileBody(displayName: 'Ana')));
      await tester.pumpAndSettle();
      expect(_chips(tester), [('Spanish', 'Native')]);
      expect(server.count(ApiPaths.languages), 1);
      expect(server.count(ApiPaths.myLanguages), 1);
      for (final path in [ApiPaths.languages, ApiPaths.myLanguages]) {
        expect(server.to(path).single.method, 'GET');
        expect(server.to(path).single.url.hasQuery, isFalse);
      }
    });

    testWidgets('while the profile failed to load, only its error shows', (
      tester,
    ) async {
      final server = _backend()..once('GET', ApiPaths.profile, networkFailure);
      _catalog(server);
      _languages(server, learning: [('ja', 'a2')]);
      await _openProfile(tester, server);

      expect(find.text(l10n.errorNetwork), findsOneWidget);
      expect(find.text(l10n.languagesHeading), findsNothing);
      expect(find.byType(LanguageChip), findsNothing);
      expect(server.count(ApiPaths.myLanguages), 0);

      // The profile's retry brings the languages with it.
      await tapAndSettle(
        tester,
        find.widgetWithText(FilledButton, l10n.tryAgain),
      );
      expect(_chips(tester), [('Japanese', 'A2')]);
      expect(server.count(ApiPaths.myLanguages), 1);
    });
  });

  group('the summary', () {
    testWidgets('both lists under their headings, in the member\'s order, '
        'named by the catalog', (tester) async {
      final server = _backend();
      _catalog(server);
      _languages(
        server,
        spoken: [('es', 'native'), ('en', 'c1')],
        learning: [('ja', 'a2')],
      );
      final handle = tester.ensureSemantics();
      await _openProfile(tester, server);

      expect(_chips(tester), [
        ('Spanish', 'Native'),
        ('English', 'C1'),
        ('Japanese', 'A2'),
      ]);
      expect(find.text(l10n.languagesEmpty), findsNothing);
      final spoken = tester.getTopLeft(find.text(l10n.languagesSpokenHeading));
      final learning = tester.getTopLeft(
        find.text(l10n.languagesLearningHeading),
      );
      expect(spoken.dy, lessThan(tester.getTopLeft(find.text('Spanish')).dy));
      expect(tester.getTopLeft(find.text('English')).dy, lessThan(learning.dy));
      expect(
        learning.dy,
        lessThan(tester.getTopLeft(find.text('Japanese')).dy),
      );
      for (final heading in [
        l10n.languagesSpokenHeading,
        l10n.languagesLearningHeading,
      ]) {
        expect(
          tester.getSemantics(find.text(heading)),
          isSemantics(label: heading, isHeader: true),
        );
      }
      // Read-only: the chips are no controls, and the section has none,
      // not even one that opens the editor.
      expect(
        find.descendant(
          of: find.byType(LanguageChip),
          matching: find.byWidgetPredicate(
            (w) => w is ButtonStyleButton || w is InkResponse,
          ),
        ),
        findsNothing,
      );
      expect(_sectionControls, findsNothing);
      expect(find.text('Edit languages'), findsNothing);
      handle.dispose();
    });

    testWidgets('only the list that has languages gets a heading', (
      tester,
    ) async {
      final server = _backend();
      _catalog(server);
      _languages(server, learning: [('ja', 'b1')]);
      await _openProfile(tester, server);

      expect(_chips(tester), [('Japanese', 'B1')]);
      expect(find.text(l10n.languagesLearningHeading), findsOneWidget);
      expect(find.text(l10n.languagesSpokenHeading), findsNothing);
      expect(find.text(l10n.languagesEmpty), findsNothing);
    });

    testWidgets('every level has its own label', (tester) async {
      final server = _backend()
        ..once(
          'GET',
          ApiPaths.languages,
          (_) => jsonResponse(200, {
            'languages': [
              for (final code in ['aa', 'bb', 'cc', 'dd', 'ee', 'ff', 'gg'])
                {'code': code, 'name': 'Name $code', 'endonym': code},
            ],
          }),
        );
      _languages(
        server,
        spoken: [('aa', 'a1'), ('bb', 'a2'), ('cc', 'b1'), ('dd', 'b2')],
        learning: [('ee', 'c1'), ('ff', 'c2')],
      );
      await _openProfile(tester, server);
      expect(_chips(tester).map((c) => c.$2), [
        l10n.languageLevelA1,
        l10n.languageLevelA2,
        l10n.languageLevelB1,
        l10n.languageLevelB2,
        l10n.languageLevelC1,
        l10n.languageLevelC2,
      ]);
      expect({
        ..._chips(tester).map((c) => c.$2),
        l10n.languageLevelNative,
      }, hasLength(7));
    });

    testWidgets('a language the catalog doesn\'t name is shown by its code', (
      tester,
    ) async {
      final server = _backend();
      _catalog(server);
      _languages(server, spoken: [('es', 'c2'), ('xx', 'b2')]);
      await _openProfile(tester, server);

      expect(_chips(tester), [('Spanish', 'C2'), ('xx', 'B2')]);
      expect(_sectionError, findsNothing);
    });

    testWidgets('many long names wrap at large text on a small screen', (
      tester,
    ) async {
      final server = _backend()
        ..once(
          'GET',
          ApiPaths.languages,
          (_) => jsonResponse(200, {
            'languages': [
              for (var i = 0; i < 10; i++)
                {
                  'code': 'l${String.fromCharCode(97 + i)}',
                  'name': 'Norwegian Bokmål $i',
                  'endonym': 'Norsk bokmål',
                },
            ],
          }),
        );
      List<(String, String)> list(int from) => [
        for (var i = from; i < from + 5; i++)
          ('l${String.fromCharCode(97 + i)}', 'native'),
      ];
      _languages(server, spoken: list(0), learning: list(5));
      final app = await pumpApp(
        tester,
        server: server,
        signedIn: true,
        size: const Size(320, 480),
        textScale: 2,
      );
      await tapAndSettle(tester, _open);
      expect(app.location(tester), '/profile');
      expect(tester.takeException(), isNull);
      await tester.ensureVisible(find.text('Norwegian Bokmål 9'));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(_chips(tester), hasLength(10));
    });
  });

  group('a failed load', () {
    final cases = <String, (void Function(FakeServer), String)>{
      'the languages, 500': (
        (s) {
          _catalog(s);
          s.once(
            'GET',
            ApiPaths.myLanguages,
            (_) => errorResponse(500, 'internal_error'),
          );
        },
        l10n.errorUnexpected,
      ),
      'the catalog, network': (
        (s) {
          s.once('GET', ApiPaths.languages, networkFailure);
          _languages(s, spoken: [('es', 'native')]);
        },
        l10n.errorNetwork,
      ),
      'both': (
        (s) => s
          ..once('GET', ApiPaths.languages, networkFailure)
          ..once('GET', ApiPaths.myLanguages, networkFailure),
        l10n.errorNetwork,
      ),
      '429': (
        (s) {
          _catalog(s);
          s.once(
            'GET',
            ApiPaths.myLanguages,
            (_) => errorResponse(
              429,
              'rate_limited',
              headers: {'retry-after': '10'},
            ),
          );
        },
        'Too many attempts. Try again in 10 seconds.',
      ),
      // Never shown without the entry: the load fails whole (030).
      'a level this app doesn\'t know': (
        (s) {
          _catalog(s);
          _languages(s, spoken: [('es', 'native'), ('en', 'c3')]);
        },
        l10n.errorUnexpected,
      ),
      // Not "none yet": only a 200 with two lists is.
      'a 404': (
        (s) {
          _catalog(s);
          s.once(
            'GET',
            ApiPaths.myLanguages,
            (_) => http.Response('Not Found', 404),
          );
        },
        l10n.errorUnexpected,
      ),
      'a malformed catalog': (
        (s) {
          s.once(
            'GET',
            ApiPaths.languages,
            (_) => jsonResponse(200, {'languages': null}),
          );
          _languages(s, spoken: [('es', 'native')]);
        },
        l10n.errorUnexpected,
      ),
    };
    cases.forEach((name, c) {
      final (script, message) = c;
      testWidgets('$name: an error and a retry in the section, and the rest '
          'of the page as it is', (tester) async {
        final server = _backend();
        script(server);
        final app = await _openProfile(tester, server);

        expect(
          find.descendant(of: _sectionError, matching: find.text(message)),
          findsOneWidget,
        );
        expect(find.byType(FormErrorBanner), findsOneWidget);
        expect(find.byType(LanguageChip), findsNothing);
        expect(find.text(l10n.languagesEmpty), findsNothing);
        expect(app.session.status, SessionStatus.signedIn);
        expect(app.location(tester), '/profile');

        // The profile is there, with its way to the edit screen.
        expect(find.text('Ana'), findsOneWidget);
        expect(_editProfile, findsOneWidget);

        // The retry asks for both again.
        _catalog(server);
        _languages(server, spoken: [('es', 'native')]);
        await tapAndSettle(tester, _retry);
        expect(_sectionError, findsNothing);
        expect(_retry, findsNothing);
        expect(_chips(tester), [('Spanish', 'Native')]);
        expect(server.count(ApiPaths.languages), 2);
        expect(server.count(ApiPaths.myLanguages), 2);
        // The profile was not asked for again.
        expect(find.text('Ana'), findsOneWidget);
        expect(server.count(ApiPaths.profile), 1);
      });
    });

    testWidgets('a retry shows the spinner, and can fail again', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.myLanguages, networkFailure)
        ..once('GET', ApiPaths.myLanguages, (_) => reply.future);
      _catalog(server);
      _catalog(server);
      final handle = tester.ensureSemantics();
      await _openProfile(tester, server);

      await tester.ensureVisible(_retry);
      await tester.pumpAndSettle();
      await tester.tap(_retry);
      await tester.pump();
      expect(find.bySemanticsLabel(l10n.languagesLoading), findsOneWidget);
      expect(_sectionError, findsNothing);
      expect(_retry, findsNothing);

      reply.complete(errorResponse(503, 'service_unavailable'));
      await tester.pumpAndSettle();
      expect(
        find.descendant(
          of: _sectionError,
          matching: find.text(l10n.errorUnavailableNoWait),
        ),
        findsOneWidget,
      );
      expect(_retry, findsOneWidget);
      handle.dispose();
    });

    testWidgets('a load that gets no answer times out', (tester) async {
      final server = _backend()
        ..once('GET', ApiPaths.myLanguages, neverAnswers);
      _catalog(server);
      await _openProfile(tester, server, settle: false);

      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(
        find.descendant(
          of: _sectionError,
          matching: find.text(l10n.errorTimeout),
        ),
        findsOneWidget,
      );
      expect(_editProfile, findsOneWidget);
    });
  });

  group('the session and navigation', () {
    testWidgets('a session that ended while loading shows no error', (
      tester,
    ) async {
      final server = _backend()
        ..always(
          'GET',
          ApiPaths.myLanguages,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..always(
          'GET',
          ApiPaths.languages,
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
      expect(tester.takeException(), isNull);
    });

    testWidgets('logging out elsewhere while loading leaves for log in', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.myLanguages, (_) => reply.future);
      _catalog(server);
      final app = await _openProfile(tester, server, settle: false);

      await app.session.logout();
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);

      reply.complete(errorResponse(401, 'invalid_access_token'));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(server.count(ApiPaths.refresh), 0);
    });

    testWidgets('an answer that arrives after leaving is dropped', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.myLanguages, (_) => reply.future);
      _catalog(server);
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

    testWidgets('opening the profile again loads the languages again', (
      tester,
    ) async {
      final server = _backend();
      _catalog(server);
      _languages(server);
      await _openProfile(tester, server);
      expect(find.text(l10n.languagesEmpty), findsOneWidget);

      await tester.pageBack();
      await tester.pumpAndSettle();
      _catalog(server);
      _languages(server, learning: [('en', 'b2')]);
      await tapAndSettle(tester, _open);
      expect(_chips(tester), [('English', 'B2')]);
    });
  });

  group('the result of the editor', () {
    /// The catalog always; the selection as scripted, request by request.
    FakeServer backend() => _backend()
      ..always(
        'GET',
        ApiPaths.languages,
        (_) => jsonResponse(200, catalogBody()),
      );

    testWidgets('after a save, the page shows the new selection, in the new '
        'order, when the member comes back to it', (tester) async {
      final server = backend();
      _languages(server, spoken: [('es', 'native'), ('en', 'c1')]);
      final app = await _openProfile(tester, server);
      expect(_chips(tester), [('Spanish', 'Native'), ('English', 'C1')]);

      _languages(server, spoken: [('es', 'native'), ('en', 'c1')]);
      await _openEditor(tester);
      expect(app.location(tester), '/profile/languages');

      await tapAndSettle(tester, find.text(l10n.languagesAddButton).last);
      await tapAndSettle(tester, find.text('Japanese'));
      await tapAndSettle(tester, find.text(l10n.languageLevelA2).last);
      await tapAndSettle(
        tester,
        find.byTooltip(l10n.languageMoveUp('English')),
      );
      server.once(
        'PUT',
        ApiPaths.myLanguages,
        (_) => jsonResponse(
          200,
          languagesBody(
            spoken: [('en', 'c1'), ('es', 'native')],
            learning: [('ja', 'a2')],
          ),
        ),
      );
      await tapAndSettle(
        tester,
        find.widgetWithText(FilledButton, l10n.languagesSaveButton),
      );

      // The editor closes on the edit screen, which shows no language and
      // asks for none.
      expect(_editor, findsNothing);
      expect(_form, findsOneWidget);
      expect(app.location(tester), '/profile/edit');
      expect(find.byType(LanguageChip), findsNothing);
      expect(server.count(ApiPaths.myLanguages), 3);

      _languages(
        server,
        spoken: [('en', 'c1'), ('es', 'native')],
        learning: [('ja', 'a2')],
      );
      await _backToPage(tester);
      expect(app.location(tester), '/profile');
      expect(_chips(tester), [
        ('English', 'C1'),
        ('Spanish', 'Native'),
        ('Japanese', 'A2'),
      ]);
      // The page, the editor, its save, and the page again: the section
      // never saves.
      expect(server.to(ApiPaths.myLanguages).map((r) => r.method), [
        'GET',
        'GET',
        'PUT',
        'GET',
      ]);
      // The page, the edit screen, and the page again.
      expect(server.count(ApiPaths.profile), 3);
    });

    testWidgets('after leaving the editor without saving, the page loads the '
        'languages again', (tester) async {
      final server = backend();
      _languages(server, spoken: [('es', 'native')]);
      await _openProfile(tester, server);

      _languages(server, spoken: [('es', 'native')]);
      await _openEditor(tester);
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(_editor, findsNothing);
      // What the server has by now, whoever stored it.
      _languages(server, spoken: [('es', 'native'), ('en', 'b2')]);
      await _backToPage(tester);

      expect(_chips(tester), [('Spanish', 'Native'), ('English', 'B2')]);
      expect(server.count(ApiPaths.myLanguages), 3);
      expect(
        server.to(ApiPaths.myLanguages).map((r) => r.method),
        everyElement('GET'),
      );
    });

    testWidgets('a failed reload shows the section\'s error and retry, and '
        'the rest of the page as it is', (tester) async {
      final server = backend();
      _languages(server, spoken: [('es', 'native')]);
      await _openProfile(tester, server);
      _languages(server, spoken: [('es', 'native')]);
      await _openEditor(tester);
      await tester.pageBack();
      await tester.pumpAndSettle();
      server.once('GET', ApiPaths.myLanguages, networkFailure);
      await _backToPage(tester);

      expect(
        find.descendant(
          of: _sectionError,
          matching: find.text(l10n.errorNetwork),
        ),
        findsOneWidget,
      );
      // Never the selection shown before: it may no longer be stored.
      expect(find.byType(LanguageChip), findsNothing);
      expect(find.byType(FormErrorBanner), findsOneWidget);
      expect(find.text('Ana'), findsOneWidget);
      expect(find.text(l10n.profileFriendsHeading), findsOneWidget);
      expect(_editProfile, findsOneWidget);

      _languages(server, spoken: [('es', 'native')]);
      await tapAndSettle(tester, _retry);
      expect(_chips(tester), [('Spanish', 'Native')]);
      expect(_sectionError, findsNothing);
    });
  });

  testWidgets('home loads no languages by itself', (tester) async {
    final server = _backend();
    await pumpApp(tester, server: server, signedIn: true);
    expect(_open, findsOneWidget);
    expect(server.count(ApiPaths.languages), 0);
    expect(server.count(ApiPaths.myLanguages), 0);
  });
}

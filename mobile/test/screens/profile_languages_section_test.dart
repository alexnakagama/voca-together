import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/screens/home_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/profile_languages_section.dart';
import 'package:vocatogether/screens/profile_screen.dart';
import 'package:vocatogether/session.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/language_chip.dart';

import '../support/fakes.dart';
import 'harness.dart';

Finder get _open => find.widgetWithText(OutlinedButton, l10n.profileButton);
Finder get _save => find.widgetWithText(FilledButton, l10n.profileSaveButton);
Finder get _section => find.byType(ProfileLanguagesSection);
Finder get _sectionError =>
    find.descendant(of: _section, matching: find.byType(FormErrorBanner));

/// The section's own retry; the profile's is a filled button.
Finder get _retry => find.widgetWithText(OutlinedButton, l10n.tryAgain);

/// A backend for a signed-in user on home whose profile is saved; the
/// language calls are scripted by each test.
FakeServer _backend() => FakeServer()
  ..always('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()))
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
    testWidgets('a labelled spinner under the form, then "none yet"', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.myLanguages, (_) => reply.future);
      _catalog(server);
      final handle = tester.ensureSemantics();
      await _openProfile(tester, server, settle: false);

      // The form doesn't wait for the languages.
      expect(_save, findsOneWidget);
      expect(find.text(l10n.languagesHeading), findsOneWidget);
      expect(find.bySemanticsLabel(l10n.languagesLoading), findsOneWidget);
      expect(find.text(l10n.languagesEmpty), findsNothing);
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
      handle.dispose();
    });

    testWidgets('asks for the catalog and the languages once each, when the '
        'screen opens', (tester) async {
      final profile = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.profile, (_) => profile.future);
      _catalog(server);
      _languages(server, spoken: [('es', 'native')]);
      await _openProfile(tester, server, settle: false);

      // Already asked, but shown only with the form.
      expect(server.count(ApiPaths.languages), 1);
      expect(server.count(ApiPaths.myLanguages), 1);
      expect(find.text(l10n.languagesHeading), findsNothing);
      expect(find.byType(LanguageChip), findsNothing);

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

      // The profile's retry shows the languages that had already loaded.
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
      // Read-only: nothing in the section can be tapped.
      expect(
        find.descendant(of: _section, matching: find.byType(ButtonStyleButton)),
        findsNothing,
      );
      expect(
        find.descendant(of: _section, matching: find.byType(InkResponse)),
        findsNothing,
      );
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
      testWidgets('$name: an error and a retry in the section, and the form '
          'still works', (tester) async {
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

        // The profile is there and saves.
        expect(
          textFieldOf(tester, field(l10n.displayNameLabel)).controller!.text,
          'Ana',
        );
        server.once(
          'PUT',
          ApiPaths.profile,
          (_) => jsonResponse(200, profileBody(displayName: 'Ana')),
        );
        await tapAndSettle(tester, _save);
        expect(find.text(l10n.profileSaved), findsOneWidget);
        expect(_sectionError, findsOneWidget);

        // The retry asks for both again.
        _catalog(server);
        _languages(server, spoken: [('es', 'native')]);
        await tapAndSettle(tester, _retry);
        expect(_sectionError, findsNothing);
        expect(_retry, findsNothing);
        expect(_chips(tester), [('Spanish', 'Native')]);
        expect(server.count(ApiPaths.languages), 2);
        expect(server.count(ApiPaths.myLanguages), 2);
        // The form was left as it was.
        expect(find.text(l10n.profileSaved), findsOneWidget);
        expect(server.count(ApiPaths.profile), 2);
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
      expect(_save, findsOneWidget);
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

  testWidgets('saving the profile neither reloads nor sends the languages', (
    tester,
  ) async {
    final server = _backend()
      ..once(
        'PUT',
        ApiPaths.profile,
        (_) => jsonResponse(200, profileBody(displayName: 'Ana López')),
      );
    _catalog(server);
    _languages(server, spoken: [('es', 'native')]);
    await _openProfile(tester, server);

    await tester.enterText(field(l10n.displayNameLabel), 'Ana López');
    await tester.pumpAndSettle();
    await tapAndSettle(tester, _save);
    expect(find.text(l10n.profileSaved), findsOneWidget);
    expect(_chips(tester), [('Spanish', 'Native')]);
    expect(server.count(ApiPaths.languages), 1);
    expect(server.count(ApiPaths.myLanguages), 1);
    expect(server.requests.last.body, isNot(contains('spoken')));
  });

  testWidgets('home loads no languages by itself', (tester) async {
    final server = _backend();
    await pumpApp(tester, server: server, signedIn: true);
    expect(_open, findsOneWidget);
    expect(server.count(ApiPaths.languages), 0);
    expect(server.count(ApiPaths.myLanguages), 0);
  });
}

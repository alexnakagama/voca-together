import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/screens/languages_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/profile_screen.dart';
import 'package:vocatogether/session.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/language_row.dart';
import 'package:vocatogether/ui/widgets/primary_button.dart';
import 'package:vocatogether/ui/widgets/secondary_button.dart';

import '../support/fakes.dart';
import 'harness.dart';

typedef _Entries = List<(String, String)>;

Finder get _openProfile =>
    find.widgetWithText(OutlinedButton, l10n.profileButton);
Finder get _edit =>
    find.widgetWithText(OutlinedButton, l10n.languagesEditButton);
Finder get _save => find.widgetWithText(FilledButton, l10n.languagesSaveButton);
Finder get _cancel =>
    find.widgetWithText(OutlinedButton, l10n.languagesCancelButton);
Finder get _editor => find.byType(LanguagesScreen);
Finder get _sheet => find.byType(BottomSheet);
Finder get _dialog => find.byType(AlertDialog);
Finder get _banners =>
    find.descendant(of: _editor, matching: find.byType(FormErrorBanner));

/// "Add a language" under "I speak" (0) or "I'm learning" (1).
Finder _add(int list) => find.text(l10n.languagesAddButton).at(list);

Finder _inSheet(String text) =>
    find.descendant(of: _sheet, matching: find.text(text));

bool _saveEnabled(WidgetTester tester) =>
    tester
        .widget<PrimaryButton>(
          find.descendant(of: _editor, matching: find.byType(PrimaryButton)),
        )
        .onPressed !=
    null;

/// A backend for a signed-in user with a profile, the three-language
/// catalog, and [spoken] and [learning] stored. A `PUT` must be scripted.
FakeServer _backend({
  _Entries spoken = const [],
  _Entries learning = const [],
  Map<String, Object?>? catalog,
}) => FakeServer()
  ..always('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()))
  ..always('GET', ApiPaths.healthz, (_) => healthy())
  ..always('POST', ApiPaths.logout, (_) => noContent())
  ..always(
    'GET',
    ApiPaths.profile,
    (_) => jsonResponse(200, profileBody(displayName: 'Ana')),
  )
  ..always(
    'GET',
    ApiPaths.languages,
    (_) => jsonResponse(200, catalog ?? catalogBody()),
  )
  ..always(
    'GET',
    ApiPaths.myLanguages,
    (_) => jsonResponse(200, languagesBody(spoken: spoken, learning: learning)),
  );

/// Answers the next save as the backend does: 200 with what was sent.
void _accept(FakeServer server) => server.once(
  'PUT',
  ApiPaths.myLanguages,
  (request) => jsonResponse(200, jsonDecode(request.body)),
);

http.Response _validation(_Entries fields) => jsonResponse(422, {
  'error': {
    'code': 'validation_failed',
    'fields': [
      for (final (field, code) in fields) {'field': field, 'code': code},
    ],
  },
});

List<http.Request> _puts(FakeServer server) => [
  for (final request in server.to(ApiPaths.myLanguages))
    if (request.method == 'PUT') request,
];

/// Opens the profile and, from its languages section, the editor.
Future<TestApp> _openEditor(
  WidgetTester tester,
  FakeServer server, {
  Size? size,
  double textScale = 1,
  double keyboard = 0,
}) async {
  final app = await pumpApp(
    tester,
    server: server,
    signedIn: true,
    size: size,
    textScale: textScale,
    keyboard: keyboard,
  );
  await tapAndSettle(tester, _openProfile);
  await tapAndSettle(tester, _edit);
  expect(_editor, findsOneWidget);
  expect(app.location(tester), '/profile/languages');
  return app;
}

/// The rows of one list as (name, level), in order.
_Entries _rows(WidgetTester tester, {required bool learning}) {
  final split = tester.getTopLeft(find.text(l10n.languagesLearningHeading)).dy;
  return [
    for (final element in find.byType(LanguageRow).evaluate())
      if ((tester.getTopLeft(find.byWidget(element.widget)).dy > split) ==
          learning)
        (
          (element.widget as LanguageRow).name,
          (element.widget as LanguageRow).level,
        ),
  ];
}

_Entries _spoken(WidgetTester tester) => _rows(tester, learning: false);
_Entries _learning(WidgetTester tester) => _rows(tester, learning: true);

/// Adds [name] at [level] to a list through the picker and the level sheet.
Future<void> _pick(
  WidgetTester tester,
  int list,
  String name,
  String level,
) async {
  await tapAndSettle(tester, _add(list));
  await tapAndSettle(tester, _inSheet(name));
  await tapAndSettle(tester, _inSheet(level));
  expect(_sheet, findsNothing);
}

Future<void> _tapTooltip(WidgetTester tester, String tooltip) =>
    tapAndSettle(tester, find.byTooltip(tooltip));

bool _enabled(WidgetTester tester, String tooltip) =>
    tester
        .widget<IconButton>(
          find.ancestor(
            of: find.byTooltip(tooltip),
            matching: find.byType(IconButton),
          ),
        )
        .onPressed !=
    null;

/// The system's back, as the Android button or gesture sends it.
Future<void> _systemBack(WidgetTester tester) async {
  await tester.binding.handlePopRoute();
  await tester.pumpAndSettle();
}

void main() {
  group('loading', () {
    testWidgets('a labelled spinner, then the notice and both lists', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once(
          'GET',
          ApiPaths.myLanguages,
          (_) => jsonResponse(200, languagesBody()),
        )
        ..once('GET', ApiPaths.myLanguages, (_) => reply.future);
      final handle = tester.ensureSemantics();
      final app = await pumpApp(tester, server: server, signedIn: true);
      await tapAndSettle(tester, _openProfile);
      await tester.ensureVisible(_edit);
      await tester.tap(_edit);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));

      expect(app.location(tester), '/profile/languages');
      expect(find.bySemanticsLabel(l10n.languagesLoading), findsOneWidget);
      expect(find.text(l10n.languagesVisibilityNotice), findsNothing);
      expect(find.text(l10n.languagesAddButton), findsNothing);
      expect(_save, findsNothing);

      reply.complete(
        jsonResponse(
          200,
          languagesBody(
            spoken: [('es', 'native'), ('en', 'c1')],
            learning: [('ja', 'a2')],
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.bySemanticsLabel(l10n.languagesLoading), findsNothing);
      expect(_spoken(tester), [('Spanish', 'Native'), ('English', 'C1')]);
      expect(_learning(tester), [('Japanese', 'A2')]);
      // The name the language gives itself, where it differs.
      expect(find.text('Español'), findsOneWidget);
      expect(find.text('日本語'), findsOneWidget);
      expect(find.text('English'), findsOneWidget);
      expect(find.text(l10n.languagesAddButton), findsNWidgets(2));
      expect(_banners, findsNothing);
      for (final heading in [
        l10n.languagesSpokenHeading,
        l10n.languagesLearningHeading,
      ]) {
        expect(
          tester.getSemantics(find.text(heading)),
          isSemantics(label: heading, isHeader: true),
        );
      }
      // The member is told before they can save.
      expect(
        tester.getTopLeft(find.text(l10n.languagesVisibilityNotice)).dy,
        lessThan(tester.getTopLeft(_save).dy),
      );
      handle.dispose();
    });

    testWidgets('asks for the catalog and the selection once each', (
      tester,
    ) async {
      final server = _backend(spoken: [('es', 'native')]);
      await _openEditor(tester, server);
      // Once for the profile's summary, once for the editor.
      expect(server.count(ApiPaths.languages), 2);
      expect(server.count(ApiPaths.myLanguages), 2);
      for (final request
          in server
              .to(ApiPaths.languages)
              .followedBy(server.to(ApiPaths.myLanguages))) {
        expect(request.method, 'GET');
        expect(request.url.hasQuery, isFalse);
      }
    });

    testWidgets('with no language chosen, two empty lists', (tester) async {
      await _openEditor(tester, _backend());
      expect(find.byType(LanguageRow), findsNothing);
      expect(find.text(l10n.languagesSpokenHeading), findsOneWidget);
      expect(find.text(l10n.languagesLearningHeading), findsOneWidget);
      expect(find.text(l10n.languagesAddButton), findsNWidgets(2));
      expect(_saveEnabled(tester), isFalse);
    });

    final failures = <String, (void Function(FakeServer), String)>{
      'the selection, 500': (
        (s) => s.once(
          'GET',
          ApiPaths.myLanguages,
          (_) => errorResponse(500, 'internal_error'),
        ),
        l10n.errorUnexpected,
      ),
      'the catalog, network': (
        (s) => s.once('GET', ApiPaths.languages, networkFailure),
        l10n.errorNetwork,
      ),
      'a level this app doesn\'t know': (
        (s) => s.once(
          'GET',
          ApiPaths.myLanguages,
          (_) => jsonResponse(200, languagesBody(spoken: [('es', 'c3')])),
        ),
        l10n.errorUnexpected,
      ),
    };
    failures.forEach((name, c) {
      final (script, message) = c;
      testWidgets('$name: an error and a retry, and nothing to edit', (
        tester,
      ) async {
        final server = _backend(spoken: [('es', 'native')]);
        final app = await pumpApp(tester, server: server, signedIn: true);
        await tapAndSettle(tester, _openProfile);
        script(server);
        await tapAndSettle(tester, _edit);

        expect(
          find.descendant(of: _banners, matching: find.text(message)),
          findsOneWidget,
        );
        expect(find.byType(LanguageRow), findsNothing);
        expect(find.text(l10n.languagesAddButton), findsNothing);
        expect(find.text(l10n.languagesVisibilityNotice), findsNothing);
        expect(_save, findsNothing);
        expect(app.session.status, SessionStatus.signedIn);

        // The retry shows the spinner and asks for both again.
        final before = (
          server.count(ApiPaths.languages),
          server.count(ApiPaths.myLanguages),
        );
        await tapAndSettle(
          tester,
          find.widgetWithText(FilledButton, l10n.tryAgain),
        );
        expect(_banners, findsNothing);
        expect(_spoken(tester), [('Spanish', 'Native')]);
        expect(server.count(ApiPaths.languages), before.$1 + 1);
        expect(server.count(ApiPaths.myLanguages), before.$2 + 1);
      });
    });

    testWidgets('a load that gets no answer times out', (tester) async {
      final server = _backend();
      final app = await pumpApp(tester, server: server, signedIn: true);
      await tapAndSettle(tester, _openProfile);
      server.once('GET', ApiPaths.myLanguages, neverAnswers);
      await tester.ensureVisible(_edit);
      await tester.tap(_edit);
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(app.location(tester), '/profile/languages');
    });

    testWidgets('a session that ended while loading shows no error', (
      tester,
    ) async {
      final server = _backend();
      final app = await pumpApp(tester, server: server, signedIn: true);
      await tapAndSettle(tester, _openProfile);
      server
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
      await tapAndSettle(tester, _edit);

      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(tester.takeException(), isNull);
    });

    testWidgets('an answer that arrives after leaving is dropped', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend();
      await pumpApp(tester, server: server, signedIn: true);
      await tapAndSettle(tester, _openProfile);
      server.once('GET', ApiPaths.myLanguages, (_) => reply.future);
      await tester.ensureVisible(_edit);
      await tester.tap(_edit);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));

      // Nothing was loaded, so nothing is asked.
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(_dialog, findsNothing);
      expect(find.byType(ProfileScreen), findsOneWidget);
      expect(_editor, findsNothing);

      reply.complete(errorResponse(500, 'internal_error'));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.text(l10n.errorUnexpected), findsNothing);
    });

    testWidgets('leaving from the load error asks nothing', (tester) async {
      final server = _backend();
      await pumpApp(tester, server: server, signedIn: true);
      await tapAndSettle(tester, _openProfile);
      server.once('GET', ApiPaths.languages, networkFailure);
      await tapAndSettle(tester, _edit);
      expect(find.text(l10n.errorNetwork), findsOneWidget);

      await _systemBack(tester);
      expect(_dialog, findsNothing);
      expect(find.byType(ProfileScreen), findsOneWidget);
    });

    testWidgets('a language the catalog doesn\'t name is shown by its code, '
        'can be edited, and is kept by a save', (tester) async {
      final server = _backend(spoken: [('es', 'c2'), ('xx', 'b2')]);
      await _openEditor(tester, server);
      expect(_spoken(tester), [('Spanish', 'C2'), ('xx', 'B2')]);
      expect(find.text('xx'), findsOneWidget);

      await _tapTooltip(tester, l10n.languageMoveUp('xx'));
      expect(_spoken(tester), [('xx', 'B2'), ('Spanish', 'C2')]);
      await _pick(tester, 1, 'Japanese', 'A1');
      _accept(server);
      await tapAndSettle(tester, _save);
      expect(jsonDecode(_puts(server).single.body), {
        'spoken': [
          {'language': 'xx', 'level': 'b2'},
          {'language': 'es', 'level': 'c2'},
        ],
        'learning': [
          {'language': 'ja', 'level': 'a1'},
        ],
      });
    });
  });

  group('adding a language', () {
    testWidgets('to an empty list, through the picker and the level', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      await _openEditor(tester, _backend());

      await tapAndSettle(tester, _add(1));
      expect(
        tester.getSemantics(_inSheet(l10n.languagePickerTitle)),
        isSemantics(label: l10n.languagePickerTitle, isHeader: true),
      );
      // The whole catalog, each language with its own name.
      expect(_inSheet('English'), findsOneWidget);
      expect(_inSheet('Spanish'), findsOneWidget);
      expect(_inSheet('Español'), findsOneWidget);
      expect(_inSheet('日本語'), findsOneWidget);

      await tapAndSettle(tester, _inSheet('Japanese'));
      expect(
        _inSheet(l10n.languageLevelPickerTitle('Japanese')),
        findsOneWidget,
      );
      await tapAndSettle(tester, _inSheet(l10n.languageLevelA2));

      expect(_sheet, findsNothing);
      expect(_learning(tester), [('Japanese', 'A2')]);
      expect(_spoken(tester), isEmpty);
      expect(_saveEnabled(tester), isTrue);
      handle.dispose();
    });

    testWidgets('a new language goes last', (tester) async {
      await _openEditor(tester, _backend(spoken: [('es', 'native')]));
      await _pick(tester, 0, 'English', 'C1');
      expect(_spoken(tester), [('Spanish', 'Native'), ('English', 'C1')]);
    });

    testWidgets('the search matches the name, the own name and the code, '
        'whatever the case', (tester) async {
      await _openEditor(tester, _backend());
      await tapAndSettle(tester, _add(0));
      final search = find.descendant(
        of: _sheet,
        matching: find.byType(TextField),
      );

      List<String> shown() => [
        for (final tile in tester.widgetList<ListTile>(
          find.descendant(of: _sheet, matching: find.byType(ListTile)),
        ))
          (tile.title! as Text).data!,
      ];
      expect(shown(), ['English', 'Japanese', 'Spanish']);

      // The own name: "Español".
      await tester.enterText(search, 'ESPA');
      await tester.pumpAndSettle();
      expect(shown(), ['Spanish']);
      // The English name.
      await tester.enterText(search, ' japan ');
      await tester.pumpAndSettle();
      expect(shown(), ['Japanese']);
      // The code.
      await tester.enterText(search, 'JA');
      await tester.pumpAndSettle();
      expect(shown(), ['Japanese']);
      await tester.enterText(search, '本');
      await tester.pumpAndSettle();
      expect(shown(), ['Japanese']);

      await tester.enterText(search, 'klingon');
      await tester.pumpAndSettle();
      expect(shown(), isEmpty);
      expect(_inSheet(l10n.languagePickerNoMatch), findsOneWidget);

      await tester.enterText(search, '');
      await tester.pumpAndSettle();
      expect(shown(), ['English', 'Japanese', 'Spanish']);
      expect(_inSheet(l10n.languagePickerNoMatch), findsNothing);
    });

    testWidgets('a language already in either list is not offered', (
      tester,
    ) async {
      await _openEditor(
        tester,
        _backend(spoken: [('es', 'native')], learning: [('ja', 'a2')]),
      );
      for (final list in [0, 1]) {
        await tapAndSettle(tester, _add(list));
        expect(_inSheet('English'), findsOneWidget);
        expect(_inSheet('Spanish'), findsNothing);
        expect(_inSheet('Japanese'), findsNothing);
        await _systemBack(tester);
        expect(_sheet, findsNothing);
      }
    });

    testWidgets('backing out of the picker or of the level adds nothing', (
      tester,
    ) async {
      await _openEditor(tester, _backend(spoken: [('es', 'native')]));

      await tapAndSettle(tester, _add(0));
      await _systemBack(tester);
      expect(_sheet, findsNothing);
      expect(_editor, findsOneWidget);

      await tapAndSettle(tester, _add(0));
      await tapAndSettle(tester, _inSheet('English'));
      expect(
        _inSheet(l10n.languageLevelPickerTitle('English')),
        findsOneWidget,
      );
      await _systemBack(tester);
      expect(_sheet, findsNothing);

      expect(_spoken(tester), [('Spanish', 'Native')]);
      expect(_learning(tester), isEmpty);
      expect(_saveEnabled(tester), isFalse);
      // Nothing changed, so leaving asks nothing.
      await _systemBack(tester);
      expect(_dialog, findsNothing);
      expect(find.byType(ProfileScreen), findsOneWidget);
    });

    testWidgets('a large catalog is built lazily and scrolls to its end, '
        'with the keyboard open', (tester) async {
      await _openEditor(
        tester,
        _backend(catalog: largeCatalogBody()),
        size: const Size(360, 640),
        keyboard: 280,
      );
      await tapAndSettle(tester, _add(0));
      final tiles = find.descendant(
        of: _sheet,
        matching: find.byType(ListTile),
      );
      expect(tiles, findsWidgets);
      expect(tiles.evaluate().length, lessThan(40));
      expect(_inSheet('Language 119'), findsNothing);

      await tester.scrollUntilVisible(
        _inSheet('Language 119'),
        400,
        scrollable: find
            .descendant(of: _sheet, matching: find.byType(Scrollable))
            .first,
        maxScrolls: 200,
      );
      expect(tester.takeException(), isNull);
      // Above the keyboard, where it can be tapped.
      expect(
        tester.getBottomLeft(_inSheet('Language 119')).dy,
        lessThanOrEqualTo(640 - 280),
      );
      // The search field stayed in reach.
      expect(
        find
            .descendant(of: _sheet, matching: find.byType(TextField))
            .hitTestable(),
        findsOneWidget,
      );
      await tapAndSettle(tester, _inSheet('Language 119'));
      await tapAndSettle(tester, _inSheet(l10n.languageLevelB1));
      expect(_spoken(tester), [('Language 119', 'B1')]);
    });
  });

  group('choosing a level', () {
    List<String> offered(WidgetTester tester) => [
      for (final tile in tester.widgetList<ListTile>(
        find.descendant(of: _sheet, matching: find.byType(ListTile)),
      ))
        (tile.title! as Text).data!,
    ];

    testWidgets('a spoken language may be native; one being learned is '
        'offered A1 to C2', (tester) async {
      await _openEditor(tester, _backend());

      await tapAndSettle(tester, _add(0));
      await tapAndSettle(tester, _inSheet('Spanish'));
      expect(offered(tester), ['A1', 'A2', 'B1', 'B2', 'C1', 'C2', 'Native']);
      // Each level says what it means.
      for (final description in [
        l10n.languageLevelDescriptionA1,
        l10n.languageLevelDescriptionB2,
        l10n.languageLevelDescriptionNative,
      ]) {
        expect(_inSheet(description), findsOneWidget);
      }
      await tapAndSettle(tester, _inSheet(l10n.languageLevelNative));

      await tapAndSettle(tester, _add(1));
      await tapAndSettle(tester, _inSheet('Japanese'));
      expect(offered(tester), ['A1', 'A2', 'B1', 'B2', 'C1', 'C2']);
      expect(_inSheet(l10n.languageLevelNative), findsNothing);
      await tapAndSettle(tester, _inSheet(l10n.languageLevelC2));

      expect(_spoken(tester), [('Spanish', 'Native')]);
      expect(_learning(tester), [('Japanese', 'C2')]);
    });

    testWidgets('a row\'s level button changes the level in place, with the '
        'current one marked', (tester) async {
      await _openEditor(tester, _backend(spoken: [('es', 'b1'), ('en', 'c1')]));
      await tapAndSettle(tester, find.text('B1'));
      expect(
        _inSheet(l10n.languageLevelPickerTitle('Spanish')),
        findsOneWidget,
      );
      final tiles = tester
          .widgetList<ListTile>(
            find.descendant(of: _sheet, matching: find.byType(ListTile)),
          )
          .toList();
      expect(
        [for (final tile in tiles) tile.selected],
        [false, false, true, false, false, false, false],
      );
      await tapAndSettle(tester, _inSheet(l10n.languageLevelNative));

      expect(_spoken(tester), [('Spanish', 'Native'), ('English', 'C1')]);
      expect(_saveEnabled(tester), isTrue);
      // Under "I'm learning" the same button offers no "Native".
    });

    testWidgets('closing the level choice keeps the level', (tester) async {
      await _openEditor(tester, _backend(learning: [('ja', 'a2')]));
      await tapAndSettle(tester, find.text('A2'));
      expect(offered(tester), ['A1', 'A2', 'B1', 'B2', 'C1', 'C2']);
      await _systemBack(tester);
      expect(_learning(tester), [('Japanese', 'A2')]);
      expect(_saveEnabled(tester), isFalse);

      // Choosing the level it has changes nothing either.
      await tapAndSettle(tester, find.text('A2'));
      await tapAndSettle(tester, _inSheet(l10n.languageLevelA2));
      expect(_saveEnabled(tester), isFalse);
    });
  });

  group('ordering and removing', () {
    testWidgets('move up and move down swap neighbours of the same list', (
      tester,
    ) async {
      await _openEditor(
        tester,
        _backend(
          spoken: [('es', 'native'), ('en', 'c1')],
          learning: [('ja', 'a2')],
        ),
      );
      await _tapTooltip(tester, l10n.languageMoveUp('English'));
      expect(_spoken(tester), [('English', 'C1'), ('Spanish', 'Native')]);
      expect(_learning(tester), [('Japanese', 'A2')]);
      expect(_saveEnabled(tester), isTrue);

      await _tapTooltip(tester, l10n.languageMoveDown('English'));
      expect(_spoken(tester), [('Spanish', 'Native'), ('English', 'C1')]);
      // Back where it was: nothing to save.
      expect(_saveEnabled(tester), isFalse);
    });

    testWidgets('the ends of a list, and a single entry, can\'t move further', (
      tester,
    ) async {
      await _openEditor(
        tester,
        _backend(
          spoken: [('es', 'native'), ('en', 'c1')],
          learning: [('ja', 'a2')],
        ),
      );
      expect(_enabled(tester, l10n.languageMoveUp('Spanish')), isFalse);
      expect(_enabled(tester, l10n.languageMoveDown('Spanish')), isTrue);
      expect(_enabled(tester, l10n.languageMoveUp('English')), isTrue);
      expect(_enabled(tester, l10n.languageMoveDown('English')), isFalse);
      expect(_enabled(tester, l10n.languageMoveUp('Japanese')), isFalse);
      expect(_enabled(tester, l10n.languageMoveDown('Japanese')), isFalse);
      for (final name in ['Spanish', 'English', 'Japanese']) {
        expect(_enabled(tester, l10n.languageRemove(name)), isTrue);
      }
    });

    testWidgets('remove takes the language out at once, and the picker '
        'offers it again', (tester) async {
      await _openEditor(
        tester,
        _backend(spoken: [('es', 'native'), ('en', 'c1')]),
      );
      await _tapTooltip(tester, l10n.languageRemove('Spanish'));
      expect(_dialog, findsNothing);
      expect(_spoken(tester), [('English', 'C1')]);

      await _tapTooltip(tester, l10n.languageRemove('English'));
      expect(find.byType(LanguageRow), findsNothing);
      expect(find.text(l10n.languagesAddButton), findsNWidgets(2));

      await tapAndSettle(tester, _add(1));
      expect(_inSheet('Spanish'), findsOneWidget);
      expect(_inSheet('English'), findsOneWidget);
    });
  });

  group('saving', () {
    testWidgets('sends both lists though only one was edited, and returns to '
        'the profile', (tester) async {
      final server = _backend(
        spoken: [('es', 'native'), ('en', 'c1')],
        learning: [('ja', 'a2')],
      );
      final app = await _openEditor(tester, server);
      await _tapTooltip(tester, l10n.languageRemove('Japanese'));
      _accept(server);
      await tapAndSettle(tester, _save);

      final put = _puts(server).single;
      expect(put.url.hasQuery, isFalse);
      expect(
        put.body,
        '{"spoken":[{"language":"es","level":"native"},'
        '{"language":"en","level":"c1"}],"learning":[]}',
      );
      // No question about unsaved changes on the way out.
      expect(_dialog, findsNothing);
      expect(_editor, findsNothing);
      expect(find.byType(ProfileScreen), findsOneWidget);
      expect(app.location(tester), '/profile');
    });

    testWidgets('an empty "I speak" is sent as an empty array', (tester) async {
      final server = _backend(
        spoken: [('es', 'native')],
        learning: [('ja', 'a2')],
      );
      await _openEditor(tester, server);
      await _tapTooltip(tester, l10n.languageRemove('Spanish'));
      _accept(server);
      await tapAndSettle(tester, _save);
      expect(
        _puts(server).single.body,
        '{"spoken":[],"learning":[{"language":"ja","level":"a2"}]}',
      );
      expect(_editor, findsNothing);
    });

    testWidgets('removing everything clears the selection', (tester) async {
      final server = _backend(
        spoken: [('es', 'native')],
        learning: [('ja', 'a2')],
      );
      await _openEditor(tester, server);
      await _tapTooltip(tester, l10n.languageRemove('Spanish'));
      await _tapTooltip(tester, l10n.languageRemove('Japanese'));
      _accept(server);
      await tapAndSettle(tester, _save);
      expect(_puts(server).single.body, '{"spoken":[],"learning":[]}');
      expect(_banners, findsNothing);
      expect(_editor, findsNothing);
    });

    testWidgets('sends each list in the order shown', (tester) async {
      final server = _backend(spoken: [('es', 'native'), ('en', 'c1')]);
      await _openEditor(tester, server);
      await _pick(tester, 0, 'Japanese', 'B2');
      await _tapTooltip(tester, l10n.languageMoveUp('Japanese'));
      await _tapTooltip(tester, l10n.languageMoveUp('Japanese'));
      expect(_spoken(tester), [
        ('Japanese', 'B2'),
        ('Spanish', 'Native'),
        ('English', 'C1'),
      ]);
      _accept(server);
      await tapAndSettle(tester, _save);
      expect(jsonDecode(_puts(server).single.body), {
        'spoken': [
          {'language': 'ja', 'level': 'b2'},
          {'language': 'es', 'level': 'native'},
          {'language': 'en', 'level': 'c1'},
        ],
        'learning': <Object?>[],
      });
    });

    testWidgets('applies no limit of its own: more languages than the server '
        'takes are sent as shown', (tester) async {
      final server = _backend(catalog: largeCatalogBody(8));
      await _openEditor(tester, server);
      for (var i = 0; i < 7; i++) {
        await _pick(tester, 0, 'Language 00$i', 'A1');
      }
      expect(_spoken(tester), hasLength(7));
      // Still offered.
      await tapAndSettle(tester, _add(0));
      expect(_inSheet('Language 007'), findsOneWidget);
      await _systemBack(tester);

      server.once(
        'PUT',
        ApiPaths.myLanguages,
        (_) => _validation([('spoken', 'too_many')]),
      );
      await tapAndSettle(tester, _save);
      final sent = jsonDecode(_puts(server).single.body) as Map;
      expect(sent['spoken'], hasLength(7));
      expect(find.text(l10n.errorLanguagesTooMany), findsOneWidget);
    });

    testWidgets('Save is there only for a change, and a second tap sends '
        'nothing more', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend(spoken: [('es', 'native')])
        ..once('PUT', ApiPaths.myLanguages, (_) => reply.future);
      await _openEditor(tester, server);

      // Nothing changed: the button does nothing.
      expect(_saveEnabled(tester), isFalse);
      await tester.ensureVisible(_save);
      await tester.tap(_save);
      await tester.pump();
      expect(_puts(server), isEmpty);

      await _pick(tester, 1, 'Japanese', 'A1');
      await tester.ensureVisible(_save);
      await tester.pumpAndSettle();
      await tester.tap(_save);
      await tester.pump();
      await tester.tap(_save, warnIfMissed: false);
      await tester.pump();
      expect(_puts(server), hasLength(1));

      // Everything waits for the answer.
      expect(
        tester
            .widget<PrimaryButton>(
              find.descendant(
                of: _editor,
                matching: find.byType(PrimaryButton),
              ),
            )
            .busy,
        isTrue,
      );
      expect(
        tester
            .widget<SecondaryButton>(
              find.descendant(
                of: _editor,
                matching: find.byType(SecondaryButton),
              ),
            )
            .onPressed,
        isNull,
      );
      for (final row in tester.widgetList<LanguageRow>(
        find.byType(LanguageRow),
      )) {
        expect(row.onLevelPressed, isNull);
        expect(row.onMoveUp, isNull);
        expect(row.onMoveDown, isNull);
        expect(row.onRemove, isNull);
      }
      await tester.tap(_add(0), warnIfMissed: false);
      await tester.pump();
      expect(_sheet, findsNothing);

      // Back, by the system and by the app bar, waits too.
      await tester.binding.handlePopRoute();
      await tester.pump();
      await tester.tap(find.byType(BackButton));
      await tester.pump();
      expect(_editor, findsOneWidget);
      expect(_dialog, findsNothing);

      reply.complete(
        jsonResponse(
          200,
          languagesBody(spoken: [('es', 'native')], learning: [('ja', 'a1')]),
        ),
      );
      await tester.pumpAndSettle();
      expect(_editor, findsNothing);
      expect(_puts(server), hasLength(1));
    });

    testWidgets('a save that gets no answer times out and can be sent again', (
      tester,
    ) async {
      final server = _backend(spoken: [('es', 'native')])
        ..once('PUT', ApiPaths.myLanguages, neverAnswers);
      await _openEditor(tester, server);
      await _pick(tester, 1, 'Japanese', 'A1');
      await tester.ensureVisible(_save);
      await tester.pumpAndSettle();
      await tester.tap(_save);
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();

      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(_learning(tester), [('Japanese', 'A1')]);
      expect(_saveEnabled(tester), isTrue);

      // The same complete selection again; the save is idempotent (029).
      _accept(server);
      await tapAndSettle(tester, _save);
      final puts = _puts(server);
      expect(puts, hasLength(2));
      expect(puts[1].body, puts[0].body);
      expect(_editor, findsNothing);
    });
  });

  group('a failed save', () {
    Future<FakeServer> edited(WidgetTester tester) async {
      final server = _backend(
        spoken: [('es', 'native')],
        learning: [('ja', 'a2')],
      );
      await _openEditor(tester, server);
      await _pick(tester, 0, 'English', 'C1');
      return server;
    }

    void kept(WidgetTester tester) {
      expect(_spoken(tester), [('Spanish', 'Native'), ('English', 'C1')]);
      expect(_learning(tester), [('Japanese', 'A2')]);
      expect(_saveEnabled(tester), isTrue);
      expect(_editor, findsOneWidget);
    }

    final codes = {
      'too_many': l10n.errorLanguagesTooMany,
      'unknown_language': l10n.errorLanguageUnknown,
      'invalid_level': l10n.errorLanguageLevelInvalid,
      'duplicate': l10n.errorLanguageDuplicate,
    };
    for (final field in ['spoken', 'learning']) {
      codes.forEach((code, message) {
        testWidgets('$code on $field is shown under that list', (tester) async {
          final server = await edited(tester);
          server.once(
            'PUT',
            ApiPaths.myLanguages,
            (_) => _validation([(field, code)]),
          );
          await tapAndSettle(tester, _save);

          expect(_banners, findsOneWidget);
          final error = find.descendant(
            of: _banners,
            matching: find.text(message),
          );
          expect(error, findsOneWidget);
          final dy = tester.getTopLeft(error).dy;
          final spokenDy = tester
              .getTopLeft(find.text(l10n.languagesSpokenHeading))
              .dy;
          final learningDy = tester
              .getTopLeft(find.text(l10n.languagesLearningHeading))
              .dy;
          expect(dy, greaterThan(field == 'spoken' ? spokenDy : learningDy));
          if (field == 'spoken') expect(dy, lessThan(learningDy));
          // Brought into view.
          expect(error.hitTestable(), findsOneWidget);
          kept(tester);
        });
      });
    }

    testWidgets('errors on both lists are each shown under their own', (
      tester,
    ) async {
      final server = await edited(tester);
      server.once(
        'PUT',
        ApiPaths.myLanguages,
        (_) => _validation([
          ('spoken', 'too_many'),
          ('learning', 'invalid_level'),
        ]),
      );
      await tapAndSettle(tester, _save);
      expect(_banners, findsNWidgets(2));
      final first = tester.getTopLeft(find.text(l10n.errorLanguagesTooMany)).dy;
      final heading = tester
          .getTopLeft(find.text(l10n.languagesLearningHeading))
          .dy;
      final second = tester
          .getTopLeft(find.text(l10n.errorLanguageLevelInvalid))
          .dy;
      expect(first, lessThan(heading));
      expect(heading, lessThan(second));
      kept(tester);
    });

    final others = <String, (http.Response Function(), String)>{
      'a 422 with no field this app knows': (
        () => _validation([('kind', 'weird')]),
        l10n.errorCheckInput,
      ),
      '429 with a wait': (
        () =>
            errorResponse(429, 'rate_limited', headers: {'retry-after': '10'}),
        'Too many attempts. Try again in 10 seconds.',
      ),
      '429 without a wait': (
        () => errorResponse(429, 'rate_limited'),
        l10n.errorRateLimitedNoWait,
      ),
      '503': (
        () => errorResponse(503, 'service_unavailable'),
        l10n.errorUnavailableNoWait,
      ),
      '500': (() => errorResponse(500, 'internal_error'), l10n.errorUnexpected),
      // What the app could send can't be this, but a proxy could answer it.
      '400': (
        () => errorResponse(400, 'invalid_request'),
        l10n.errorUnexpected,
      ),
      'a 200 that isn\'t a selection': (
        () => jsonResponse(200, {'spoken': null, 'learning': <Object?>[]}),
        l10n.errorUnexpected,
      ),
    };
    others.forEach((name, c) {
      final (respond, message) = c;
      testWidgets('$name: one message above the lists, which are kept', (
        tester,
      ) async {
        final server = await edited(tester);
        server.once('PUT', ApiPaths.myLanguages, (_) => respond());
        await tapAndSettle(tester, _save);

        expect(_banners, findsOneWidget);
        final error = find.descendant(
          of: _banners,
          matching: find.text(message),
        );
        expect(error, findsOneWidget);
        expect(
          tester.getTopLeft(error).dy,
          lessThan(
            tester.getTopLeft(find.text(l10n.languagesVisibilityNotice)).dy,
          ),
        );
        kept(tester);
      });
    });

    testWidgets('a network failure, then the retry sends the same selection', (
      tester,
    ) async {
      final server = await edited(tester);
      server.once('PUT', ApiPaths.myLanguages, networkFailure);
      await tapAndSettle(tester, _save);
      expect(find.text(l10n.errorNetwork), findsOneWidget);
      kept(tester);

      _accept(server);
      await tapAndSettle(tester, _save);
      final puts = _puts(server);
      expect(puts, hasLength(2));
      expect(puts[1].body, puts[0].body);
      expect(_editor, findsNothing);
    });

    testWidgets('any edit clears the messages under both lists and above', (
      tester,
    ) async {
      final server = await edited(tester);
      server.once(
        'PUT',
        ApiPaths.myLanguages,
        (_) => _validation([
          ('spoken', 'duplicate'),
          ('learning', 'duplicate'),
          ('kind', 'weird'),
        ]),
      );
      await tapAndSettle(tester, _save);
      expect(_banners, findsNWidgets(3));

      await _tapTooltip(tester, l10n.languageMoveUp('English'));
      expect(_banners, findsNothing);
    });

    testWidgets('the server\'s own text is never shown', (tester) async {
      final server = await edited(tester);
      server.once(
        'PUT',
        ApiPaths.myLanguages,
        (_) => http.Response(
          '{"error":{"code":"validation_failed","message":"SERVERTEXT es",'
          '"fields":[{"field":"spoken","code":"too_many",'
          '"message":"SERVERTEXT"}]}}',
          422,
          headers: {'content-type': 'application/json'},
        ),
      );
      await tapAndSettle(tester, _save);
      expect(find.text(l10n.errorLanguagesTooMany), findsOneWidget);
      expect(find.textContaining('SERVERTEXT'), findsNothing);
    });

    testWidgets('a session that ends during the save shows no error', (
      tester,
    ) async {
      final server = _backend(spoken: [('es', 'native')]);
      final app = await _openEditor(tester, server);
      await _pick(tester, 1, 'Japanese', 'A1');
      server
        ..once(
          'PUT',
          ApiPaths.myLanguages,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..once(
          'POST',
          ApiPaths.refresh,
          (_) => errorResponse(401, 'invalid_refresh_token'),
        );
      await tapAndSettle(tester, _save);

      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_editor, findsNothing);
      expect(_dialog, findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(tester.takeException(), isNull);
    });
  });

  group('leaving', () {
    final ways = <String, Future<void> Function(WidgetTester)>{
      'Cancel': (tester) => tapAndSettle(tester, _cancel),
      'the app bar\'s back': (tester) async {
        await tester.pageBack();
        await tester.pumpAndSettle();
      },
      'the system\'s back': _systemBack,
    };

    ways.forEach((name, leave) {
      testWidgets('$name with nothing changed leaves at once', (tester) async {
        final server = _backend(spoken: [('es', 'native')]);
        final app = await _openEditor(tester, server);
        await leave(tester);
        expect(_dialog, findsNothing);
        expect(_editor, findsNothing);
        expect(app.location(tester), '/profile');
        expect(_puts(server), isEmpty);
      });

      testWidgets('$name with a change asks, and "Keep editing" stays', (
        tester,
      ) async {
        final server = _backend(spoken: [('es', 'native')]);
        await _openEditor(tester, server);
        await _pick(tester, 1, 'Japanese', 'A1');
        await leave(tester);

        expect(_dialog, findsOneWidget);
        expect(find.text(l10n.languagesDiscardTitle), findsOneWidget);
        expect(find.text(l10n.languagesDiscardMessage), findsOneWidget);
        await tapAndSettle(tester, find.text(l10n.languagesDiscardKeep));
        expect(_dialog, findsNothing);
        expect(_editor, findsOneWidget);
        expect(_learning(tester), [('Japanese', 'A1')]);
        expect(_saveEnabled(tester), isTrue);
      });

      testWidgets('$name with a change, then "Discard", leaves without '
          'saving', (tester) async {
        final server = _backend(spoken: [('es', 'native')]);
        final app = await _openEditor(tester, server);
        await _pick(tester, 1, 'Japanese', 'A1');
        await leave(tester);
        await tapAndSettle(tester, find.text(l10n.languagesDiscardConfirm));

        expect(_dialog, findsNothing);
        expect(_editor, findsNothing);
        expect(app.location(tester), '/profile');
        expect(_puts(server), isEmpty);
        // The profile shows what is stored.
        expect(find.text('Japanese'), findsNothing);
        expect(find.text('Spanish'), findsOneWidget);
      });
    });

    final changes = <String, Future<void> Function(WidgetTester)>{
      'a new level': (tester) async {
        await tapAndSettle(tester, find.text('C1'));
        await tapAndSettle(tester, _inSheet(l10n.languageLevelC2));
      },
      'a move': (tester) => _tapTooltip(tester, l10n.languageMoveUp('English')),
      'a removal': (tester) =>
          _tapTooltip(tester, l10n.languageRemove('English')),
    };
    changes.forEach((name, change) {
      testWidgets('$name is a change to ask about', (tester) async {
        await _openEditor(
          tester,
          _backend(spoken: [('es', 'native'), ('en', 'c1')]),
        );
        await change(tester);
        await _systemBack(tester);
        expect(_dialog, findsOneWidget);
        expect(_editor, findsOneWidget);
      });
    });

    testWidgets('dismissing the question keeps editing', (tester) async {
      await _openEditor(tester, _backend(spoken: [('es', 'native')]));
      await _tapTooltip(tester, l10n.languageRemove('Spanish'));
      await _systemBack(tester);
      expect(_dialog, findsOneWidget);
      // Back again closes only the dialog.
      await _systemBack(tester);
      expect(_dialog, findsNothing);
      expect(_editor, findsOneWidget);
      expect(_spoken(tester), isEmpty);

      await _systemBack(tester);
      await tester.tapAt(const Offset(5, 5));
      await tester.pumpAndSettle();
      expect(_dialog, findsNothing);
      expect(_editor, findsOneWidget);
    });

    testWidgets('a failed save still counts as unsaved', (tester) async {
      final server = _backend(spoken: [('es', 'native')]);
      await _openEditor(tester, server);
      await _tapTooltip(tester, l10n.languageRemove('Spanish'));
      server.once('PUT', ApiPaths.myLanguages, networkFailure);
      await tapAndSettle(tester, _save);
      await _systemBack(tester);
      expect(_dialog, findsOneWidget);
    });
  });

  group('the session ending with unsaved changes', () {
    final states = <String, Future<void> Function(WidgetTester)>{
      'nothing else open': (tester) async {},
      'the picker open': (tester) => tapAndSettle(tester, _add(0)),
      'the level choice open': (tester) async {
        await tapAndSettle(tester, _add(0));
        await tapAndSettle(tester, _inSheet('English'));
      },
      'the discard question open': _systemBack,
    };
    states.forEach((name, open) {
      testWidgets('with $name: log in, and nothing of the editor', (
        tester,
      ) async {
        final server = _backend(spoken: [('es', 'native')]);
        final app = await _openEditor(tester, server);
        await _pick(tester, 1, 'Japanese', 'A1');
        await open(tester);

        await app.session.logout();
        await tester.pumpAndSettle();

        expect(app.location(tester), '/login');
        expect(find.byType(LoginScreen), findsOneWidget);
        expect(_editor, findsNothing);
        expect(_sheet, findsNothing);
        expect(_dialog, findsNothing);
        expect(find.text(l10n.languagesDiscardTitle), findsNothing);
        expect(find.text(l10n.languagePickerTitle), findsNothing);
        expect(tester.takeException(), isNull);
        expect(_puts(server), isEmpty);
      });
    });
  });

  testWidgets('the location never holds a language', (tester) async {
    final server = _backend(spoken: [('es', 'native'), ('en', 'c1')]);
    final app = await _openEditor(tester, server);
    final seen = <String>{app.location(tester)};

    await tapAndSettle(tester, _add(1));
    seen.add(app.location(tester));
    await tapAndSettle(tester, _inSheet('Japanese'));
    seen.add(app.location(tester));
    await tapAndSettle(tester, _inSheet(l10n.languageLevelA2));
    await _tapTooltip(tester, l10n.languageMoveUp('English'));
    await _tapTooltip(tester, l10n.languageRemove('Spanish'));
    seen.add(app.location(tester));
    await _systemBack(tester);
    seen.add(app.location(tester));
    await tapAndSettle(tester, find.text(l10n.languagesDiscardKeep));
    server.once('PUT', ApiPaths.myLanguages, networkFailure);
    await tapAndSettle(tester, _save);
    seen.add(app.location(tester));

    expect(seen, {'/profile/languages'});
  });

  testWidgets('large text on a small screen: every control of every row can '
      'be reached and used', (tester) async {
    final server = _backend(
      spoken: [('es', 'native'), ('en', 'c1'), ('ja', 'b1')],
    );
    await _openEditor(tester, server, size: const Size(320, 480), textScale: 2);
    expect(tester.takeException(), isNull);

    await _tapTooltip(tester, l10n.languageMoveDown('Spanish'));
    await _tapTooltip(tester, l10n.languageMoveUp('Japanese'));
    await _tapTooltip(tester, l10n.languageRemove('English'));
    expect(_spoken(tester), [('Japanese', 'B1'), ('Spanish', 'Native')]);
    await _pick(tester, 1, 'English', 'A1');
    expect(tester.takeException(), isNull);

    await tapAndSettle(tester, _cancel);
    expect(_dialog, findsOneWidget);
    expect(tester.takeException(), isNull);
    await tapAndSettle(tester, find.text(l10n.languagesDiscardKeep));

    _accept(server);
    await tapAndSettle(tester, _save);
    expect(tester.takeException(), isNull);
    expect(_editor, findsNothing);
    expect(jsonDecode(_puts(server).single.body), {
      'spoken': [
        {'language': 'ja', 'level': 'b1'},
        {'language': 'es', 'level': 'native'},
      ],
      'learning': [
        {'language': 'en', 'level': 'a1'},
      ],
    });
  });
}

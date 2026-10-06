import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/widgets/language_row.dart';

import 'harness.dart';

Widget _row({
  String name = 'Spanish',
  String endonym = 'Español',
  String level = 'B2',
  VoidCallback? onLevelPressed,
  VoidCallback? onRemove,
}) => LanguageRow(
  name: name,
  endonym: endonym,
  level: level,
  levelSemanticLabel: '$name, level $level',
  removeLabel: 'Remove $name',
  onLevelPressed: onLevelPressed,
  onRemove: onRemove,
);

void main() {
  testWidgets('shows the name, the endonym and the level', (tester) async {
    await pumpUi(tester, _row(onLevelPressed: () {}, onRemove: () {}));
    expect(find.text('Spanish'), findsOneWidget);
    expect(find.text('Español'), findsOneWidget);
    expect(find.text('B2'), findsOneWidget);
  });

  testWidgets('an endonym equal to the name is shown once', (tester) async {
    await pumpUi(
      tester,
      _row(
        name: 'English',
        endonym: 'English',
        onLevelPressed: () {},
        onRemove: () {},
      ),
    );
    expect(find.text('English'), findsOneWidget);
  });

  testWidgets('each button calls its own callback', (tester) async {
    var level = 0;
    var removed = 0;
    await pumpUi(
      tester,
      _row(onLevelPressed: () => level++, onRemove: () => removed++),
    );
    await tester.tap(find.text('B2'));
    expect((level, removed), (1, 0));
    await tester.tap(find.byTooltip('Remove Spanish'));
    expect((level, removed), (1, 1));
  });

  for (final brightness in Brightness.values) {
    testWidgets('labelled 48 dp targets with readable text ($brightness)', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      await pumpUi(
        tester,
        _row(onLevelPressed: () {}, onRemove: () {}),
        brightness: brightness,
      );
      expect(
        tester.getSize(find.byType(TextButton)).height,
        greaterThanOrEqualTo(48),
      );
      final remove = tester.getSize(find.byType(IconButton));
      expect(remove.width, greaterThanOrEqualTo(48));
      expect(remove.height, greaterThanOrEqualTo(48));
      // The level alone doesn't say whose it is.
      expect(
        tester.getSemantics(find.byType(TextButton)),
        isSemantics(
          label: 'Spanish, level B2',
          isButton: true,
          hasEnabledState: true,
          isEnabled: true,
          hasTapAction: true,
          isFocusable: true,
        ),
      );
      expect(find.bySemanticsLabel('Spanish\nEspañol'), findsOneWidget);
      await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
      await expectLater(tester, meetsGuideline(labeledTapTargetGuideline));
      await expectLater(tester, meetsGuideline(textContrastGuideline));
      handle.dispose();
    });
  }

  testWidgets('null callbacks disable the buttons', (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, _row());
    expect(tester.widget<TextButton>(find.byType(TextButton)).enabled, isFalse);
    expect(
      tester.widget<IconButton>(find.byType(IconButton)).onPressed,
      isNull,
    );
    expect(
      tester.getSemantics(find.byType(TextButton)),
      isSemantics(
        label: 'Spanish, level B2',
        isButton: true,
        hasEnabledState: true,
        isEnabled: false,
      ),
    );
    handle.dispose();
  });

  testWidgets('a long name wraps at large text on small screens', (
    tester,
  ) async {
    await pumpUi(
      tester,
      _row(
        name: 'Scottish Gaelic',
        endonym: 'Gàidhlig',
        level: 'Native',
        onLevelPressed: () {},
        onRemove: () {},
      ),
      size: const Size(320, 480),
      textScale: 2,
    );
    expect(tester.takeException(), isNull);
    // The buttons moved below the name, which keeps the full width.
    expect(
      tester.getTopLeft(find.byType(TextButton)).dy,
      greaterThanOrEqualTo(tester.getBottomLeft(find.text('Gàidhlig')).dy),
    );
    expect(tester.getSize(find.byType(IconButton)).height, 48 * 1.0);
  });

  testWidgets('at normal sizes the buttons sit on the right of the name', (
    tester,
  ) async {
    await pumpUi(
      tester,
      SizedBox(
        width: 328,
        child: _row(onLevelPressed: () {}, onRemove: () {}),
      ),
      size: const Size(360, 640),
    );
    final row = tester.getRect(find.byType(LanguageRow));
    expect(tester.getTopLeft(find.text('Spanish')).dx, row.left);
    expect(tester.getTopRight(find.byType(IconButton)).dx, row.right);
    expect(
      tester.getCenter(find.byType(IconButton)).dy,
      moreOrLessEquals(row.center.dy),
    );
  });
}

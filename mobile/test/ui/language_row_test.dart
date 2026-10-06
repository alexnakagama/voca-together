import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/widgets/language_row.dart';

import 'harness.dart';

Widget _row({
  String name = 'Spanish',
  String endonym = 'Español',
  String level = 'B2',
  VoidCallback? onLevelPressed,
  VoidCallback? onMoveUp,
  VoidCallback? onMoveDown,
  VoidCallback? onRemove,
}) => LanguageRow(
  name: name,
  endonym: endonym,
  level: level,
  levelSemanticLabel: '$name, level $level',
  moveUpLabel: 'Move $name up',
  moveDownLabel: 'Move $name down',
  removeLabel: 'Remove $name',
  onLevelPressed: onLevelPressed,
  onMoveUp: onMoveUp,
  onMoveDown: onMoveDown,
  onRemove: onRemove,
);

/// A row with every button enabled.
Widget _enabled({
  String name = 'Spanish',
  String endonym = 'Español',
  String level = 'B2',
}) => _row(
  name: name,
  endonym: endonym,
  level: level,
  onLevelPressed: () {},
  onMoveUp: () {},
  onMoveDown: () {},
  onRemove: () {},
);

/// The icon button whose tooltip is [tooltip], at its full tap-target size.
Finder _button(String tooltip) => find.ancestor(
  of: find.byTooltip(tooltip),
  matching: find.byType(IconButton),
);

Finder get _up => _button('Move Spanish up');
Finder get _down => _button('Move Spanish down');
Finder get _remove => _button('Remove Spanish');

void main() {
  testWidgets('shows the name, the endonym and the level', (tester) async {
    await pumpUi(tester, _enabled());
    expect(find.text('Spanish'), findsOneWidget);
    expect(find.text('Español'), findsOneWidget);
    expect(find.text('B2'), findsOneWidget);
  });

  testWidgets('an endonym equal to the name is shown once', (tester) async {
    await pumpUi(tester, _enabled(name: 'English', endonym: 'English'));
    expect(find.text('English'), findsOneWidget);
  });

  testWidgets('each button calls its own callback', (tester) async {
    final calls = <String>[];
    await pumpUi(
      tester,
      _row(
        onLevelPressed: () => calls.add('level'),
        onMoveUp: () => calls.add('up'),
        onMoveDown: () => calls.add('down'),
        onRemove: () => calls.add('remove'),
      ),
    );
    await tester.tap(find.text('B2'));
    await tester.tap(_up);
    await tester.tap(_down);
    await tester.tap(_remove);
    expect(calls, ['level', 'up', 'down', 'remove']);
  });

  for (final brightness in Brightness.values) {
    testWidgets('labelled 48 dp targets with readable text ($brightness)', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      await pumpUi(tester, _enabled(), brightness: brightness);
      expect(
        tester.getSize(find.byType(TextButton)).height,
        greaterThanOrEqualTo(48),
      );
      for (final button in [_up, _down, _remove]) {
        final size = tester.getSize(button);
        expect(size.width, greaterThanOrEqualTo(48));
        expect(size.height, greaterThanOrEqualTo(48));
      }
      // Each icon button says what it does, and to which language.
      for (final label in [
        'Move Spanish up',
        'Move Spanish down',
        'Remove Spanish',
      ]) {
        expect(tester.getSemantics(_button(label)).tooltip, label);
      }
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
    for (final button in tester.widgetList<IconButton>(
      find.byType(IconButton),
    )) {
      expect(button.onPressed, isNull);
    }
    expect(find.byType(IconButton), findsNWidgets(3));
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
    final calls = <String>[];
    await pumpUi(
      tester,
      _row(
        name: 'Scottish Gaelic',
        endonym: 'Gàidhlig',
        level: 'Native',
        onLevelPressed: () => calls.add('level'),
        onMoveUp: () => calls.add('up'),
        onMoveDown: () => calls.add('down'),
        onRemove: () => calls.add('remove'),
      ),
      size: const Size(320, 480),
      textScale: 2,
    );
    expect(tester.takeException(), isNull);
    // Nothing is pushed off the screen, and every button takes its tap.
    final row = tester.getRect(find.byType(LanguageRow));
    for (final button in [
      find.byType(TextButton),
      ...[
        'Move Scottish Gaelic up',
        'Move Scottish Gaelic down',
      ].map(find.byTooltip),
      find.byTooltip('Remove Scottish Gaelic'),
    ]) {
      final rect = tester.getRect(button);
      expect(rect.left, greaterThanOrEqualTo(row.left));
      expect(rect.right, lessThanOrEqualTo(row.right));
      await tester.tap(button);
    }
    expect(calls, ['level', 'up', 'down', 'remove']);
    // The buttons moved below the name, which keeps the full width.
    expect(
      tester.getTopLeft(find.byType(TextButton)).dy,
      greaterThanOrEqualTo(tester.getBottomLeft(find.text('Gàidhlig')).dy),
    );
    expect(tester.getSize(_button('Remove Scottish Gaelic')).height, 48 * 1.0);
  });

  testWidgets('with room, the buttons sit on the right of the name', (
    tester,
  ) async {
    await pumpUi(
      tester,
      SizedBox(width: 448, child: _enabled()),
      size: const Size(480, 640),
    );
    final row = tester.getRect(find.byType(LanguageRow));
    expect(tester.getTopLeft(find.text('Spanish')).dx, row.left);
    expect(tester.getTopRight(_remove).dx, row.right);
    for (final button in [_up, _down, _remove]) {
      expect(tester.getCenter(button).dy, moreOrLessEquals(row.center.dy));
    }
    // In the order they read: level, up, down, remove.
    expect(
      tester.getCenter(find.byType(TextButton)).dx,
      lessThan(tester.getCenter(_up).dx),
    );
    expect(tester.getCenter(_up).dx, lessThan(tester.getCenter(_down).dx));
    expect(tester.getCenter(_down).dx, lessThan(tester.getCenter(_remove).dx));
  });
}

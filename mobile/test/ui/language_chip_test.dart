import 'package:flutter/material.dart';
import 'package:flutter/semantics.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/widgets/language_chip.dart';

import 'harness.dart';

void main() {
  testWidgets('shows the name and the level, read as one item', (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, const LanguageChip(name: 'Spanish', level: 'B2'));
    expect(find.text('Spanish'), findsOneWidget);
    expect(find.text('B2'), findsOneWidget);
    expect(
      tester.getSemantics(find.byType(LanguageChip)),
      isSemantics(label: 'Spanish\nB2'),
    );
    handle.dispose();
  });

  testWidgets('is not a control', (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, const LanguageChip(name: 'Spanish', level: 'B2'));
    final data = tester
        .getSemantics(find.byType(LanguageChip))
        .getSemanticsData();
    expect(data.flagsCollection.isButton, isFalse);
    expect(data.hasAction(SemanticsAction.tap), isFalse);
    handle.dispose();
  });

  for (final brightness in Brightness.values) {
    testWidgets('readable secondary-container colors ($brightness)', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      await pumpUi(
        tester,
        const LanguageChip(name: 'Spanish', level: 'Native'),
        brightness: brightness,
      );
      final scheme = Theme.of(tester.element(find.byType(LanguageChip)))
          .colorScheme;
      final box = tester.widget<DecoratedBox>(
        find.descendant(
          of: find.byType(LanguageChip),
          matching: find.byType(DecoratedBox),
        ),
      );
      expect(
        (box.decoration as BoxDecoration).color,
        scheme.secondaryContainer,
      );
      for (final text in ['Spanish', 'Native']) {
        expect(
          tester.widget<Text>(find.text(text)).style?.color,
          scheme.onSecondaryContainer,
        );
      }
      await expectLater(tester, meetsGuideline(textContrastGuideline));
      handle.dispose();
    });
  }

  testWidgets('the level differs from the name by weight, not only color', (
    tester,
  ) async {
    await pumpUi(tester, const LanguageChip(name: 'Spanish', level: 'B2'));
    final name = tester.widget<Text>(find.text('Spanish')).style!;
    final level = tester.widget<Text>(find.text('B2')).style!;
    expect(level.color, name.color);
    expect(level.fontWeight, isNot(name.fontWeight));
  });

  testWidgets('is only as wide as its text', (tester) async {
    await pumpUi(
      tester,
      const Wrap(
        children: [LanguageChip(name: 'Thai', level: 'A1')],
      ),
    );
    expect(tester.getSize(find.byType(LanguageChip)).width, lessThan(160));
  });

  testWidgets('a long name wraps at large text on small screens', (
    tester,
  ) async {
    await pumpUi(
      tester,
      const Wrap(
        children: [LanguageChip(name: 'Norwegian Bokmål', level: 'Native')],
      ),
      size: const Size(320, 480),
      textScale: 2,
    );
    expect(tester.takeException(), isNull);
    expect(
      tester.getSize(find.byType(LanguageChip)).width,
      lessThanOrEqualTo(320),
    );
  });
}

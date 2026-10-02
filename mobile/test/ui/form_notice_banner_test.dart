import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/widgets/form_notice_banner.dart';

import 'harness.dart';

void main() {
  testWidgets('shows the message with an info icon, not only color', (
    tester,
  ) async {
    await pumpUi(tester, const FormNoticeBanner(message: 'We sent a link.'));
    expect(find.text('We sent a link.'), findsOneWidget);
    final icon = tester.widget<Icon>(find.byType(Icon));
    expect(icon.icon, Icons.info_outline);
    expect(icon.semanticLabel, 'Notice');
  });

  for (final brightness in Brightness.values) {
    testWidgets('readable secondary-container colors ($brightness)', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      await pumpUi(
        tester,
        const FormNoticeBanner(message: 'We sent a link.'),
        brightness: brightness,
      );
      final scheme = Theme.of(tester.element(find.byType(FormNoticeBanner)))
          .colorScheme;
      final box = tester.widget<DecoratedBox>(
        find.descendant(
          of: find.byType(FormNoticeBanner),
          matching: find.byType(DecoratedBox),
        ),
      );
      expect(
        (box.decoration as BoxDecoration).color,
        scheme.secondaryContainer,
      );
      expect(
        contrastRatio(scheme.onSecondaryContainer, scheme.secondaryContainer),
        greaterThanOrEqualTo(4.5),
      );
      await expectLater(tester, meetsGuideline(textContrastGuideline));
      handle.dispose();
    });
  }

  testWidgets('is announced as a live region', (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, const FormNoticeBanner(message: 'We sent a link.'));
    expect(
      tester.getSemantics(find.byType(FormNoticeBanner)),
      isSemantics(isLiveRegion: true, label: 'Notice\nWe sent a link.'),
    );
    handle.dispose();
  });

  testWidgets('long messages wrap at large text on small screens', (
    tester,
  ) async {
    await pumpUi(
      tester,
      const FormNoticeBanner(
        message:
            'If ana.long.address@example.com is waiting to be verified, '
            'we’ve sent a new link. It can take a few minutes to arrive.',
      ),
      size: const Size(320, 480),
      textScale: 2,
    );
    expect(tester.takeException(), isNull);
  });
}

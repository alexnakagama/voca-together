import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';

import 'harness.dart';

void main() {
  testWidgets('shows the message with an error icon, not only color', (
    tester,
  ) async {
    await pumpUi(
      tester,
      const FormErrorBanner(message: 'Incorrect email or password.'),
    );
    expect(find.text('Incorrect email or password.'), findsOneWidget);
    final icon = tester.widget<Icon>(find.byType(Icon));
    expect(icon.icon, Icons.error_outline);
    expect(icon.semanticLabel, 'Error');
  });

  testWidgets('uses the error container colors', (tester) async {
    await pumpUi(tester, const FormErrorBanner(message: 'Something failed.'));
    final scheme = Theme.of(tester.element(find.byType(FormErrorBanner)))
        .colorScheme;

    final box = tester.widget<DecoratedBox>(
      find.descendant(
        of: find.byType(FormErrorBanner),
        matching: find.byType(DecoratedBox),
      ),
    );
    expect((box.decoration as BoxDecoration).color, scheme.errorContainer);
    final text = tester.widget<Text>(find.text('Something failed.'));
    expect(text.style!.color, scheme.onErrorContainer);
  });

  testWidgets('is announced as a live region', (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, const FormErrorBanner(message: 'Something failed.'));
    expect(
      tester.getSemantics(find.byType(FormErrorBanner)),
      isSemantics(isLiveRegion: true, label: 'Error\nSomething failed.'),
    );
    await expectLater(tester, meetsGuideline(textContrastGuideline));
    handle.dispose();
  });

  testWidgets('long messages wrap at large text on small screens', (
    tester,
  ) async {
    await pumpUi(
      tester,
      const FormErrorBanner(
        message:
            "We couldn't reach VocaTogether. Check your connection and try "
            'again in a moment.',
      ),
      size: const Size(320, 480),
      textScale: 2,
    );
    expect(tester.takeException(), isNull);
  });
}

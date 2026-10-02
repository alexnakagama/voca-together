import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/widgets/secondary_button.dart';

import 'harness.dart';

void main() {
  testWidgets('calls onPressed and meets the tap-target guideline', (
    tester,
  ) async {
    final handle = tester.ensureSemantics();
    var calls = 0;
    await pumpUi(
      tester,
      SecondaryButton(label: 'Log out', onPressed: () => calls++),
    );
    await tester.tap(find.text('Log out'));
    expect(calls, 1);
    expect(tester.getSize(find.byType(OutlinedButton)).height, 48);
    await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
    await expectLater(tester, meetsGuideline(textContrastGuideline));
    handle.dispose();
  });

  testWidgets('busy: a spinner, no taps, same size, label still announced', (
    tester,
  ) async {
    final handle = tester.ensureSemantics();
    var calls = 0;
    void onPressed() => calls++;
    await pumpUi(
      tester,
      SecondaryButton(label: 'Log out', onPressed: onPressed),
    );
    final idle = tester.getSize(find.byType(OutlinedButton));

    await pumpUi(
      tester,
      SecondaryButton(label: 'Log out', onPressed: onPressed, busy: true),
    );
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    expect(tester.getSize(find.byType(OutlinedButton)), idle);
    await tester.tap(find.byType(OutlinedButton), warnIfMissed: false);
    expect(calls, 0);
    expect(
      tester.getSemantics(find.byType(OutlinedButton)),
      isSemantics(
        label: 'Log out',
        isButton: true,
        hasEnabledState: true,
        isEnabled: false,
      ),
    );
    handle.dispose();
  });

  testWidgets('long labels wrap at large text on small screens', (
    tester,
  ) async {
    await pumpUi(
      tester,
      SecondaryButton(label: 'Send a new verification email', onPressed: () {}),
      size: const Size(320, 480),
      textScale: 2,
    );
    expect(tester.takeException(), isNull);
  });
}

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/widgets/primary_button.dart';

import 'harness.dart';

void main() {
  testWidgets('an enabled button calls onPressed once per tap', (tester) async {
    var calls = 0;
    await pumpUi(
      tester,
      PrimaryButton(label: 'Log in', onPressed: () => calls++),
    );

    await tester.tap(find.text('Log in'));
    await tester.pump();
    expect(calls, 1);
    expect(find.byType(CircularProgressIndicator), findsNothing);
  });

  testWidgets('a disabled button ignores taps and says so', (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, const PrimaryButton(label: 'Log in', onPressed: null));

    await tester.tap(find.text('Log in'), warnIfMissed: false);
    expect(
      tester.getSemantics(find.byType(FilledButton)),
      isSemantics(
        label: 'Log in',
        isButton: true,
        hasEnabledState: true,
        isEnabled: false,
      ),
    );
    handle.dispose();
  });

  testWidgets('a busy button shows progress, ignores taps and keeps its size', (
    tester,
  ) async {
    final handle = tester.ensureSemantics();
    var calls = 0;
    void onPressed() => calls++;

    await pumpUi(tester, PrimaryButton(label: 'Log in', onPressed: onPressed));
    final idleSize = tester.getSize(find.byType(FilledButton));

    await pumpUi(
      tester,
      PrimaryButton(label: 'Log in', onPressed: onPressed, busy: true),
    );
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    expect(tester.getSize(find.byType(FilledButton)), idleSize);

    await tester.tap(find.byType(FilledButton), warnIfMissed: false);
    await tester.pump();
    expect(calls, 0);

    // Still announced by its label, as not enabled.
    expect(
      tester.getSemantics(find.byType(FilledButton)),
      isSemantics(
        label: 'Log in',
        isButton: true,
        hasEnabledState: true,
        isEnabled: false,
      ),
    );
    handle.dispose();
  });

  testWidgets('a busy button keeps its enabled colors', (tester) async {
    await pumpUi(
      tester,
      const PrimaryButton(label: 'Log in', onPressed: null, busy: true),
    );
    final material = tester.widget<Material>(
      find.descendant(
        of: find.byType(FilledButton),
        matching: find.byType(Material),
      ),
    );
    final context = tester.element(find.byType(FilledButton));
    expect(material.color, Theme.of(context).colorScheme.primary);
  });

  testWidgets('a second tap in the same frame is ignored', (tester) async {
    var calls = 0;
    await pumpUi(
      tester,
      PrimaryButton(label: 'Log in', onPressed: () => calls++),
    );

    await tester.tap(find.text('Log in'));
    await tester.tap(find.text('Log in'));
    expect(calls, 1);

    // The latch releases on the next frame, so a parent that never sets busy
    // can't leave the button stuck.
    await tester.pump();
    await tester.tap(find.text('Log in'));
    expect(calls, 2);
  });

  testWidgets('a parent that sets busy blocks repeated taps', (tester) async {
    await pumpUi(tester, const _Submitter());

    for (var i = 0; i < 3; i++) {
      await tester.tap(find.byType(FilledButton), warnIfMissed: false);
      await tester.pump();
    }
    final state = tester.state<_SubmitterState>(find.byType(_Submitter));
    expect(state.submissions, 1);
  });

  for (final brightness in Brightness.values) {
    testWidgets('meets accessibility guidelines ($brightness)', (tester) async {
      final handle = tester.ensureSemantics();
      await pumpUi(
        tester,
        PrimaryButton(label: 'Log in', onPressed: () {}),
        brightness: brightness,
      );
      await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
      await expectLater(tester, meetsGuideline(labeledTapTargetGuideline));
      await expectLater(tester, meetsGuideline(textContrastGuideline));
      handle.dispose();
    });
  }

  testWidgets('wraps instead of overflowing at large text', (tester) async {
    await pumpUi(
      tester,
      PrimaryButton(label: 'Send me a new verification link', onPressed: () {}),
      size: const Size(320, 480),
      textScale: 2,
    );
    expect(tester.takeException(), isNull);
  });
}

/// A parent that marks itself busy when submitting, as screens will.
class _Submitter extends StatefulWidget {
  const _Submitter();

  @override
  State<_Submitter> createState() => _SubmitterState();
}

class _SubmitterState extends State<_Submitter> {
  int submissions = 0;
  bool busy = false;

  @override
  Widget build(BuildContext context) {
    return PrimaryButton(
      label: 'Log in',
      busy: busy,
      onPressed: () => setState(() {
        submissions++;
        busy = true;
      }),
    );
  }
}

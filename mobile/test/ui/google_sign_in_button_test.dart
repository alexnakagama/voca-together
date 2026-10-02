import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/widgets/google_sign_in_button.dart';

import 'harness.dart';

Finder get _logo => find.descendant(
  of: find.byType(GoogleSignInButton),
  matching: find.byType(Ink),
);

String _logoAsset(WidgetTester tester) {
  final ink = tester.widget<Ink>(_logo);
  final decoration = ink.decoration! as BoxDecoration;
  return (decoration.image!.image as AssetImage).assetName;
}

ButtonStyle _style(WidgetTester tester) =>
    tester.widget<OutlinedButton>(find.byType(OutlinedButton)).style!;

void main() {
  testWidgets('renders the approved text and the official logo', (
    tester,
  ) async {
    await pumpUi(tester, GoogleSignInButton(onPressed: () {}));

    expect(find.text('Continue with Google'), findsOneWidget);
    expect(_logoAsset(tester), 'assets/google/g_logo_light.png');
    expect(tester.getSize(_logo), const Size.square(20));
  });

  testWidgets("uses Google's light palette in the light theme", (tester) async {
    await pumpUi(tester, GoogleSignInButton(onPressed: () {}));
    final style = _style(tester);
    expect(style.backgroundColor!.resolve({}), const Color(0xFFFFFFFF));
    expect(style.foregroundColor!.resolve({}), const Color(0xFF1F1F1F));
    expect(style.side!.resolve({})!.color, const Color(0xFF747775));
  });

  testWidgets("uses Google's dark palette and logo in the dark theme", (
    tester,
  ) async {
    await pumpUi(
      tester,
      GoogleSignInButton(onPressed: () {}),
      brightness: Brightness.dark,
    );
    final style = _style(tester);
    expect(style.backgroundColor!.resolve({}), const Color(0xFF131314));
    expect(style.foregroundColor!.resolve({}), const Color(0xFFE3E3E3));
    expect(style.side!.resolve({})!.color, const Color(0xFF8E918F));
    expect(_logoAsset(tester), 'assets/google/g_logo_dark.png');
  });

  test('Google text colors are readable on their fills', () {
    expect(
      contrastRatio(const Color(0xFF1F1F1F), const Color(0xFFFFFFFF)),
      greaterThanOrEqualTo(4.5),
    );
    expect(
      contrastRatio(const Color(0xFFE3E3E3), const Color(0xFF131314)),
      greaterThanOrEqualTo(4.5),
    );
  });

  testWidgets('a tap calls onPressed', (tester) async {
    var calls = 0;
    await pumpUi(tester, GoogleSignInButton(onPressed: () => calls++));
    await tester.tap(find.byType(GoogleSignInButton));
    expect(calls, 1);
  });

  testWidgets('disabled: faded, same colors, no taps', (tester) async {
    await pumpUi(tester, const GoogleSignInButton(onPressed: null));

    final opacity = tester.widget<Opacity>(
      find.ancestor(
        of: find.byType(OutlinedButton),
        matching: find.byType(Opacity),
      ),
    );
    expect(opacity.opacity, 0.38);
    final style = _style(tester);
    expect(
      style.backgroundColor!.resolve({WidgetState.disabled}),
      const Color(0xFFFFFFFF),
    );
    expect(
      style.foregroundColor!.resolve({WidgetState.disabled}),
      const Color(0xFF1F1F1F),
    );
    await tester.tap(find.byType(GoogleSignInButton), warnIfMissed: false);
  });

  testWidgets('the logo is decorative; the button is labeled by its text', (
    tester,
  ) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, GoogleSignInButton(onPressed: () {}));
    expect(
      tester.getSemantics(find.byType(OutlinedButton)),
      isSemantics(
        label: 'Continue with Google',
        isButton: true,
        hasEnabledState: true,
        isEnabled: true,
        hasTapAction: true,
        isFocusable: true,
      ),
    );
    await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
    await expectLater(tester, meetsGuideline(labeledTapTargetGuideline));
    handle.dispose();
  });

  testWidgets('is as tall as the primary button', (tester) async {
    await pumpUi(tester, GoogleSignInButton(onPressed: () {}));
    expect(tester.getSize(find.byType(OutlinedButton)).height, 48);
  });

  testWidgets('wraps on a narrow screen with large text, logo unscaled', (
    tester,
  ) async {
    await pumpUi(
      tester,
      GoogleSignInButton(onPressed: () {}),
      size: const Size(320, 480),
      textScale: 2,
    );
    expect(tester.takeException(), isNull);
    expect(tester.getSize(_logo), const Size.square(20));
  });
}

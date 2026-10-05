import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/widgets/app_text_field.dart';

import 'harness.dart';

TextField _field(WidgetTester tester) => tester.widget(find.byType(TextField));

void main() {
  testWidgets('email field', (tester) async {
    await pumpUi(tester, const AppTextField.email());

    final field = _field(tester);
    expect(field.keyboardType, TextInputType.emailAddress);
    expect(field.autofillHints, [AutofillHints.email]);
    expect(field.autocorrect, isFalse);
    expect(field.obscureText, isFalse);
    expect(find.text('Email'), findsOneWidget);
  });

  testWidgets('password field is obscured, with no text assistance', (
    tester,
  ) async {
    await pumpUi(tester, const AppTextField.password());

    final field = _field(tester);
    expect(field.obscureText, isTrue);
    expect(field.autocorrect, isFalse);
    expect(field.enableSuggestions, isFalse);
    expect(field.enableIMEPersonalizedLearning, isFalse);
    expect(field.smartDashesType, SmartDashesType.disabled);
    expect(field.smartQuotesType, SmartQuotesType.disabled);
    expect(field.autofillHints, [AutofillHints.password]);
    expect(find.text('Password'), findsOneWidget);
  });

  testWidgets('new password field asks for a new-password autofill', (
    tester,
  ) async {
    await pumpUi(tester, const AppTextField.password(newPassword: true));
    expect(_field(tester).autofillHints, [AutofillHints.newPassword]);
  });

  testWidgets('the toggle reveals and hides the password', (tester) async {
    await pumpUi(tester, const AppTextField.password());

    expect(find.byTooltip('Show password'), findsOneWidget);
    await tester.tap(find.byTooltip('Show password'));
    await tester.pump();
    expect(_field(tester).obscureText, isFalse);
    expect(find.byTooltip('Hide password'), findsOneWidget);

    await tester.tap(find.byTooltip('Hide password'));
    await tester.pump();
    expect(_field(tester).obscureText, isTrue);
  });

  testWidgets('typed text reaches the controller untouched', (tester) async {
    final controller = TextEditingController();
    addTearDown(controller.dispose);
    await pumpUi(tester, AppTextField.password(controller: controller));

    await tester.enterText(find.byType(TextField), '  pass word  ');
    expect(controller.text, '  pass word  ');
  });

  testWidgets('errorText is shown', (tester) async {
    await pumpUi(
      tester,
      const AppTextField.email(errorText: 'Enter a valid email address.'),
    );
    expect(find.text('Enter a valid email address.'), findsOneWidget);
  });

  testWidgets('label can be overridden', (tester) async {
    await pumpUi(tester, const AppTextField.password(label: 'New password'));
    expect(find.text('New password'), findsOneWidget);
    expect(find.text('Password'), findsNothing);
  });

  testWidgets('a disabled field takes no input and no toggle', (tester) async {
    await pumpUi(tester, const AppTextField.password(enabled: false));

    expect(_field(tester).enabled, isFalse);
    final toggle = tester.widget<IconButton>(find.byType(IconButton));
    expect(toggle.onPressed, isNull);
  });

  testWidgets('the toggle meets tap-target and label guidelines', (
    tester,
  ) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, const AppTextField.password());
    await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
    await expectLater(tester, meetsGuideline(labeledTapTargetGuideline));
    handle.dispose();
  });

  testWidgets('text field: one line, labelled, for a name', (tester) async {
    await pumpUi(tester, const AppTextField.text(label: 'Name'));

    final field = _field(tester);
    expect(field.maxLines, 1);
    expect(field.obscureText, isFalse);
    expect(field.keyboardType, TextInputType.name);
    expect(field.textCapitalization, TextCapitalization.words);
    expect(field.autofillHints, isEmpty);
    expect(find.text('Name'), findsOneWidget);
  });

  testWidgets('multiline field grows with its text and takes line breaks', (
    tester,
  ) async {
    await pumpUi(tester, const AppTextField.multiline(label: 'About you'));

    final field = _field(tester);
    expect(field.maxLines, isNull);
    expect(field.minLines, 3);
    expect(field.keyboardType, TextInputType.multiline);
    expect(field.textInputAction, TextInputAction.newline);
    expect(field.textCapitalization, TextCapitalization.sentences);
    expect(find.text('About you'), findsOneWidget);
  });

  testWidgets('text and multiline fields show a given error and disable', (
    tester,
  ) async {
    for (final field in [
      const AppTextField.text(
        label: 'Name',
        errorText: 'Too long',
        enabled: false,
      ),
      const AppTextField.multiline(
        label: 'About you',
        errorText: 'Too long',
        enabled: false,
      ),
    ]) {
      await pumpUi(tester, field);
      expect(find.text('Too long'), findsOneWidget);
      expect(_field(tester).enabled, isFalse);
    }
  });
}

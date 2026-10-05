import 'package:flutter/material.dart';

import '../widgets/app_text_field.dart';
import 'preview_support.dart';

const _group = 'AppTextField';

@VocaPreview(group: _group, name: 'Email')
Widget emailField() => const AppTextField.email();

@VocaPreview(group: _group, name: 'Email with error')
Widget emailFieldWithError() =>
    const AppTextField.email(errorText: 'Enter a valid email address.');

@VocaPreview(group: _group, name: 'Password')
Widget passwordField() => const AppTextField.password();

@VocaPreview(group: _group, name: 'Password, dark', brightness: Brightness.dark)
Widget passwordFieldDark() => const AppTextField.password();

@VocaPreview(group: _group, name: 'New password, disabled')
Widget newPasswordFieldDisabled() =>
    const AppTextField.password(newPassword: true, enabled: false);

@VocaPreview(group: _group, name: 'Name')
Widget nameField() => const AppTextField.text(label: 'Name');

@VocaPreview(group: _group, name: 'Name with error')
Widget nameFieldWithError() => const AppTextField.text(
  label: 'Name',
  errorText: 'This name is too long. Use a shorter one.',
);

@VocaPreview(group: _group, name: 'Multiline')
Widget multilineField() => const AppTextField.multiline(label: 'About you');

@VocaPreview(
  group: _group,
  name: 'Multiline with text, dark',
  brightness: Brightness.dark,
)
Widget multilineFieldDarkFilled() => _FilledMultiline();

class _FilledMultiline extends StatefulWidget {
  @override
  State<_FilledMultiline> createState() => _FilledMultilineState();
}

class _FilledMultilineState extends State<_FilledMultiline> {
  final _controller = TextEditingController(
    text:
        'I’m learning Japanese and can help with Spanish.\n\n'
        'Evenings and weekends work best for me.',
  );

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) =>
      AppTextField.multiline(label: 'About you', controller: _controller);
}

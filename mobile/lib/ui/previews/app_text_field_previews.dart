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

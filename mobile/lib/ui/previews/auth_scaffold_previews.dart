import 'package:flutter/material.dart';

import '../widgets/app_text_field.dart';
import '../widgets/auth_scaffold.dart';
import '../widgets/form_error_banner.dart';
import '../widgets/google_sign_in_button.dart';
import '../widgets/primary_button.dart';
import 'preview_support.dart';

const _group = 'AuthScaffold';

void _noop() {}

/// A sample log-in layout built only from the shared components.
Widget _sampleLogin({bool withError = false}) {
  return AuthScaffold(
    title: 'Log in',
    children: [
      if (withError)
        const FormErrorBanner(message: 'Incorrect email or password.'),
      const AppTextField.email(textInputAction: TextInputAction.next),
      const AppTextField.password(textInputAction: TextInputAction.done),
      const PrimaryButton(label: 'Log in', onPressed: _noop),
      const GoogleSignInButton(onPressed: _noop),
      TextButton(onPressed: _noop, child: const Text('Forgot password?')),
    ],
  );
}

@VocaPreview(
  group: _group,
  name: 'Log in sample',
  size: Size(390, 760),
  brightness: Brightness.light,
)
Widget authScaffoldLight() => _sampleLogin();

@VocaPreview(
  group: _group,
  name: 'Log in sample, dark, error',
  size: Size(390, 760),
  brightness: Brightness.dark,
)
Widget authScaffoldDarkError() => _sampleLogin(withError: true);

@VocaPreview(
  group: _group,
  name: 'Small screen, large text',
  size: Size(320, 480),
  textScaleFactor: 2,
)
Widget authScaffoldSmallLargeText() => _sampleLogin(withError: true);

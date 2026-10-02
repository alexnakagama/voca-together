import 'package:flutter/material.dart';

import '../widgets/google_sign_in_button.dart';
import 'preview_support.dart';

const _group = 'GoogleSignInButton';

void _noop() {}

@VocaPreview(group: _group, name: 'Light', brightness: Brightness.light)
Widget googleButtonLight() => const GoogleSignInButton(onPressed: _noop);

@VocaPreview(group: _group, name: 'Dark', brightness: Brightness.dark)
Widget googleButtonDark() => const GoogleSignInButton(onPressed: _noop);

@VocaPreview(group: _group, name: 'Disabled')
Widget googleButtonDisabled() => const GoogleSignInButton(onPressed: null);

@VocaPreview(
  group: _group,
  name: 'Large text, narrow',
  size: Size(320, 160),
  textScaleFactor: 2,
)
Widget googleButtonLargeText() => const GoogleSignInButton(onPressed: _noop);

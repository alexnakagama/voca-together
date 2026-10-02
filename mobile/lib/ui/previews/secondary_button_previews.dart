import 'package:flutter/material.dart';

import '../widgets/secondary_button.dart';
import 'preview_support.dart';

const _group = 'SecondaryButton';

void _noop() {}

@VocaPreview(group: _group, name: 'Enabled')
Widget secondaryButton() =>
    const SecondaryButton(label: 'Log out', onPressed: _noop);

@VocaPreview(group: _group, name: 'Busy')
Widget secondaryButtonBusy() =>
    const SecondaryButton(label: 'Log out', onPressed: _noop, busy: true);

@VocaPreview(
  group: _group,
  name: 'Large text, dark',
  textScaleFactor: 2,
  brightness: Brightness.dark,
)
Widget secondaryButtonLargeText() => const SecondaryButton(
  label: 'Send a new verification email',
  onPressed: _noop,
);

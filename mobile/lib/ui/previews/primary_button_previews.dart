import 'package:flutter/material.dart';

import '../widgets/primary_button.dart';
import 'preview_support.dart';

const _group = 'PrimaryButton';

void _noop() {}

@VocaPreview(group: _group, name: 'Enabled')
Widget primaryButton() =>
    const PrimaryButton(label: 'Log in', onPressed: _noop);

@VocaPreview(group: _group, name: 'Disabled')
Widget primaryButtonDisabled() =>
    const PrimaryButton(label: 'Log in', onPressed: null);

@VocaPreview(group: _group, name: 'Busy')
Widget primaryButtonBusy() =>
    const PrimaryButton(label: 'Log in', onPressed: _noop, busy: true);

@VocaPreview(group: _group, name: 'Large text', textScaleFactor: 2)
Widget primaryButtonLargeText() =>
    const PrimaryButton(label: 'Create account', onPressed: _noop);

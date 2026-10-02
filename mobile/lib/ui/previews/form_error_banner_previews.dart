import 'package:flutter/material.dart';

import '../widgets/form_error_banner.dart';
import 'preview_support.dart';

const _group = 'FormErrorBanner';

@VocaPreview(group: _group, name: 'Short message')
Widget errorBanner() =>
    const FormErrorBanner(message: 'Incorrect email or password.');

@VocaPreview(
  group: _group,
  name: 'Long message, dark',
  brightness: Brightness.dark,
)
Widget errorBannerLong() => const FormErrorBanner(
  message:
      "We couldn't reach VocaTogether. Check your connection and try again "
      'in a moment.',
);

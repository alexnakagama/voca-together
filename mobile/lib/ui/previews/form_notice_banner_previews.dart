import 'package:flutter/material.dart';

import '../widgets/form_notice_banner.dart';
import 'preview_support.dart';

const _group = 'FormNoticeBanner';

@VocaPreview(group: _group, name: 'Short message')
Widget noticeBanner() =>
    const FormNoticeBanner(message: 'We’ve sent you a new link.');

@VocaPreview(
  group: _group,
  name: 'Long message, dark',
  brightness: Brightness.dark,
)
Widget noticeBannerLong() => const FormNoticeBanner(
  message:
      'Verify your email address to log in. Open the link we sent to '
      'ana@example.com, then log in again.',
);

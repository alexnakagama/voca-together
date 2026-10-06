import 'package:flutter/material.dart';

import '../theme.dart';
import '../widgets/language_chip.dart';
import 'preview_support.dart';

const _group = 'LanguageChip';

@VocaPreview(group: _group, name: 'One language')
Widget languageChip() => const Align(
  alignment: Alignment.centerLeft,
  child: LanguageChip(name: 'Spanish', level: 'Native'),
);

@VocaPreview(
  group: _group,
  name: 'A wrapping list, dark',
  brightness: Brightness.dark,
)
Widget languageChipList() => const Wrap(
  spacing: Spacing.sm,
  runSpacing: Spacing.sm,
  children: [
    LanguageChip(name: 'Spanish', level: 'Native'),
    LanguageChip(name: 'English', level: 'C1'),
    LanguageChip(name: 'Japanese', level: 'A2'),
    LanguageChip(name: 'Scottish Gaelic', level: 'B1'),
  ],
);

@VocaPreview(
  group: _group,
  name: 'Long name, large text',
  size: Size(320, double.infinity),
  textScaleFactor: 2,
)
Widget languageChipLargeText() => const Wrap(
  children: [LanguageChip(name: 'Norwegian Bokmål', level: 'Native')],
);

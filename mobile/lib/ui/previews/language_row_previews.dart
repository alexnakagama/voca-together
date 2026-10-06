import 'package:flutter/material.dart';

import '../widgets/language_row.dart';
import 'preview_support.dart';

const _group = 'LanguageRow';

void _noop() {}

@VocaPreview(group: _group, name: 'Name and endonym')
Widget languageRow() => const LanguageRow(
  name: 'Spanish',
  endonym: 'Español',
  level: 'Native',
  levelSemanticLabel: 'Spanish, level Native',
  removeLabel: 'Remove Spanish',
  onLevelPressed: _noop,
  onRemove: _noop,
);

@VocaPreview(
  group: _group,
  name: 'Same name in both, dark',
  brightness: Brightness.dark,
)
Widget languageRowSameName() => const LanguageRow(
  name: 'English',
  endonym: 'English',
  level: 'C1',
  levelSemanticLabel: 'English, level C1',
  removeLabel: 'Remove English',
  onLevelPressed: _noop,
  onRemove: _noop,
);

@VocaPreview(group: _group, name: 'Disabled')
Widget languageRowDisabled() => const LanguageRow(
  name: 'Japanese',
  endonym: '日本語',
  level: 'A2',
  levelSemanticLabel: 'Japanese, level A2',
  removeLabel: 'Remove Japanese',
  onLevelPressed: null,
  onRemove: null,
);

@VocaPreview(
  group: _group,
  name: 'Long name, large text',
  size: Size(320, double.infinity),
  textScaleFactor: 2,
)
Widget languageRowLargeText() => const LanguageRow(
  name: 'Scottish Gaelic',
  endonym: 'Gàidhlig',
  level: 'Native',
  levelSemanticLabel: 'Scottish Gaelic, level Native',
  removeLabel: 'Remove Scottish Gaelic',
  onLevelPressed: _noop,
  onRemove: _noop,
);

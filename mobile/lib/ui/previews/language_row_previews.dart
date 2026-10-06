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
  moveUpLabel: 'Move Spanish up',
  moveDownLabel: 'Move Spanish down',
  onLevelPressed: _noop,
  onMoveUp: _noop,
  onMoveDown: _noop,
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
  moveUpLabel: 'Move English up',
  moveDownLabel: 'Move English down',
  onLevelPressed: _noop,
  onMoveUp: _noop,
  onMoveDown: _noop,
  onRemove: _noop,
);

@VocaPreview(group: _group, name: 'First in its list')
Widget languageRowFirst() => const LanguageRow(
  name: 'Portuguese',
  endonym: 'Português',
  level: 'B1',
  levelSemanticLabel: 'Portuguese, level B1',
  removeLabel: 'Remove Portuguese',
  moveUpLabel: 'Move Portuguese up',
  moveDownLabel: 'Move Portuguese down',
  onLevelPressed: _noop,
  onMoveUp: null,
  onMoveDown: _noop,
  onRemove: _noop,
);

@VocaPreview(group: _group, name: 'Disabled')
Widget languageRowDisabled() => const LanguageRow(
  name: 'Japanese',
  endonym: '日本語',
  level: 'A2',
  levelSemanticLabel: 'Japanese, level A2',
  removeLabel: 'Remove Japanese',
  moveUpLabel: 'Move Japanese up',
  moveDownLabel: 'Move Japanese down',
  onLevelPressed: null,
  onMoveUp: null,
  onMoveDown: null,
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
  moveUpLabel: 'Move Scottish Gaelic up',
  moveDownLabel: 'Move Scottish Gaelic down',
  onLevelPressed: _noop,
  onMoveUp: _noop,
  onMoveDown: _noop,
  onRemove: _noop,
);

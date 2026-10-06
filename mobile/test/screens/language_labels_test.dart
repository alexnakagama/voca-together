import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/api/languages.dart';
import 'package:vocatogether/screens/language_labels.dart';

import 'harness.dart';

void main() {
  test('every level has its own short name and its own description', () {
    final labels = {
      for (final level in LanguageLevel.values) languageLevelLabel(level, l10n),
    };
    final descriptions = {
      for (final level in LanguageLevel.values)
        languageLevelDescription(level, l10n),
    };
    expect(labels, hasLength(LanguageLevel.values.length));
    expect(descriptions, hasLength(LanguageLevel.values.length));
    expect(labels.followedBy(descriptions), everyElement(isNotEmpty));
  });
}

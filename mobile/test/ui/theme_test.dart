import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/theme.dart';

import 'harness.dart';

void main() {
  for (final (name, theme, brightness) in [
    ('light', AppTheme.light, Brightness.light),
    ('dark', AppTheme.dark, Brightness.dark),
  ]) {
    group('$name theme', () {
      test('is Material 3 with the expected brightness', () {
        expect(theme.useMaterial3, isTrue);
        expect(theme.colorScheme.brightness, brightness);
      });

      test('color pairs meet WCAG AA contrast for text', () {
        final s = theme.colorScheme;
        final pairs = {
          'primary': (s.primary, s.onPrimary),
          'primaryContainer': (s.primaryContainer, s.onPrimaryContainer),
          'tertiary': (s.tertiary, s.onTertiary),
          'tertiaryContainer': (s.tertiaryContainer, s.onTertiaryContainer),
          'surface': (s.surface, s.onSurface),
          'surfaceVariant': (s.surface, s.onSurfaceVariant),
          'error': (s.error, s.onError),
          'errorContainer': (s.errorContainer, s.onErrorContainer),
        };
        pairs.forEach((role, pair) {
          expect(
            contrastRatio(pair.$1, pair.$2),
            greaterThanOrEqualTo(4.5),
            reason: role,
          );
        });
      });

      test('tertiary is warm (coral), primary is teal', () {
        final tertiary = HSLColor.fromColor(theme.colorScheme.tertiary).hue;
        final primary = HSLColor.fromColor(theme.colorScheme.primary).hue;
        expect(tertiary, anyOf(lessThan(40), greaterThan(340)));
        expect(primary, inInclusiveRange(160, 200));
      });

      test('buttons are at least 48 dp tall', () {
        for (final style in [
          theme.filledButtonTheme.style,
          theme.outlinedButtonTheme.style,
          theme.textButtonTheme.style,
        ]) {
          expect(style!.minimumSize!.resolve({})!.height, 48);
        }
      });
    });
  }
}

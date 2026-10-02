import 'package:flutter/material.dart';

/// The app's Material 3 themes.
///
/// A deep-teal seed gives the primary, secondary and neutral roles; the
/// tertiary roles (accents, used sparingly) come from a coral seed. Text uses
/// the platform font (Roboto on Android).
abstract final class AppTheme {
  static final ThemeData light = _build(Brightness.light);
  static final ThemeData dark = _build(Brightness.dark);

  static const _teal = Color(0xFF00696B);
  static const _coral = Color(0xFFE8735A);

  static ThemeData _build(Brightness brightness) {
    final coral = ColorScheme.fromSeed(
      seedColor: _coral,
      brightness: brightness,
    );
    // Take all four tertiary roles from one scheme so every on-color keeps
    // the contrast Material computed for its pair.
    final scheme =
        ColorScheme.fromSeed(seedColor: _teal, brightness: brightness).copyWith(
          tertiary: coral.primary,
          onTertiary: coral.onPrimary,
          tertiaryContainer: coral.primaryContainer,
          onTertiaryContainer: coral.onPrimaryContainer,
        );

    const buttonShape = StadiumBorder();
    const buttonMinSize = Size(64, 48);
    final fieldBorder = OutlineInputBorder(
      borderRadius: BorderRadius.circular(Radii.field),
    );

    return ThemeData(
      colorScheme: scheme,
      materialTapTargetSize: MaterialTapTargetSize.padded,
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          minimumSize: buttonMinSize,
          shape: buttonShape,
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          minimumSize: buttonMinSize,
          shape: buttonShape,
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(minimumSize: buttonMinSize),
      ),
      inputDecorationTheme: InputDecorationThemeData(border: fieldBorder),
    );
  }
}

/// Spacing steps, in logical pixels.
abstract final class Spacing {
  static const double xs = 4;
  static const double sm = 8;
  static const double md = 16;
  static const double lg = 24;
  static const double xl = 32;
}

/// Corner radii, in logical pixels.
abstract final class Radii {
  static const double field = 12;
  static const double card = 12;
}

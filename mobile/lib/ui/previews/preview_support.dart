import 'package:flutter/material.dart';
import 'package:flutter/widget_previews.dart';

import '../../l10n/app_localizations.dart';
import '../theme.dart';

/// A widget preview rendered with the app's theme and localizations.
///
/// Light or dark follows the preview's [brightness] (or the previewer's
/// toggle). Previews are pure UI: they must not touch `dart:io`, plugins,
/// HTTP, the session or the configuration. Text written in a preview is
/// sample content, not app strings.
///
/// The previewer lays previews out with unbounded width unless they set a
/// [size], and our widgets fill the available width, so the default is a
/// phone-width column (an infinite height means "no height").
final class VocaPreview extends Preview {
  const VocaPreview({
    required super.group,
    required super.name,
    super.size = defaultSize,
    super.textScaleFactor,
    super.brightness,
  }) : super(wrapper: previewWrapper, localizations: previewLocalizations);

  static const defaultSize = Size(360, double.infinity);
}

/// Applies the app theme matching the preview's brightness and gives the
/// previewed widget a surface to sit on.
Widget previewWrapper(Widget child) {
  return Builder(
    builder: (context) {
      final theme = MediaQuery.platformBrightnessOf(context) == Brightness.dark
          ? AppTheme.dark
          : AppTheme.light;
      return Theme(
        data: theme,
        child: Material(
          color: theme.colorScheme.surface,
          child: Padding(
            padding: const EdgeInsets.all(Spacing.md),
            child: child,
          ),
        ),
      );
    },
  );
}

PreviewLocalizationsData previewLocalizations() {
  return const PreviewLocalizationsData(
    localizationsDelegates: AppLocalizations.localizationsDelegates,
    supportedLocales: AppLocalizations.supportedLocales,
  );
}

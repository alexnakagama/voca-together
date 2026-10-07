import 'dart:convert';
import 'dart:typed_data';

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

/// A sample profile picture for previews: a 16 by 16 JPEG held in the
/// source, so a preview needs no file, asset or network.
final Uint8List previewPicture = base64Decode(
  '/9j/2wCEAA0JCgsKCA0LCgsODg0PEyAVExISEyccHhcgLikxMC4pLSwzOko+MzZGNywt'
  'QFdBRkxOUlNSMj5aYVpQYEpRUk8BDg4OExETJhUVJk81LTVPT09PT09PT09PT09PT09P'
  'T09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT//AABEIABAAEAMBIgACEQED'
  'EQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAAB'
  'fQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkq'
  'NDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SV'
  'lpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq'
  '8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUE'
  'BAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZ'
  'GiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeI'
  'iYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl'
  '5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AMiKOrkUdEUdXIo6zqVCsLhT/9k=',
);

import '../api/languages.dart';
import '../l10n/app_localizations.dart';

/// The short localized name of [level], as shown next to a language: "B2",
/// "Native".
String languageLevelLabel(LanguageLevel level, AppLocalizations l10n) {
  return switch (level) {
    LanguageLevel.a1 => l10n.languageLevelA1,
    LanguageLevel.a2 => l10n.languageLevelA2,
    LanguageLevel.b1 => l10n.languageLevelB1,
    LanguageLevel.b2 => l10n.languageLevelB2,
    LanguageLevel.c1 => l10n.languageLevelC1,
    LanguageLevel.c2 => l10n.languageLevelC2,
    LanguageLevel.native => l10n.languageLevelNative,
  };
}

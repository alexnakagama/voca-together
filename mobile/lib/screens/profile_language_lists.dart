import 'package:flutter/material.dart';

import '../api/languages.dart';
import '../l10n/app_localizations.dart';
import '../ui/theme.dart';
import '../ui/widgets/language_chip.dart';
import 'language_labels.dart';

/// A member's languages, read-only: the ones they speak and the ones they
/// are learning, each list under its heading with every language's level,
/// in the member's order (decision 030).
///
/// It shows a selection that was already loaded and requests nothing. A
/// list with no language gets no heading; with both empty, one text says
/// that none is chosen.
class ProfileLanguageLists extends StatelessWidget {
  const ProfileLanguageLists({
    super.key,
    required this.languages,
    required this.names,
  });

  final UserLanguages languages;

  /// The catalog's English names by language code.
  final Map<String, String> names;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    if (languages.spoken.isEmpty && languages.learning.isEmpty) {
      return Text(
        l10n.languagesEmpty,
        style: Theme.of(context).textTheme.bodyMedium,
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (languages.spoken.isNotEmpty)
          _LanguageList(
            heading: l10n.languagesSpokenHeading,
            languages: languages.spoken,
            names: names,
          ),
        if (languages.spoken.isNotEmpty && languages.learning.isNotEmpty)
          const SizedBox(height: Spacing.md),
        if (languages.learning.isNotEmpty)
          _LanguageList(
            heading: l10n.languagesLearningHeading,
            languages: languages.learning,
            names: names,
          ),
      ],
    );
  }
}

/// One of the two lists, under its heading, in the member's order.
class _LanguageList extends StatelessWidget {
  const _LanguageList({
    required this.heading,
    required this.languages,
    required this.names,
  });

  final String heading;
  final List<UserLanguage> languages;
  final Map<String, String> names;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Semantics(
          header: true,
          child: Text(
            heading,
            style: theme.textTheme.titleSmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ),
        const SizedBox(height: Spacing.sm),
        Wrap(
          spacing: Spacing.sm,
          runSpacing: Spacing.sm,
          children: [
            for (final language in languages)
              LanguageChip(
                // A code the catalog doesn't name is shown as it is, so the
                // member still sees every language they have (030).
                name: names[language.code] ?? language.code,
                level: languageLevelLabel(language.level, l10n),
              ),
          ],
        ),
      ],
    );
  }
}

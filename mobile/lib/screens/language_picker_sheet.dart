import 'package:flutter/material.dart';

import '../api/languages.dart';
import '../l10n/app_localizations.dart';
import '../ui/theme.dart';
import 'language_labels.dart';

/// Asks the member to pick one of [languages] in a sheet with a search field,
/// and completes with it, or with null when the sheet is closed.
///
/// The caller passes only the languages that can still be added. The choice
/// is the sheet's result: no language goes in a route (decision 030).
Future<Language?> showLanguagePicker(
  BuildContext context, {
  required List<Language> languages,
}) {
  return showModalBottomSheet<Language>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    builder: (context) => _LanguagePicker(languages: languages),
  );
}

/// Asks the member for their level in the language called [name], offering
/// [levels] in the order given with [current] marked, and completes with the
/// choice, or with null when the sheet is closed.
Future<LanguageLevel?> showLanguageLevelPicker(
  BuildContext context, {
  required String name,
  required List<LanguageLevel> levels,
  LanguageLevel? current,
}) {
  return showModalBottomSheet<LanguageLevel>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    builder: (context) =>
        _LevelPicker(name: name, levels: levels, current: current),
  );
}

class _LanguagePicker extends StatefulWidget {
  const _LanguagePicker({required this.languages});

  final List<Language> languages;

  @override
  State<_LanguagePicker> createState() => _LanguagePickerState();
}

class _LanguagePickerState extends State<_LanguagePicker> {
  final _search = TextEditingController();
  String _query = '';

  @override
  void initState() {
    super.initState();
    _search.addListener(_searchEdited);
  }

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  void _searchEdited() {
    final query = _search.text.trim().toLowerCase();
    if (query != _query) setState(() => _query = query);
  }

  /// Whether [language]'s name, own name or code contains the search text,
  /// whatever the case.
  bool _matches(Language language) =>
      _query.isEmpty ||
      language.name.toLowerCase().contains(_query) ||
      language.endonym.toLowerCase().contains(_query) ||
      language.code.toLowerCase().contains(_query);

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final theme = Theme.of(context);
    final matches = [
      for (final language in widget.languages)
        if (_matches(language)) language,
    ];

    return Padding(
      // Keeps the list above the keyboard.
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
      // One scroll view, with the search field pinned: on a small screen
      // with the keyboard open there may be little room for anything else.
      child: CustomScrollView(
        slivers: [
          SliverToBoxAdapter(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(
                Spacing.lg,
                Spacing.lg,
                Spacing.lg,
                Spacing.sm,
              ),
              child: Semantics(
                header: true,
                child: Text(
                  l10n.languagePickerTitle,
                  style: theme.textTheme.titleLarge,
                ),
              ),
            ),
          ),
          PinnedHeaderSliver(
            child: Material(
              // The sheet's own color, so the list scrolls under the field.
              color: theme.colorScheme.surfaceContainerLow,
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: Spacing.lg,
                  vertical: Spacing.sm,
                ),
                child: TextField(
                  controller: _search,
                  textInputAction: TextInputAction.search,
                  autocorrect: false,
                  decoration: InputDecoration(
                    labelText: l10n.languagePickerSearchLabel,
                    prefixIcon: const Icon(Icons.search),
                  ),
                ),
              ),
            ),
          ),
          if (matches.isEmpty)
            SliverToBoxAdapter(
              child: Padding(
                padding: const EdgeInsets.all(Spacing.lg),
                child: Text(
                  l10n.languagePickerNoMatch,
                  style: theme.textTheme.bodyMedium,
                ),
              ),
            )
          else
            SliverList.builder(
              itemCount: matches.length,
              itemBuilder: (context, index) {
                final language = matches[index];
                return ListTile(
                  contentPadding: const EdgeInsets.symmetric(
                    horizontal: Spacing.lg,
                  ),
                  title: Text(language.name),
                  // Some languages are named the same in English.
                  subtitle: language.endonym == language.name
                      ? null
                      : Text(language.endonym),
                  onTap: () => Navigator.of(context).pop(language),
                );
              },
            ),
        ],
      ),
    );
  }
}

class _LevelPicker extends StatelessWidget {
  const _LevelPicker({
    required this.name,
    required this.levels,
    required this.current,
  });

  final String name;
  final List<LanguageLevel> levels;
  final LanguageLevel? current;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final theme = Theme.of(context);
    return SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(
              Spacing.lg,
              Spacing.lg,
              Spacing.lg,
              Spacing.sm,
            ),
            child: Semantics(
              header: true,
              child: Text(
                l10n.languageLevelPickerTitle(name),
                style: theme.textTheme.titleLarge,
              ),
            ),
          ),
          for (final level in levels)
            ListTile(
              contentPadding: const EdgeInsets.symmetric(
                horizontal: Spacing.lg,
              ),
              title: Text(languageLevelLabel(level, l10n)),
              subtitle: Text(languageLevelDescription(level, l10n)),
              selected: level == current,
              trailing: level == current ? const Icon(Icons.check) : null,
              onTap: () => Navigator.of(context).pop(level),
            ),
          const SizedBox(height: Spacing.sm),
        ],
      ),
    );
  }
}

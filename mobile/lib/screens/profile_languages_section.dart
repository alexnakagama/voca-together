import 'dart:async';

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../api/languages.dart';
import '../l10n/app_localizations.dart';
import '../router.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/language_chip.dart';
import '../ui/widgets/secondary_button.dart';
import 'failure_presentation.dart';
import 'language_labels.dart';

/// The read-only summary of the signed-in user's own languages on the
/// profile screen: the ones they speak and the ones they are learning, each
/// with its level, in the member's order (decision 030).
///
/// It loads by itself, the catalog (for the names) and the selection
/// together, and keeps its own state: a failure here shows a retry in the
/// section and leaves the profile form alone. Nothing is changed from here:
/// its button opens the editor, and the section loads again when the member
/// comes back.
class ProfileLanguagesSection extends StatefulWidget {
  const ProfileLanguagesSection({super.key, required this.session});

  final SessionManager session;

  @override
  State<ProfileLanguagesSection> createState() =>
      _ProfileLanguagesSectionState();
}

class _ProfileLanguagesSectionState extends State<ProfileLanguagesSection> {
  /// The load hasn't answered yet (or is being retried).
  bool _loading = true;
  String? _loadError;

  /// The member's selection, or null until a load succeeds.
  UserLanguages? _languages;

  /// The catalog's English names by language code.
  Map<String, String> _names = const {};

  /// Identifies the latest load; answers to earlier ones are dropped.
  int _request = 0;

  /// The editor was opened from here and hasn't been left yet.
  bool _editorOpen = false;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  void _retry() {
    setState(() {
      _loading = true;
      _loadError = null;
    });
    unawaited(_load());
  }

  /// Opens the editor on top of the profile, and on coming back, saved or
  /// not, loads what the server has: a save whose answer was lost may still
  /// have been stored.
  Future<void> _openEditor() async {
    // A second tap before the editor covers the button would push it twice.
    if (_editorOpen) return;
    _editorOpen = true;
    await context.push<void>(Routes.languages);
    _editorOpen = false;
    if (!mounted) return;
    setState(() {
      _loading = true;
      _loadError = null;
      // What was shown may no longer be what is stored: if this load fails,
      // the section shows the failure, not the old selection.
      _languages = null;
    });
    unawaited(_load());
  }

  Future<void> _load() async {
    final request = ++_request;
    try {
      final catalog = widget.session.languageCatalog();
      final languages = widget.session.languages();
      // Waits for both, so neither is left to fail unobserved.
      await Future.wait<void>([catalog, languages]);
      final names = {
        for (final language in await catalog) language.code: language.name,
      };
      final selection = await languages;
      if (!mounted || request != _request) return;
      setState(() {
        _names = names;
        _languages = selection;
        _loading = false;
      });
    } on Exception catch (e) {
      if (!mounted || request != _request) return;
      final failure = presentFailure(e, AppLocalizations.of(context));
      // The session is gone: the router is already taking the user to log
      // in, so an error here would only flash.
      if (failure.kind == FailureKind.sessionEnded) return;
      setState(() {
        _loadError = failure.message;
        _loading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final theme = Theme.of(context);
    final loadError = _loadError;
    final languages = _languages;

    final List<Widget> content;
    if (_loading) {
      content = [
        Padding(
          padding: const EdgeInsets.symmetric(vertical: Spacing.md),
          child: Center(
            child: CircularProgressIndicator(
              semanticsLabel: l10n.languagesLoading,
            ),
          ),
        ),
      ];
    } else if (languages == null) {
      content = [
        if (loadError != null) ...[
          FormErrorBanner(message: loadError),
          const SizedBox(height: Spacing.md),
        ],
        SecondaryButton(label: l10n.tryAgain, onPressed: _retry),
      ];
    } else if (languages.spoken.isEmpty && languages.learning.isEmpty) {
      content = [Text(l10n.languagesEmpty, style: theme.textTheme.bodyMedium)];
    } else {
      content = [
        if (languages.spoken.isNotEmpty)
          _LanguageList(
            heading: l10n.languagesSpokenHeading,
            languages: languages.spoken,
            names: _names,
          ),
        if (languages.spoken.isNotEmpty && languages.learning.isNotEmpty)
          const SizedBox(height: Spacing.md),
        if (languages.learning.isNotEmpty)
          _LanguageList(
            heading: l10n.languagesLearningHeading,
            languages: languages.learning,
            names: _names,
          ),
      ];
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Semantics(
          header: true,
          child: Text(l10n.languagesHeading, style: theme.textTheme.titleLarge),
        ),
        const SizedBox(height: Spacing.md),
        ...content,
        if (!_loading && languages != null) ...[
          const SizedBox(height: Spacing.lg),
          SecondaryButton(
            label: l10n.languagesEditButton,
            onPressed: () => unawaited(_openEditor()),
          ),
        ],
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

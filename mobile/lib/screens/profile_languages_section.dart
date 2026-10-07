import 'dart:async';

import 'package:flutter/material.dart';

import '../api/languages.dart';
import '../l10n/app_localizations.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/secondary_button.dart';
import 'failure_presentation.dart';
import 'profile_language_lists.dart';

/// The read-only summary of the signed-in user's own languages on the
/// profile page: the ones they speak and the ones they are learning, each
/// with its level, in the member's order (decision 030).
///
/// It loads by itself, the catalog (for the names) and the selection
/// together, and keeps its own state: a failure here shows a retry in the
/// section and leaves the rest of the page alone. Nothing is changed from
/// here and nothing opens the editor. It loads once: the page mounts a new
/// section each time it loads again.
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
    } else {
      content = [ProfileLanguageLists(languages: languages, names: _names)];
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
      ],
    );
  }
}

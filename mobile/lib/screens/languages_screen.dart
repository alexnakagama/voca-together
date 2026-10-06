import 'dart:async';

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../api/languages.dart';
import '../l10n/app_localizations.dart';
import '../router.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/auth_scaffold.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/language_row.dart';
import '../ui/widgets/primary_button.dart';
import '../ui/widgets/secondary_button.dart';
import 'failure_presentation.dart';
import 'language_labels.dart';
import 'language_picker_sheet.dart';

/// The editor of the signed-in user's own languages: the ones they speak and
/// the ones they are learning, each with a level, in the member's order
/// (`GET /v1/languages`, `GET`/`PUT /v1/me/languages` through
/// [SessionManager], decision 030).
///
/// It loads the catalog and the selection together and edits a copy. Save
/// sends the complete selection, both lists, as the member arranged them: no
/// rule of the server is checked here. Leaving with unsaved changes asks
/// first. It never decides access; when the session ends the router leaves
/// this screen on its own.
class LanguagesScreen extends StatefulWidget {
  const LanguagesScreen({super.key, required this.session});

  final SessionManager session;

  @override
  State<LanguagesScreen> createState() => _LanguagesScreenState();
}

/// The two lists of a selection.
enum _ListKind { spoken, learning }

class _LanguagesScreenState extends State<LanguagesScreen> {
  /// The load hasn't answered yet (or is being retried).
  bool _loading = true;
  String? _loadError;

  /// The catalog, in the server's order, and its entries by code.
  List<Language> _catalog = const [];
  Map<String, Language> _byCode = const {};

  /// The selection as loaded, or null until a load succeeds. What the lists
  /// below are compared with to know whether anything changed.
  UserLanguages? _loaded;

  /// The lists the member edits. They start as what was loaded, entries the
  /// catalog doesn't name included, so a save drops only what the member
  /// removed.
  List<UserLanguage> _spoken = [];
  List<UserLanguage> _learning = [];

  bool _busy = false;
  String? _banner;
  String? _spokenError;
  String? _learningError;

  final _spokenKey = GlobalKey();
  final _learningKey = GlobalKey();

  /// Identifies the latest load; answers to earlier ones are dropped.
  int _request = 0;

  /// The member's complete selection as it is on screen: what a save sends,
  /// always both lists (030).
  UserLanguages get _selection =>
      UserLanguages(spoken: _spoken, learning: _learning);

  bool get _changed => _loaded != null && _selection != _loaded;

  List<UserLanguage> _list(_ListKind kind) => switch (kind) {
    _ListKind.spoken => _spoken,
    _ListKind.learning => _learning,
  };

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
      final entries = await catalog;
      final selection = await languages;
      if (!mounted || request != _request) return;
      setState(() {
        _catalog = entries;
        _byCode = {for (final language in entries) language.code: language};
        _loaded = selection;
        _spoken = [...selection.spoken];
        _learning = [...selection.learning];
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

  /// Applies a change to the lists. What the last save said no longer
  /// describes them, under either list: a language in both is reported on
  /// one and may be fixed in the other (029).
  void _edit(VoidCallback change) {
    setState(() {
      change();
      _banner = null;
      _spokenError = null;
      _learningError = null;
    });
  }

  /// The levels offered for a language of [kind]. A language being learned
  /// is never native (029), so that choice isn't offered there; nothing is
  /// checked when saving.
  List<LanguageLevel> _levels(_ListKind kind) => switch (kind) {
    _ListKind.spoken => LanguageLevel.values,
    _ListKind.learning => [
      for (final level in LanguageLevel.values)
        if (level != LanguageLevel.native) level,
    ],
  };

  Future<void> _add(_ListKind kind) async {
    if (_busy) return;
    // A language is in one list only (029), so one already chosen in either
    // isn't offered again.
    final chosen = {
      for (final language in _spoken.followedBy(_learning)) language.code,
    };
    final language = await showLanguagePicker(
      context,
      languages: [
        for (final language in _catalog)
          if (!chosen.contains(language.code)) language,
      ],
    );
    if (language == null || !mounted) return;
    final level = await showLanguageLevelPicker(
      context,
      name: language.name,
      levels: _levels(kind),
    );
    if (level == null || !mounted) return;
    _edit(() => _list(kind).add(UserLanguage(language.code, level)));
  }

  Future<void> _changeLevel(_ListKind kind, int index) async {
    if (_busy) return;
    final language = _list(kind)[index];
    final level = await showLanguageLevelPicker(
      context,
      name: _name(language),
      levels: _levels(kind),
      current: language.level,
    );
    if (level == null || !mounted) return;
    _edit(() => _list(kind)[index] = UserLanguage(language.code, level));
  }

  /// Swaps the entry at [index] with its neighbour [by] places away.
  void _move(_ListKind kind, int index, int by) {
    _edit(() {
      final list = _list(kind);
      final language = list.removeAt(index);
      list.insert(index + by, language);
    });
  }

  void _remove(_ListKind kind, int index) {
    _edit(() => _list(kind).removeAt(index));
  }

  /// A code the catalog doesn't name is shown as it is, so the member still
  /// sees, and keeps, every language they have (030).
  String _name(UserLanguage language) =>
      _byCode[language.code]?.name ?? language.code;

  Future<void> _save() async {
    if (_busy || !_changed) return;
    final l10n = AppLocalizations.of(context);
    setState(() {
      _busy = true;
      _banner = null;
      _spokenError = null;
      _learningError = null;
    });
    try {
      // As chosen, and whole: the server validates (029).
      await widget.session.saveLanguages(_selection);
      if (!mounted) return;
      // The profile loads what was stored when it is shown again.
      _leave();
    } on Exception catch (e) {
      if (!mounted) return;
      final failure = presentFailure(e, l10n);
      if (failure.kind == FailureKind.sessionEnded) return;
      setState(() {
        _banner = failure.message;
        _spokenError = failure.spokenError;
        _learningError = failure.learningError;
        _busy = false;
      });
      final first = failure.spokenError != null
          ? _spokenKey
          : failure.learningError != null
          ? _learningKey
          : null;
      if (first != null) {
        // The error is laid out only after this frame.
        WidgetsBinding.instance.addPostFrameCallback((_) {
          final target = first.currentContext;
          if (target != null) unawaited(Scrollable.ensureVisible(target));
        });
      }
    }
  }

  /// Leaves the editor if nothing would be lost, otherwise asks first. The
  /// Cancel button, the app bar's back button and the system's back all end
  /// here.
  void _requestLeave() {
    // Leaving now would let the profile load before the save is stored.
    if (_busy) return;
    if (_changed) {
      unawaited(_confirmDiscard());
    } else {
      _leave();
    }
  }

  Future<void> _confirmDiscard() async {
    final l10n = AppLocalizations.of(context);
    final discard = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        // At large text on a small screen the text is taller than the
        // dialog can be.
        scrollable: true,
        title: Text(l10n.languagesDiscardTitle),
        content: Text(l10n.languagesDiscardMessage),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(l10n.languagesDiscardKeep),
          ),
          TextButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(l10n.languagesDiscardConfirm),
          ),
        ],
      ),
    );
    if (discard != true || !mounted) return;
    _leave();
  }

  void _leave() {
    if (context.canPop()) {
      context.pop();
    } else {
      // Nothing below: the editor is the profile's.
      context.go(Routes.profile);
    }
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final theme = Theme.of(context);
    final loadError = _loadError;
    final banner = _banner;

    final List<Widget> content;
    if (_loading) {
      content = [
        Padding(
          padding: const EdgeInsets.symmetric(vertical: Spacing.xl),
          child: Center(
            child: CircularProgressIndicator(
              semanticsLabel: l10n.languagesLoading,
            ),
          ),
        ),
      ];
    } else if (_loaded == null) {
      content = [
        if (loadError != null) ...[
          FormErrorBanner(message: loadError),
          const SizedBox(height: Spacing.md),
        ],
        PrimaryButton(label: l10n.tryAgain, onPressed: _retry),
      ];
    } else {
      content = [
        if (banner != null) ...[
          FormErrorBanner(message: banner),
          const SizedBox(height: Spacing.md),
        ],
        Text(l10n.languagesVisibilityNotice, style: theme.textTheme.bodyMedium),
        const SizedBox(height: Spacing.lg),
        _section(_ListKind.spoken, l10n),
        const Divider(height: Spacing.xl * 2),
        _section(_ListKind.learning, l10n),
        const SizedBox(height: Spacing.xl),
        PrimaryButton(
          label: l10n.languagesSaveButton,
          onPressed: _changed ? _save : null,
          busy: _busy,
        ),
        const SizedBox(height: Spacing.md),
        SecondaryButton(
          label: l10n.languagesCancelButton,
          onPressed: _busy ? null : _requestLeave,
        ),
      ];
    }

    return PopScope(
      // The back button and the system's back ask before a change is lost.
      canPop: !_changed && !_busy,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _requestLeave();
      },
      child: Scaffold(
        appBar: AppBar(title: Text(l10n.languagesEditorTitle)),
        body: SafeArea(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(Spacing.lg),
            child: Center(
              child: ConstrainedBox(
                constraints: const BoxConstraints(
                  maxWidth: AuthScaffold.maxContentWidth,
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: content,
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  /// One of the two lists: its heading, the server's error for it, its
  /// languages in the member's order, and the button that adds one.
  Widget _section(_ListKind kind, AppLocalizations l10n) {
    final theme = Theme.of(context);
    final list = _list(kind);
    final (key, heading, error) = switch (kind) {
      _ListKind.spoken => (
        _spokenKey,
        l10n.languagesSpokenHeading,
        _spokenError,
      ),
      _ListKind.learning => (
        _learningKey,
        l10n.languagesLearningHeading,
        _learningError,
      ),
    };
    return Column(
      key: key,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Semantics(
          header: true,
          child: Text(heading, style: theme.textTheme.titleLarge),
        ),
        const SizedBox(height: Spacing.sm),
        if (error != null) ...[
          FormErrorBanner(message: error),
          const SizedBox(height: Spacing.sm),
        ],
        for (final (index, language) in list.indexed)
          LanguageRow(
            name: _name(language),
            endonym: _byCode[language.code]?.endonym ?? language.code,
            level: languageLevelLabel(language.level, l10n),
            levelSemanticLabel: l10n.languageLevelSemantics(
              _name(language),
              languageLevelLabel(language.level, l10n),
            ),
            moveUpLabel: l10n.languageMoveUp(_name(language)),
            moveDownLabel: l10n.languageMoveDown(_name(language)),
            removeLabel: l10n.languageRemove(_name(language)),
            onLevelPressed: _busy ? null : () => _changeLevel(kind, index),
            onMoveUp: _busy || index == 0 ? null : () => _move(kind, index, -1),
            onMoveDown: _busy || index == list.length - 1
                ? null
                : () => _move(kind, index, 1),
            onRemove: _busy ? null : () => _remove(kind, index),
          ),
        Align(
          alignment: AlignmentDirectional.centerStart,
          child: TextButton.icon(
            onPressed: _busy ? null : () => _add(kind),
            icon: const Icon(Icons.add),
            label: Text(l10n.languagesAddButton),
          ),
        ),
      ],
    );
  }
}

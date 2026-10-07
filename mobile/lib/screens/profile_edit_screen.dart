import 'dart:async';

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../api/profile.dart';
import '../l10n/app_localizations.dart';
import '../media/photo_source.dart';
import '../router.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/app_text_field.dart';
import '../ui/widgets/auth_scaffold.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/primary_button.dart';
import '../ui/widgets/secondary_button.dart';
import 'failure_presentation.dart';
import 'profile_avatar_editor.dart';

/// The form where the signed-in user creates and edits their own profile:
/// the name and the "about you" text other members will see (`GET`/`PUT
/// /v1/me/profile` through [SessionManager], decision 028).
///
/// One form creates and edits: a user without a profile gets it empty. The
/// text is sent as typed, and a successful save returns to the profile
/// page, which shows what the server stored. Leaving with unsaved text asks
/// first. Its "Languages" row opens the languages editor on top; the form
/// holds no language itself.
///
/// [ProfileAvatarEditor] is mounted with the form and applies a picture at
/// once, apart from Save, so a change of picture is never an unsaved change.
/// While it works the form is locked as during a save.
///
/// It never decides access; when the session ends the router leaves this
/// screen on its own.
class ProfileEditScreen extends StatefulWidget {
  const ProfileEditScreen({
    super.key,
    required this.session,
    required this.photoSource,
  });

  final SessionManager session;

  /// The device's photo chooser, for the picture control.
  final PhotoSource photoSource;

  @override
  State<ProfileEditScreen> createState() => _ProfileEditScreenState();
}

class _ProfileEditScreenState extends State<ProfileEditScreen> {
  final _name = TextEditingController();
  final _bio = TextEditingController();
  final _nameFocus = FocusNode();
  final _bioFocus = FocusNode();

  /// The first load hasn't answered yet (or is being retried).
  bool _loading = true;
  String? _loadError;

  /// Whether the user has a saved profile; decides the heading.
  bool _exists = false;

  /// The text as loaded, empty without a profile. What the fields are
  /// compared with to know whether anything changed.
  String _loadedName = '';
  String _loadedBio = '';

  /// A save is in flight.
  bool _busy = false;

  /// The picture control is choosing, uploading or removing.
  bool _pictureBusy = false;

  String? _banner;
  String? _nameError;
  String? _bioError;
  String _lastName = '';
  String _lastBio = '';

  /// Identifies the latest load; answers to earlier ones are dropped.
  int _request = 0;

  /// The languages editor was opened from here and hasn't been left yet.
  bool _languagesOpen = false;

  bool get _changed => _name.text != _loadedName || _bio.text != _loadedBio;

  /// Nothing can be started or left: a save or a picture action is running.
  bool get _locked => _busy || _pictureBusy;

  @override
  void initState() {
    super.initState();
    _name.addListener(_nameEdited);
    _bio.addListener(_bioEdited);
    unawaited(_load());
  }

  @override
  void dispose() {
    _name.dispose();
    _bio.dispose();
    _nameFocus.dispose();
    _bioFocus.dispose();
    super.dispose();
  }

  void _nameEdited() {
    if (_name.text == _lastName) return;
    _lastName = _name.text;
    // Also rebuilds: whether leaving asks first follows the text.
    setState(() => _nameError = null);
  }

  void _bioEdited() {
    if (_bio.text == _lastBio) return;
    _lastBio = _bio.text;
    setState(() => _bioError = null);
  }

  /// Puts [profile]'s text in the form without it counting as an edit.
  void _show(Profile profile) {
    _loadedName = _lastName = profile.displayName;
    _loadedBio = _lastBio = profile.bio;
    _name.text = profile.displayName;
    _bio.text = profile.bio;
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
      final profile = await widget.session.profile();
      if (!mounted || request != _request) return;
      setState(() {
        if (profile != null) _show(profile);
        _exists = profile != null;
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

  Future<void> _save() async {
    if (_locked) return;
    final l10n = AppLocalizations.of(context);
    if (_name.text.trim().isEmpty) {
      setState(() {
        _banner = null;
        _nameError = l10n.displayNameRequired;
      });
      _nameFocus.requestFocus();
      return;
    }

    setState(() {
      _busy = true;
      _banner = null;
      _nameError = null;
      _bioError = null;
    });
    try {
      // As typed: the server normalizes and validates (027).
      await widget.session.saveProfile(displayName: _name.text, bio: _bio.text);
      if (!mounted) return;
      // The profile page loads what was stored when it is shown again.
      _leave();
    } on Exception catch (e) {
      if (!mounted) return;
      final failure = presentFailure(e, l10n);
      if (failure.kind == FailureKind.sessionEnded) return;
      setState(() {
        _banner = failure.message;
        _nameError = failure.displayNameError;
        _bioError = failure.bioError;
        _busy = false;
      });
      final focus = failure.displayNameError != null
          ? _nameFocus
          : failure.bioError != null
          ? _bioFocus
          : null;
      if (focus != null) {
        // The fields are enabled again only after this frame.
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) focus.requestFocus();
        });
      }
    }
  }

  /// Opens the languages editor on top of the form, which keeps its text.
  /// Nothing is loaded on coming back: the form shows no language.
  Future<void> _openLanguages() async {
    // A second tap before the editor covers the row would push it twice.
    if (_locked || _languagesOpen) return;
    _languagesOpen = true;
    await context.push<void>(Routes.languages);
    _languagesOpen = false;
  }

  /// Leaves the form if nothing would be lost, otherwise asks first. The
  /// Cancel button, the app bar's back button and the system's back all end
  /// here.
  void _requestLeave() {
    // Leaving now would let the profile page load before the save or the
    // picture is stored.
    if (_locked) return;
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
        scrollable: true,
        title: Text(l10n.languagesDiscardTitle),
        // The answers scroll with the text: over an open keyboard, at large
        // text on a small screen, the dialog is shorter than the two of
        // them, and `actions` stay outside what scrolls.
        content: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(l10n.profileDiscardMessage),
            const SizedBox(height: Spacing.md),
            OverflowBar(
              alignment: MainAxisAlignment.end,
              overflowAlignment: OverflowBarAlignment.end,
              spacing: Spacing.sm,
              children: [
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
          ],
        ),
      ),
    );
    if (discard != true || !mounted) return;
    _leave();
  }

  void _leave() {
    if (context.canPop()) {
      context.pop();
    } else {
      // Nothing below: the form is the profile's.
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
              semanticsLabel: l10n.profileLoading,
            ),
          ),
        ),
      ];
    } else if (loadError != null) {
      content = [
        FormErrorBanner(message: loadError),
        const SizedBox(height: Spacing.md),
        PrimaryButton(label: l10n.tryAgain, onPressed: _retry),
      ];
    } else {
      content = [
        Semantics(
          header: true,
          child: Text(
            _exists ? l10n.profileEditHeading : l10n.profileCreateHeading,
            style: theme.textTheme.headlineSmall,
          ),
        ),
        const SizedBox(height: Spacing.sm),
        Text(l10n.profileVisibilityNotice, style: theme.textTheme.bodyMedium),
        const SizedBox(height: Spacing.lg),
        ProfileAvatarEditor(
          session: widget.session,
          photoSource: widget.photoSource,
          name: _loadedName,
          enabled: !_busy,
          onBusyChanged: (busy) => setState(() => _pictureBusy = busy),
        ),
        const SizedBox(height: Spacing.lg),
        if (banner != null) ...[
          FormErrorBanner(message: banner),
          const SizedBox(height: Spacing.md),
        ],
        AppTextField.text(
          label: l10n.displayNameLabel,
          controller: _name,
          focusNode: _nameFocus,
          errorText: _nameError,
          enabled: !_locked,
          textInputAction: TextInputAction.next,
          onSubmitted: (_) => _bioFocus.requestFocus(),
        ),
        const SizedBox(height: Spacing.md),
        AppTextField.multiline(
          label: l10n.bioLabel,
          controller: _bio,
          focusNode: _bioFocus,
          errorText: _bioError,
          enabled: !_locked,
        ),
        const SizedBox(height: Spacing.lg),
        const Divider(height: 1),
        ListTile(
          contentPadding: EdgeInsets.zero,
          title: Text(l10n.profileEditLanguagesButton),
          trailing: const Icon(Icons.chevron_right),
          enabled: !_locked,
          onTap: () => unawaited(_openLanguages()),
        ),
        const Divider(height: 1),
        const SizedBox(height: Spacing.lg),
        PrimaryButton(
          label: l10n.profileSaveButton,
          onPressed: _pictureBusy ? null : _save,
          busy: _busy,
        ),
        const SizedBox(height: Spacing.md),
        SecondaryButton(
          label: l10n.profileCancelButton,
          onPressed: _locked ? null : _requestLeave,
        ),
      ];
    }

    return PopScope(
      // The back button and the system's back ask before the text is lost.
      canPop: !_changed && !_locked,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _requestLeave();
      },
      child: Scaffold(
        appBar: AppBar(title: Text(l10n.profileTitle)),
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
}

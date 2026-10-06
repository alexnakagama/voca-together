import 'dart:async';

import 'package:flutter/material.dart';

import '../api/profile.dart';
import '../l10n/app_localizations.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/app_text_field.dart';
import '../ui/widgets/auth_scaffold.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/form_notice_banner.dart';
import '../ui/widgets/primary_button.dart';
import 'failure_presentation.dart';
import 'profile_languages_section.dart';

/// The signed-in user's own profile: the name and the "about you" text other
/// members will see (`GET`/`PUT /v1/me/profile` through [SessionManager],
/// decision 028).
///
/// One form creates and edits: a user without a profile gets it empty. The
/// text is sent as typed and the form then shows what the server stored. It
/// never decides access; when the session ends the router leaves this screen
/// on its own.
///
/// Below the form, [ProfileLanguagesSection] shows the user's languages. It
/// loads and fails by itself, and the form's save doesn't touch it.
class ProfileScreen extends StatefulWidget {
  const ProfileScreen({super.key, required this.session});

  final SessionManager session;

  @override
  State<ProfileScreen> createState() => _ProfileScreenState();
}

class _ProfileScreenState extends State<ProfileScreen> {
  final _name = TextEditingController();
  final _bio = TextEditingController();
  final _nameFocus = FocusNode();
  final _bioFocus = FocusNode();

  /// The first load hasn't answered yet (or is being retried).
  bool _loading = true;
  String? _loadError;

  /// Whether the user has a saved profile; decides the heading.
  bool _exists = false;

  bool _busy = false;
  bool _saved = false;
  String? _banner;
  String? _nameError;
  String? _bioError;
  String _lastName = '';
  String _lastBio = '';

  /// Identifies the latest load; answers to earlier ones are dropped.
  int _request = 0;

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
    if (_nameError != null || _saved) {
      setState(() {
        _nameError = null;
        _saved = false;
      });
    }
  }

  void _bioEdited() {
    if (_bio.text == _lastBio) return;
    _lastBio = _bio.text;
    if (_bioError != null || _saved) {
      setState(() {
        _bioError = null;
        _saved = false;
      });
    }
  }

  /// Puts [profile]'s text in the form without it counting as an edit.
  void _show(Profile profile) {
    _lastName = profile.displayName;
    _lastBio = profile.bio;
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
    if (_busy) return;
    final l10n = AppLocalizations.of(context);
    if (_name.text.trim().isEmpty) {
      setState(() {
        _banner = null;
        _saved = false;
        _nameError = l10n.displayNameRequired;
      });
      _nameFocus.requestFocus();
      return;
    }

    setState(() {
      _busy = true;
      _saved = false;
      _banner = null;
      _nameError = null;
      _bioError = null;
    });
    try {
      // As typed: the server normalizes and validates (027).
      final stored = await widget.session.saveProfile(
        displayName: _name.text,
        bio: _bio.text,
      );
      if (!mounted) return;
      setState(() {
        _show(stored);
        _exists = true;
        _saved = true;
        _busy = false;
      });
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
        if (banner != null) ...[
          FormErrorBanner(message: banner),
          const SizedBox(height: Spacing.md),
        ],
        if (_saved) ...[
          FormNoticeBanner(message: l10n.profileSaved),
          const SizedBox(height: Spacing.md),
        ],
        AppTextField.text(
          label: l10n.displayNameLabel,
          controller: _name,
          focusNode: _nameFocus,
          errorText: _nameError,
          enabled: !_busy,
          textInputAction: TextInputAction.next,
          onSubmitted: (_) => _bioFocus.requestFocus(),
        ),
        const SizedBox(height: Spacing.md),
        AppTextField.multiline(
          label: l10n.bioLabel,
          controller: _bio,
          focusNode: _bioFocus,
          errorText: _bioError,
          enabled: !_busy,
        ),
        const SizedBox(height: Spacing.lg),
        PrimaryButton(
          label: l10n.profileSaveButton,
          onPressed: _save,
          busy: _busy,
        ),
      ];
    }

    return Scaffold(
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
                children: [
                  ...content,
                  // The languages load by themselves from the moment the
                  // screen opens, and are shown once the form is: the
                  // section keeps its state while it is hidden.
                  Visibility(
                    visible: !_loading && loadError == null,
                    maintainState: true,
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        const Divider(height: Spacing.xl * 2),
                        ProfileLanguagesSection(session: widget.session),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

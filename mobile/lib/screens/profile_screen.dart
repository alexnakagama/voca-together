import 'dart:async';

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../api/profile.dart';
import '../l10n/app_localizations.dart';
import '../router.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/auth_scaffold.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/primary_button.dart';
import '../ui/widgets/profile_header.dart';
import '../ui/widgets/secondary_button.dart';
import 'failure_presentation.dart';
import 'profile_languages_section.dart';

/// The signed-in user's own profile page, read-only: their picture's
/// placeholder, name and text as the server stored them (`GET
/// /v1/me/profile` through [SessionManager]), a Friends area that only says
/// the feature comes later, and their languages.
///
/// Nothing is changed from here: "Edit Profile" opens the edit screen, and
/// each time the member comes back, saved or not, the page drops what it
/// showed and loads again. A user without a profile is invited to create
/// one. It never decides access; when the session ends the router leaves
/// this screen on its own.
///
/// [ProfileLanguagesSection] is mounted once the profile has loaded. It
/// loads and fails by itself.
class ProfileScreen extends StatefulWidget {
  const ProfileScreen({super.key, required this.session});

  final SessionManager session;

  @override
  State<ProfileScreen> createState() => _ProfileScreenState();
}

class _ProfileScreenState extends State<ProfileScreen> {
  /// The load hasn't answered yet (or is being retried).
  bool _loading = true;
  String? _loadError;

  /// The profile as the server stored it; null with none saved, and until a
  /// load succeeds.
  Profile? _profile;

  /// Identifies the latest load; answers to earlier ones are dropped. The
  /// languages section is rebuilt for each one.
  int _request = 0;

  /// The edit screen was opened from here and hasn't been left yet.
  bool _editOpen = false;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  void _reload() {
    setState(() {
      _loading = true;
      _loadError = null;
      // What was shown may no longer be what is stored: if this load fails,
      // the page shows the failure, not the old profile.
      _profile = null;
    });
    unawaited(_load());
  }

  Future<void> _load() async {
    final request = ++_request;
    try {
      final profile = await widget.session.profile();
      if (!mounted || request != _request) return;
      setState(() {
        _profile = profile;
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

  /// Opens the edit screen on top of the page, and on coming back, saved or
  /// not, loads what the server has: a save whose answer was lost may still
  /// have been stored.
  Future<void> _openEdit() async {
    // A second tap before the screen covers the button would push it twice.
    if (_editOpen) return;
    _editOpen = true;
    await context.push<void>(Routes.profileEdit);
    _editOpen = false;
    if (!mounted) return;
    _reload();
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final theme = Theme.of(context);
    final loadError = _loadError;
    final profile = _profile;

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
        PrimaryButton(label: l10n.tryAgain, onPressed: _reload),
      ];
    } else if (profile == null) {
      content = [
        Text(l10n.profileEmptyMessage, style: theme.textTheme.bodyLarge),
        const SizedBox(height: Spacing.lg),
        PrimaryButton(
          label: l10n.profileEmptyButton,
          onPressed: () => unawaited(_openEdit()),
        ),
      ];
    } else {
      content = [
        ProfileHeader(
          name: profile.displayName,
          bio: profile.bio,
          avatarLabel: l10n.profileAvatarPlaceholderLabel,
        ),
        const SizedBox(height: Spacing.lg),
        SecondaryButton(
          label: l10n.profileEditButton,
          onPressed: () => unawaited(_openEdit()),
        ),
        const Divider(height: Spacing.xl * 2),
        Semantics(
          header: true,
          child: Text(
            l10n.profileFriendsHeading,
            style: theme.textTheme.titleLarge,
          ),
        ),
        const SizedBox(height: Spacing.sm),
        Text(
          l10n.profileFriendsComingLater,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const Divider(height: Spacing.xl * 2),
        // A new section for each load, so it loads again with the page.
        ProfileLanguagesSection(
          key: ValueKey(_request),
          session: widget.session,
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
                children: content,
              ),
            ),
          ),
        ),
      ),
    );
  }
}

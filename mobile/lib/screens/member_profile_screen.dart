import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../api/member_profile.dart';
import '../l10n/app_localizations.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/auth_scaffold.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/primary_button.dart';
import '../ui/widgets/profile_header.dart';
import 'failure_presentation.dart';
import 'profile_language_lists.dart';

/// A member's public profile, read-only (decision 032): their picture or
/// its placeholder, their name and text, and the languages they speak and
/// are learning, as any signed-in member may read them (`GET
/// /v1/profiles/{id}` through [SessionManager]).
///
/// [id] is the profile's public identifier, the only thing the route
/// carries. Nothing is changed from here, also when the profile is the
/// member's own: no edit control and no Friends area. It never decides
/// access; when the session ends the router leaves this screen on its own.
///
/// The profile and the catalog (for the languages' names) load together
/// and fail whole. A profile that doesn't exist is shown as unavailable,
/// with nothing to retry. The picture is asked for only when the profile
/// says there is one, and loads by itself: one that fails to load is the
/// placeholder, with no message.
class MemberProfileScreen extends StatefulWidget {
  const MemberProfileScreen({
    super.key,
    required this.session,
    required this.id,
  });

  final SessionManager session;

  /// The public identifier of the profile to show.
  final String id;

  @override
  State<MemberProfileScreen> createState() => _MemberProfileScreenState();
}

class _MemberProfileScreenState extends State<MemberProfileScreen> {
  /// The load hasn't answered yet (or is being retried).
  bool _loading = true;
  String? _loadError;

  /// The profile as the server returned it; null until a load succeeds, and
  /// after one that found no profile.
  MemberProfile? _profile;

  /// The catalog's English names by language code.
  Map<String, String> _names = const {};

  /// The member's picture; null with none, while it loads and when it
  /// couldn't be loaded.
  Uint8List? _image;

  /// Identifies the latest load; answers to earlier ones are dropped, the
  /// picture's too.
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
      final member = widget.session.memberProfile(widget.id);
      final catalog = widget.session.languageCatalog();
      // Waits for both, so neither is left to fail unobserved.
      await Future.wait<void>([member, catalog]);
      final profile = await member;
      final names = {
        for (final language in await catalog) language.code: language.name,
      };
      if (!mounted || request != _request) return;
      setState(() {
        _profile = profile;
        _names = names;
        _loading = false;
      });
      if (profile != null && profile.hasAvatar) {
        unawaited(_loadPicture(request));
      }
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

  /// Loads the picture for the profile that [request] loaded.
  Future<void> _loadPicture(int request) async {
    try {
      final image = await widget.session.memberAvatar(widget.id);
      if (!mounted || request != _request) return;
      setState(() => _image = image);
    } on Exception {
      // The placeholder stays, with no message: the rest of the profile is
      // what the member came for.
    }
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
              semanticsLabel: l10n.memberProfileLoading,
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
    } else if (profile == null) {
      // Unknown, removed or never saved: one answer, and asking again
      // wouldn't change it.
      content = [
        Text(l10n.memberProfileUnavailable, style: theme.textTheme.bodyLarge),
      ];
    } else {
      content = [
        ProfileHeader(
          name: profile.displayName,
          bio: profile.bio,
          avatarLabel: _image == null
              ? l10n.memberAvatarPlaceholderLabel(profile.displayName)
              : l10n.memberAvatarLabel(profile.displayName),
          image: _image,
        ),
        const Divider(height: Spacing.xl * 2),
        Semantics(
          header: true,
          child: Text(l10n.languagesHeading, style: theme.textTheme.titleLarge),
        ),
        const SizedBox(height: Spacing.md),
        ProfileLanguageLists(
          languages: profile.languages,
          names: _names,
          emptyText: l10n.memberLanguagesEmpty,
        ),
      ];
    }

    return Scaffold(
      appBar: AppBar(title: Text(l10n.memberProfileTitle)),
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

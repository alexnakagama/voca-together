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
/// carries. Nothing of the profile is changed from here, also when it is
/// the member's own: no edit control and no Friends area. It never decides
/// access; when the session ends the router leaves this screen on its own.
///
/// The profile, the catalog (for the languages' names) and the caller's own
/// profile (to know whether this one is theirs) load together and fail
/// whole. A profile that doesn't exist is shown as unavailable, with nothing
/// to retry. The picture is asked for only when the profile says there is
/// one, and loads by itself: one that fails to load is the placeholder, with
/// no message.
///
/// Another member's profile has a menu in the app bar with "Report" and
/// "Block". A block is confirmed first; once stored, the screen drops the
/// profile it held and says the member is blocked.
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

/// What the app bar's menu offers on another member's profile.
enum _MemberAction { report, block }

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

  /// Whether the profile shown is the caller's own. A caller with no
  /// profile is never the owner.
  bool _own = false;

  /// A block is being sent: the menu is disabled and leaving is held back.
  bool _busy = false;

  /// Why the last block failed, shown under the header.
  String? _blockError;

  /// The member was blocked from here: nothing of the profile is shown.
  bool _blocked = false;

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
      final own = widget.session.profile();
      // Waits for all three, so none is left to fail unobserved.
      await Future.wait<void>([member, catalog, own]);
      final profile = await member;
      final ownId = (await own)?.id;
      final names = {
        for (final language in await catalog) language.code: language.name,
      };
      if (!mounted || request != _request) return;
      setState(() {
        _profile = profile;
        _names = names;
        _own = profile != null && profile.id == ownId;
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

  /// Asks before blocking the member, then blocks them. Nothing is sent
  /// before the answer.
  Future<void> _confirmBlock() async {
    if (_busy) return;
    final l10n = AppLocalizations.of(context);
    final block = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        scrollable: true,
        title: Text(l10n.memberBlockTitle),
        // The answers scroll with the text, as in the profile form's
        // dialog: `actions` stay outside what scrolls.
        content: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(l10n.memberBlockMessage),
            const SizedBox(height: Spacing.md),
            OverflowBar(
              alignment: MainAxisAlignment.end,
              overflowAlignment: OverflowBarAlignment.end,
              spacing: Spacing.sm,
              children: [
                TextButton(
                  onPressed: () => Navigator.of(context).pop(false),
                  child: Text(l10n.memberBlockCancel),
                ),
                TextButton(
                  onPressed: () => Navigator.of(context).pop(true),
                  child: Text(l10n.memberBlockConfirm),
                ),
              ],
            ),
          ],
        ),
      ),
    );
    if (block != true || !mounted || _busy) return;

    setState(() {
      _busy = true;
      _blockError = null;
    });
    try {
      await widget.session.blockMember(widget.id);
      if (!mounted) return;
      // Drops a picture that is still loading.
      _request++;
      setState(() {
        _profile = null;
        _names = const {};
        _image = null;
        _blocked = true;
        _busy = false;
      });
    } on Exception catch (e) {
      if (!mounted) return;
      final failure = presentFailure(e, l10n);
      // The session is gone: the router is already taking the user to log
      // in, so an error here would only flash.
      if (failure.kind == FailureKind.sessionEnded) return;
      setState(() {
        _blockError = failure.blocksError ?? failure.message;
        _busy = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final theme = Theme.of(context);
    final loadError = _loadError;
    final profile = _profile;
    final blockError = _blockError;

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
    } else if (_blocked) {
      content = [Text(l10n.memberBlocked, style: theme.textTheme.bodyLarge)];
    } else if (profile == null) {
      // Unknown, removed or never saved: one answer, and asking again
      // wouldn't change it.
      content = [
        Text(l10n.memberProfileUnavailable, style: theme.textTheme.bodyLarge),
      ];
    } else {
      content = [
        if (_busy) ...[
          LinearProgressIndicator(semanticsLabel: l10n.memberBlockProgress),
          const SizedBox(height: Spacing.md),
        ],
        ProfileHeader(
          name: profile.displayName,
          bio: profile.bio,
          avatarLabel: _image == null
              ? l10n.memberAvatarPlaceholderLabel(profile.displayName)
              : l10n.memberAvatarLabel(profile.displayName),
          image: _image,
        ),
        if (blockError != null) ...[
          const SizedBox(height: Spacing.md),
          FormErrorBanner(message: blockError),
        ],
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

    // Only with another member's profile on screen: not on one's own, while
    // loading, on a failure, for an unavailable profile or once blocked.
    final hasMenu = !_loading && loadError == null && profile != null && !_own;

    return PopScope(
      // Leaving waits for the block's answer.
      canPop: !_busy,
      child: Scaffold(
        appBar: AppBar(
          title: Text(l10n.memberProfileTitle),
          actions: [
            if (hasMenu)
              PopupMenuButton<_MemberAction>(
                tooltip: l10n.memberMenuTooltip,
                enabled: !_busy,
                onSelected: (action) {
                  if (action == _MemberAction.block) unawaited(_confirmBlock());
                },
                itemBuilder: (context) => [
                  PopupMenuItem(
                    value: _MemberAction.report,
                    // The report screen isn't built yet.
                    enabled: false,
                    child: Text(l10n.memberMenuReport),
                  ),
                  PopupMenuItem(
                    value: _MemberAction.block,
                    child: Text(l10n.memberMenuBlock),
                  ),
                ],
              ),
          ],
        ),
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

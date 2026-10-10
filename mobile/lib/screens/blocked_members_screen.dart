import 'dart:async';

import 'package:flutter/material.dart';

import '../api/blocked_member.dart';
import '../l10n/app_localizations.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/auth_scaffold.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/primary_button.dart';
import 'confirm_dialog.dart';
import 'failure_presentation.dart';

/// The members the signed-in user has blocked (`GET /v1/me/blocks` through
/// [SessionManager]), most recently blocked first as the server returned
/// them, each with an "Unblock" control (decision 033).
///
/// The list is loaded on every entry and nothing of it is kept. An unblock
/// is confirmed first; once removed, the member leaves the list held here
/// without a reload. One unblock runs at a time. The screen never decides
/// access; when the session ends the router leaves it on its own.
///
/// The only text of a response it shows is a member's name.
class BlockedMembersScreen extends StatefulWidget {
  const BlockedMembersScreen({super.key, required this.session});

  final SessionManager session;

  @override
  State<BlockedMembersScreen> createState() => _BlockedMembersScreenState();
}

class _BlockedMembersScreenState extends State<BlockedMembersScreen> {
  /// The list as loaded, less the members unblocked since; null until a
  /// load succeeds.
  List<BlockedMember>? _members;
  String? _loadError;

  /// An unblock is being sent: every "Unblock" is disabled.
  bool _busy = false;

  /// Why the last unblock failed, shown above the list.
  String? _unblockError;

  /// Identifies the latest load; answers to earlier ones are dropped.
  int _request = 0;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  void _retry() {
    setState(() => _loadError = null);
    unawaited(_load());
  }

  Future<void> _load() async {
    final request = ++_request;
    try {
      final members = await widget.session.blockedMembers();
      if (!mounted || request != _request) return;
      setState(() => _members = members);
    } on Exception catch (e) {
      if (!mounted || request != _request) return;
      final failure = presentFailure(e, AppLocalizations.of(context));
      // The session is gone: the router is already taking the user to log
      // in, so an error here would only flash.
      if (failure.kind == FailureKind.sessionEnded) return;
      setState(() => _loadError = failure.message);
    }
  }

  /// Asks before unblocking [member], then unblocks them. Nothing is sent
  /// before the answer.
  Future<void> _confirmUnblock(BlockedMember member) async {
    if (_busy) return;
    final l10n = AppLocalizations.of(context);
    final unblock = await confirmDialog(
      context,
      title: l10n.unblockTitle,
      message: l10n.blockedMembersUnblockMessage(member.displayName),
      cancel: l10n.unblockCancel,
      confirm: l10n.unblockButton,
    );
    if (!unblock || !mounted || _busy) return;

    setState(() {
      _busy = true;
      _unblockError = null;
    });
    try {
      await widget.session.unblockMember(member.id);
      if (!mounted) return;
      setState(() {
        _members = [
          for (final m in _members ?? const <BlockedMember>[])
            if (m.id != member.id) m,
        ];
        _busy = false;
      });
    } on Exception catch (e) {
      if (!mounted) return;
      final failure = presentFailure(e, l10n);
      // The session is gone: the router is already taking the user to log
      // in, so an error here would only flash.
      if (failure.kind == FailureKind.sessionEnded) return;
      setState(() {
        _unblockError = failure.message;
        _busy = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final theme = Theme.of(context);
    final loadError = _loadError;
    final members = _members;
    final unblockError = _unblockError;

    final List<Widget> content;
    if (loadError != null) {
      content = [
        FormErrorBanner(message: loadError),
        const SizedBox(height: Spacing.md),
        PrimaryButton(label: l10n.tryAgain, onPressed: _retry),
      ];
    } else if (members == null) {
      content = [
        Padding(
          padding: const EdgeInsets.symmetric(vertical: Spacing.xl),
          child: Center(
            child: CircularProgressIndicator(
              semanticsLabel: l10n.blockedMembersLoading,
            ),
          ),
        ),
      ];
    } else {
      content = [
        if (_busy) ...[
          LinearProgressIndicator(semanticsLabel: l10n.unblockProgress),
          const SizedBox(height: Spacing.md),
        ],
        if (unblockError != null) ...[
          FormErrorBanner(message: unblockError),
          const SizedBox(height: Spacing.md),
        ],
        if (members.isEmpty)
          Text(l10n.blockedMembersEmpty, style: theme.textTheme.bodyLarge),
        for (final member in members)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: Spacing.sm),
            // The button drops under a name it doesn't fit beside.
            child: Wrap(
              alignment: WrapAlignment.spaceBetween,
              crossAxisAlignment: WrapCrossAlignment.center,
              spacing: Spacing.md,
              runSpacing: Spacing.sm,
              children: [
                Text(member.displayName, style: theme.textTheme.bodyLarge),
                OutlinedButton(
                  onPressed: _busy
                      ? null
                      : () => unawaited(_confirmUnblock(member)),
                  child: Text(
                    l10n.unblockButton,
                    semanticsLabel: l10n.blockedMembersUnblockLabel(
                      member.displayName,
                    ),
                  ),
                ),
              ],
            ),
          ),
      ];
    }

    return Scaffold(
      appBar: AppBar(title: Text(l10n.blockedMembersTitle)),
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

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../api/me.dart';
import '../l10n/app_localizations.dart';
import '../router.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/auth_scaffold.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/primary_button.dart';
import '../ui/widgets/secondary_button.dart';
import 'failure_presentation.dart';

/// The signed-in home screen: the account (`GET /v1/me` through
/// [SessionManager.me]), the way to the user's profile and to the members
/// they blocked, and logging out (decisions 024 and 028).
///
/// It never decides access. When the session ends, during a request or by
/// logging out, the router leaves this screen on its own.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key, required this.session});

  final SessionManager session;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  Me? _me;
  String? _error;
  bool _loggingOut = false;

  /// Identifies the latest load; answers to earlier ones are dropped.
  int _request = 0;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  void _retry() {
    setState(() => _error = null);
    unawaited(_load());
  }

  Future<void> _load() async {
    final request = ++_request;
    try {
      final me = await widget.session.me();
      if (!mounted || request != _request) return;
      setState(() => _me = me);
    } on Exception catch (e) {
      if (!mounted || request != _request) return;
      final failure = presentFailure(e, AppLocalizations.of(context));
      // The session is gone: the router is already taking the user to log
      // in, so an error here would only flash.
      if (failure.kind == FailureKind.sessionEnded) return;
      setState(() => _error = failure.message);
    }
  }

  // Pushed, so Android back returns here.
  void _openProfile() => unawaited(context.push(Routes.profile));

  void _openBlocked() => unawaited(context.push(Routes.blocked));

  Future<void> _logOut() async {
    if (_loggingOut) return;
    // Whatever a pending load returns no longer matters.
    _request++;
    setState(() => _loggingOut = true);
    // Never throws; the session goes signed out and the router moves on.
    await widget.session.logout();
    if (mounted) setState(() => _loggingOut = false);
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final theme = Theme.of(context);
    final me = _me;
    final error = _error;

    final Widget content;
    if (me != null) {
      content = Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Semantics(
            header: true,
            child: Text(
              l10n.homeSignedInTitle,
              style: theme.textTheme.headlineSmall,
            ),
          ),
          const SizedBox(height: Spacing.md),
          Text(l10n.homeSignedInAs(me.email), style: theme.textTheme.bodyLarge),
          const SizedBox(height: Spacing.sm),
          Text(
            l10n.homeMemberSince(me.createdAt.toLocal()),
            style: theme.textTheme.bodyMedium,
          ),
        ],
      );
    } else if (error != null) {
      content = Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          FormErrorBanner(message: error),
          const SizedBox(height: Spacing.md),
          PrimaryButton(
            label: l10n.tryAgain,
            onPressed: _loggingOut ? null : _retry,
          ),
        ],
      );
    } else {
      content = Padding(
        padding: const EdgeInsets.symmetric(vertical: Spacing.xl),
        child: Center(
          child: CircularProgressIndicator(semanticsLabel: l10n.homeLoading),
        ),
      );
    }

    return Scaffold(
      appBar: AppBar(title: Text(l10n.appTitle)),
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
                  content,
                  const SizedBox(height: Spacing.xl),
                  SecondaryButton(
                    label: l10n.profileButton,
                    onPressed: _loggingOut ? null : _openProfile,
                  ),
                  const SizedBox(height: Spacing.md),
                  SecondaryButton(
                    label: l10n.blockedMembersButton,
                    onPressed: _loggingOut ? null : _openBlocked,
                  ),
                  const SizedBox(height: Spacing.md),
                  SecondaryButton(
                    label: l10n.logOutButton,
                    onPressed: _logOut,
                    busy: _loggingOut,
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

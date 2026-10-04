import 'package:flutter/material.dart';

import '../l10n/app_localizations.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/google_sign_in_button.dart';
import 'failure_presentation.dart';

/// "Continue with Google" for the log-in and register screens: a divider,
/// the button and progress (decision 025).
///
/// All it does is call [SessionManager.signInWithGoogle] and hand a failure,
/// already put into words, to the host, which shows it where it shows its
/// own. It never sees a Google credential, and success needs no
/// navigation: the session becomes signed in and the router's redirect takes
/// over. A new account and a returning one look the same from here. Nothing
/// is retried automatically; every attempt is the user's and asks Google
/// again.
///
/// The host shows it only when [SessionManager.googleSignInAvailable], locks
/// its own form from [onStarted] until [onFailed], and passes [enabled] false
/// while its own request runs.
class GoogleSignInSection extends StatefulWidget {
  const GoogleSignInSection({
    super.key,
    required this.session,
    required this.enabled,
    required this.onStarted,
    required this.onFailed,
  });

  final SessionManager session;

  /// False while the host's own request runs.
  final bool enabled;

  /// A Google sign-in started: the host locks its form and drops what its
  /// last attempt said.
  final VoidCallback onStarted;

  /// It ended without a session: the host unlocks and shows the failure's
  /// message, if it has one (a closed account chooser has none). After a
  /// success nothing is called: the screen stays locked until the router
  /// replaces it.
  final ValueChanged<FailurePresentation> onFailed;

  @override
  State<GoogleSignInSection> createState() => _GoogleSignInSectionState();
}

class _GoogleSignInSectionState extends State<GoogleSignInSection> {
  bool _busy = false;

  Future<void> _signIn() async {
    // Two taps can arrive before the rebuild that disables the button.
    if (_busy || !widget.enabled) return;
    final l10n = AppLocalizations.of(context);
    setState(() => _busy = true);
    widget.onStarted();
    try {
      await widget.session.signInWithGoogle();
      // The router now leaves this screen; staying busy until then keeps the
      // form from flashing back on during the transition.
    } on Object catch (e) {
      // Errors too (a sign-in already running elsewhere, say): the user
      // gets a message and a usable form, never a stuck spinner.
      if (!mounted) return;
      setState(() => _busy = false);
      widget.onFailed(presentFailure(e, l10n));
    }
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            const Expanded(child: Divider()),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: Spacing.md),
              child: Text(
                l10n.googleSignInDivider,
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ),
            const Expanded(child: Divider()),
          ],
        ),
        const SizedBox(height: Spacing.md),
        GoogleSignInButton(
          onPressed: widget.enabled && !_busy ? _signIn : null,
        ),
        if (_busy) ...[
          const SizedBox(height: Spacing.md),
          Semantics(
            liveRegion: true,
            child: LinearProgressIndicator(
              semanticsLabel: l10n.googleSignInProgress,
            ),
          ),
        ],
      ],
    );
  }
}

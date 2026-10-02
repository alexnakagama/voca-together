import 'package:flutter/material.dart';

import '../api/account_api.dart';
import '../l10n/app_localizations.dart';
import '../ui/theme.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/form_notice_banner.dart';
import '../ui/widgets/secondary_button.dart';
import 'failure_presentation.dart';

/// Asks for a new verification email for [email]
/// (`POST /v1/auth/resend-verification`): a button, then the outcome.
///
/// Shown inline where the address is already known (log in after a 403,
/// registration's "check your email" view), so the address never travels in
/// a route. The confirmation reads the same whatever the account's state
/// (005). Nothing is retried automatically; after a success the button stays
/// available and the server's `account_mail` limit decides (018).
class ResendVerificationSection extends StatefulWidget {
  const ResendVerificationSection({
    super.key,
    required this.accountApi,
    required this.email,
  });

  final AccountApi accountApi;

  /// The address exactly as the user typed it; the server normalizes it.
  final String email;

  @override
  State<ResendVerificationSection> createState() =>
      _ResendVerificationSectionState();
}

class _ResendVerificationSectionState extends State<ResendVerificationSection> {
  bool _busy = false;
  bool _sent = false;
  String? _error;

  /// Identifies the latest request, so the answer to one made for an earlier
  /// address is dropped.
  int _request = 0;

  @override
  void didUpdateWidget(ResendVerificationSection oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.email != oldWidget.email) {
      _request++;
      _busy = false;
      _sent = false;
      _error = null;
    }
  }

  Future<void> _resend() async {
    if (_busy) return;
    final request = ++_request;
    setState(() {
      _busy = true;
      _sent = false;
      _error = null;
    });
    try {
      await widget.accountApi.resendVerification(email: widget.email);
      if (!mounted || request != _request) return;
      setState(() => _sent = true);
    } on Exception catch (e) {
      if (!mounted || request != _request) return;
      final failure = presentFailure(e, AppLocalizations.of(context));
      setState(() => _error = failure.message);
    } finally {
      if (mounted && request == _request) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final error = _error;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SecondaryButton(
          label: l10n.resendVerificationButton,
          onPressed: _resend,
          busy: _busy,
        ),
        if (_sent) ...[
          const SizedBox(height: Spacing.md),
          FormNoticeBanner(message: l10n.resendVerificationSent(widget.email)),
        ],
        if (error != null) ...[
          const SizedBox(height: Spacing.md),
          FormErrorBanner(message: error),
        ],
      ],
    );
  }
}

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../api/report_reason.dart';
import '../l10n/app_localizations.dart';
import '../router.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/app_text_field.dart';
import '../ui/widgets/auth_scaffold.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/form_notice_banner.dart';
import '../ui/widgets/primary_button.dart';
import 'failure_presentation.dart';

/// The form that reports a member (`PUT /v1/me/reports/{id}` through
/// [SessionManager], decision 034): one of the five reasons, optional
/// details, and a notice that the report is private.
///
/// [id] is the member's public identifier, the only thing the route
/// carries. The screen loads nothing, so it shows nothing of the member, and
/// nothing is ever read back: a report is write-only (033). Its only check
/// is that a reason is chosen; the details are sent exactly as typed.
///
/// Once sent, the form is replaced by a confirmation with the way back to
/// the profile. Reporting never blocks. A failure keeps the reason and the
/// text. While the request runs every control is disabled and leaving is
/// held back. The screen never decides access; when the session ends the
/// router leaves it on its own.
class ReportMemberScreen extends StatefulWidget {
  const ReportMemberScreen({
    super.key,
    required this.session,
    required this.id,
  });

  final SessionManager session;

  /// The public identifier of the member to report.
  final String id;

  @override
  State<ReportMemberScreen> createState() => _ReportMemberScreenState();
}

class _ReportMemberScreenState extends State<ReportMemberScreen> {
  final _details = TextEditingController();

  /// The reason chosen; none until the member chooses one.
  ReportReason? _reason;

  /// The report is being sent.
  bool _busy = false;

  /// The report was stored: the confirmation replaces the form.
  bool _sent = false;

  String? _banner;
  String? _reasonError;
  String? _detailsError;

  @override
  void dispose() {
    _details.dispose();
    super.dispose();
  }

  void _choose(ReportReason? reason) {
    if (_busy) return;
    setState(() {
      _reason = reason;
      _reasonError = null;
    });
  }

  Future<void> _send() async {
    if (_busy) return;
    final l10n = AppLocalizations.of(context);
    final reason = _reason;
    if (reason == null) {
      setState(() {
        _banner = null;
        _reasonError = l10n.errorReportReasonRequired;
        _detailsError = null;
      });
      return;
    }

    setState(() {
      _busy = true;
      _banner = null;
      _reasonError = null;
      _detailsError = null;
    });
    try {
      await widget.session.reportMember(
        widget.id,
        reason: reason,
        details: _details.text,
      );
      if (!mounted) return;
      setState(() {
        _sent = true;
        _busy = false;
      });
    } on Exception catch (e) {
      if (!mounted) return;
      final failure = presentFailure(e, l10n);
      // The session is gone: the router is already taking the user to log
      // in, so an error here would only flash.
      if (failure.kind == FailureKind.sessionEnded) return;
      setState(() {
        _banner = failure.message;
        _reasonError = failure.reasonError;
        _detailsError = failure.detailsError;
        _busy = false;
      });
    }
  }

  void _backToProfile() {
    if (context.canPop()) {
      context.pop();
    } else {
      // Nothing below: the report is about this member's profile.
      context.go(Routes.member(widget.id));
    }
  }

  String _label(ReportReason reason, AppLocalizations l10n) => switch (reason) {
    ReportReason.harassment => l10n.reportReasonHarassment,
    ReportReason.inappropriateContent => l10n.reportReasonInappropriateContent,
    ReportReason.spam => l10n.reportReasonSpam,
    ReportReason.impersonation => l10n.reportReasonImpersonation,
    ReportReason.other => l10n.reportReasonOther,
  };

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final theme = Theme.of(context);
    final banner = _banner;
    final reasonError = _reasonError;

    final List<Widget> content;
    if (_sent) {
      content = [
        FormNoticeBanner(message: l10n.reportSent),
        const SizedBox(height: Spacing.lg),
        PrimaryButton(
          label: l10n.reportBackToProfile,
          onPressed: _backToProfile,
        ),
      ];
    } else {
      content = [
        Text(l10n.reportPrivacyNotice, style: theme.textTheme.bodyMedium),
        const SizedBox(height: Spacing.lg),
        Semantics(
          header: true,
          child: Text(
            l10n.reportReasonHeading,
            style: theme.textTheme.titleMedium,
          ),
        ),
        RadioGroup<ReportReason>(
          groupValue: _reason,
          onChanged: _choose,
          child: Column(
            children: [
              for (final reason in ReportReason.values)
                RadioListTile<ReportReason>(
                  value: reason,
                  enabled: !_busy,
                  contentPadding: EdgeInsets.zero,
                  title: Text(_label(reason, l10n)),
                ),
            ],
          ),
        ),
        if (reasonError != null)
          Semantics(
            liveRegion: true,
            child: Text(
              reasonError,
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.error,
              ),
            ),
          ),
        const SizedBox(height: Spacing.md),
        AppTextField.multiline(
          label: l10n.reportDetailsLabel,
          controller: _details,
          errorText: _detailsError,
          enabled: !_busy,
        ),
        const SizedBox(height: Spacing.lg),
        // By the button, not at the top: the form is taller than a small
        // screen, and the failure belongs where the member just acted.
        if (banner != null) ...[
          FormErrorBanner(message: banner),
          const SizedBox(height: Spacing.md),
        ],
        PrimaryButton(
          label: l10n.reportSendButton,
          onPressed: () => unawaited(_send()),
          busy: _busy,
        ),
      ];
    }

    return PopScope(
      // Leaving waits for the answer to a report.
      canPop: !_busy,
      child: Scaffold(
        appBar: AppBar(title: Text(l10n.reportTitle)),
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

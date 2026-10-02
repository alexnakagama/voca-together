import 'dart:async';

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../api/account_api.dart';
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
import 'resend_verification.dart';

/// Email and password log in (decision 024).
///
/// Success needs no navigation here: the session becomes signed in and the
/// router's redirect takes over. The only client checks are empty fields;
/// the values are sent exactly as typed.
class LoginScreen extends StatefulWidget {
  const LoginScreen({
    super.key,
    required this.session,
    required this.accountApi,
  });

  final SessionManager session;
  final AccountApi accountApi;

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final _email = TextEditingController();
  final _password = TextEditingController();
  final _emailFocus = FocusNode();
  final _passwordFocus = FocusNode();

  bool _busy = false;
  String? _banner;
  String? _emailError;
  String? _passwordError;

  /// The address of an attempt refused with `email_not_verified`, while the
  /// screen offers to resend the link to it.
  String? _unverifiedEmail;

  String _lastEmail = '';
  String _lastPassword = '';

  @override
  void initState() {
    super.initState();
    _email.addListener(_emailEdited);
    _password.addListener(_passwordEdited);
  }

  @override
  void dispose() {
    _email.dispose();
    _password.dispose();
    _emailFocus.dispose();
    _passwordFocus.dispose();
    super.dispose();
  }

  // Listeners also fire for cursor moves; only a text change counts.
  void _emailEdited() {
    if (_email.text == _lastEmail) return;
    _lastEmail = _email.text;
    if (_emailError != null || _unverifiedEmail != null) {
      setState(() {
        _emailError = null;
        _unverifiedEmail = null;
      });
    }
  }

  void _passwordEdited() {
    if (_password.text == _lastPassword) return;
    _lastPassword = _password.text;
    if (_passwordError != null) setState(() => _passwordError = null);
  }

  Future<void> _submit() async {
    // The keyboard's done action bypasses the button's guard.
    if (_busy) return;
    final l10n = AppLocalizations.of(context);
    final email = _email.text;
    final password = _password.text;

    final emailError = email.trim().isEmpty ? l10n.emailRequired : null;
    final passwordError = password.isEmpty ? l10n.passwordRequired : null;
    if (emailError != null || passwordError != null) {
      setState(() {
        _banner = null;
        _unverifiedEmail = null;
        _emailError = emailError;
        _passwordError = passwordError;
      });
      (emailError != null ? _emailFocus : _passwordFocus).requestFocus();
      return;
    }

    setState(() {
      _busy = true;
      _banner = null;
      _emailError = null;
      _passwordError = null;
      _unverifiedEmail = null;
    });
    var signedIn = false;
    try {
      await widget.session.signIn(email: email, password: password);
      // The router now leaves this screen; staying busy until then keeps the
      // form from flashing back on during the transition.
      signedIn = true;
    } on Exception catch (e) {
      if (!mounted) return;
      _showFailure(presentFailure(e, l10n), email);
    } finally {
      if (!signedIn && mounted && _busy) setState(() => _busy = false);
    }
  }

  void _showFailure(FailurePresentation failure, String email) {
    FocusNode? focus;
    switch (failure.kind) {
      case FailureKind.invalidCredentials:
        // A definite answer about these credentials: retype the password.
        _password.clear();
        focus = _passwordFocus;
        _banner = failure.message;
      case FailureKind.emailNotVerified:
        _password.clear();
        _unverifiedEmail = email;
      case FailureKind.invalidInput:
        _emailError = failure.emailError;
        _passwordError = failure.passwordError;
        _banner = failure.message;
        focus = _emailError != null
            ? _emailFocus
            : (_passwordError != null ? _passwordFocus : null);
      default:
        // Says nothing about the credentials, and a retry is safe (018):
        // keep both fields.
        _banner = failure.message;
    }
    setState(() => _busy = false);
    // The fields are re-enabled by this rebuild; only then can they focus.
    final target = focus;
    if (target != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) target.requestFocus();
      });
    }
  }

  void _open(String route) => unawaited(context.push(route));

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final banner = _banner;
    final unverified = _unverifiedEmail;
    return AuthScaffold(
      title: l10n.logInTitle,
      children: [
        if (banner != null) FormErrorBanner(message: banner),
        if (unverified != null) ...[
          FormNoticeBanner(message: l10n.emailNotVerifiedNotice(unverified)),
          ResendVerificationSection(
            accountApi: widget.accountApi,
            email: unverified,
          ),
        ],
        AutofillGroup(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              AppTextField.email(
                controller: _email,
                focusNode: _emailFocus,
                errorText: _emailError,
                enabled: !_busy,
                textInputAction: TextInputAction.next,
                onSubmitted: (_) => _passwordFocus.requestFocus(),
              ),
              const SizedBox(height: Spacing.md),
              AppTextField.password(
                controller: _password,
                focusNode: _passwordFocus,
                errorText: _passwordError,
                enabled: !_busy,
                textInputAction: TextInputAction.done,
                onSubmitted: (_) => _submit(),
              ),
            ],
          ),
        ),
        PrimaryButton(label: l10n.logInButton, onPressed: _submit, busy: _busy),
        TextButton(
          onPressed: _busy ? null : () => _open(Routes.forgotPassword),
          child: Text(l10n.forgotPasswordLink),
        ),
        TextButton(
          onPressed: _busy ? null : () => _open(Routes.register),
          child: Text(l10n.createAccountLink),
        ),
      ],
    );
  }
}

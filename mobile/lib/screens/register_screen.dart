import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:go_router/go_router.dart';

import '../api/account_api.dart';
import '../l10n/app_localizations.dart';
import '../router.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/app_text_field.dart';
import '../ui/widgets/auth_scaffold.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/primary_button.dart';
import 'failure_presentation.dart';
import 'google_sign_in_section.dart';
import 'resend_verification.dart';

/// Registration (`POST /v1/auth/register`, decision 024).
///
/// The only client checks are empty fields and the confirmation matching;
/// the password policy and email normalization belong to the server, and
/// the values are sent exactly as typed. A 202 switches the screen to a
/// "check your email" view that reads the same whether or not the address
/// already had an account (005).
///
/// The form also offers "Continue with Google" when the build has it
/// (decision 025): for a new Google user that is the registration, and it
/// ends signed in rather than on the confirmation.
class RegisterScreen extends StatefulWidget {
  const RegisterScreen({
    super.key,
    required this.session,
    required this.accountApi,
  });

  final SessionManager session;
  final AccountApi accountApi;

  @override
  State<RegisterScreen> createState() => _RegisterScreenState();
}

class _RegisterScreenState extends State<RegisterScreen> {
  final _email = TextEditingController();
  final _password = TextEditingController();
  final _confirm = TextEditingController();
  final _emailFocus = FocusNode();
  final _passwordFocus = FocusNode();
  final _confirmFocus = FocusNode();

  bool _busy = false;

  /// A Google sign-in is running: the form is locked but not working.
  bool _googleBusy = false;
  String? _banner;
  String? _emailError;
  String? _passwordError;
  String? _confirmError;

  /// The address the accepted request was for; set once the form is done.
  String? _sentTo;

  late final _last = {_email: '', _password: '', _confirm: ''};

  @override
  void initState() {
    super.initState();
    for (final controller in [_email, _password, _confirm]) {
      controller.addListener(() => _edited(controller));
    }
  }

  @override
  void dispose() {
    for (final disposable in [
      _email,
      _password,
      _confirm,
      _emailFocus,
      _passwordFocus,
      _confirmFocus,
    ]) {
      disposable.dispose();
    }
    super.dispose();
  }

  /// Editing a field clears its error. Listeners also fire for cursor
  /// moves; only a text change counts.
  void _edited(TextEditingController controller) {
    if (controller.text == _last[controller]) return;
    _last[controller] = controller.text;
    setState(() {
      if (controller == _email) {
        _emailError = null;
      } else if (controller == _password) {
        _passwordError = null;
      } else {
        _confirmError = null;
      }
    });
  }

  Future<void> _submit() async {
    if (_busy || _googleBusy) return;
    final l10n = AppLocalizations.of(context);
    final email = _email.text;
    final password = _password.text;
    final confirm = _confirm.text;

    final emailError = email.trim().isEmpty ? l10n.emailRequired : null;
    final passwordError = password.isEmpty ? l10n.passwordRequired : null;
    final confirmError = confirm.isEmpty
        ? l10n.confirmPasswordRequired
        : (password.isNotEmpty && confirm != password
              ? l10n.passwordsDoNotMatch
              : null);
    if (emailError != null || passwordError != null || confirmError != null) {
      setState(() {
        _banner = null;
        _emailError = emailError;
        _passwordError = passwordError;
        _confirmError = confirmError;
      });
      (emailError != null
              ? _emailFocus
              : passwordError != null
              ? _passwordFocus
              : _confirmFocus)
          .requestFocus();
      return;
    }

    setState(() {
      _busy = true;
      _banner = null;
      _emailError = null;
      _passwordError = null;
      _confirmError = null;
    });
    try {
      await widget.accountApi.register(email: email, password: password);
      if (!mounted) return;
      // Let a password manager save the new password, then drop it from
      // memory before the form goes away.
      TextInput.finishAutofillContext();
      _password.clear();
      _confirm.clear();
      setState(() => _sentTo = email);
    } on Exception catch (e) {
      if (!mounted) return;
      final failure = presentFailure(e, l10n);
      // Every input is kept: the failure is about the request, or about one
      // field the user now fixes.
      setState(() {
        _banner = failure.message;
        _emailError = failure.emailError;
        _passwordError = failure.passwordError;
        _busy = false;
      });
      final focus = failure.emailError != null
          ? _emailFocus
          : (failure.passwordError != null ? _passwordFocus : null);
      if (focus != null) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) focus.requestFocus();
        });
      }
    } finally {
      if (mounted && _busy) setState(() => _busy = false);
    }
  }

  void _googleStarted() {
    setState(() {
      _googleBusy = true;
      // A new attempt: what the last one said no longer applies.
      _banner = null;
    });
  }

  void _googleFailed(FailurePresentation failure) {
    setState(() {
      _googleBusy = false;
      _banner = failure.message;
    });
  }

  void _backToLogIn() => context.go(Routes.login);

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final sentTo = _sentTo;
    if (sentTo != null) {
      return AuthScaffold(
        title: l10n.checkEmailTitle,
        children: [
          Semantics(
            liveRegion: true,
            child: Text(
              l10n.registerSent(sentTo),
              style: Theme.of(context).textTheme.bodyLarge,
            ),
          ),
          ResendVerificationSection(
            accountApi: widget.accountApi,
            email: sentTo,
          ),
          TextButton(onPressed: _backToLogIn, child: Text(l10n.backToLogIn)),
        ],
      );
    }

    final banner = _banner;
    final locked = _busy || _googleBusy;
    return AuthScaffold(
      title: l10n.registerTitle,
      children: [
        if (banner != null) FormErrorBanner(message: banner),
        AutofillGroup(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              AppTextField.email(
                controller: _email,
                focusNode: _emailFocus,
                errorText: _emailError,
                enabled: !locked,
                textInputAction: TextInputAction.next,
                onSubmitted: (_) => _passwordFocus.requestFocus(),
              ),
              const SizedBox(height: Spacing.md),
              AppTextField.password(
                controller: _password,
                focusNode: _passwordFocus,
                errorText: _passwordError,
                enabled: !locked,
                newPassword: true,
                textInputAction: TextInputAction.next,
                onSubmitted: (_) => _confirmFocus.requestFocus(),
              ),
              const SizedBox(height: Spacing.md),
              AppTextField.password(
                controller: _confirm,
                focusNode: _confirmFocus,
                label: l10n.confirmPasswordLabel,
                errorText: _confirmError,
                enabled: !locked,
                newPassword: true,
                textInputAction: TextInputAction.done,
                onSubmitted: (_) => _submit(),
              ),
            ],
          ),
        ),
        PrimaryButton(
          label: l10n.registerButton,
          onPressed: _googleBusy ? null : _submit,
          busy: _busy,
        ),
        if (widget.session.googleSignInAvailable)
          GoogleSignInSection(
            session: widget.session,
            enabled: !_busy,
            onStarted: _googleStarted,
            onFailed: _googleFailed,
          ),
        TextButton(
          onPressed: locked ? null : _backToLogIn,
          child: Text(l10n.haveAccountLink),
        ),
      ],
    );
  }
}

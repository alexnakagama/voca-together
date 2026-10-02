import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../api/account_api.dart';
import '../l10n/app_localizations.dart';
import '../router.dart';
import '../ui/widgets/app_text_field.dart';
import '../ui/widgets/auth_scaffold.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/primary_button.dart';
import 'failure_presentation.dart';

/// Asks for a password reset email (`POST /v1/auth/forgot-password`,
/// decision 024).
///
/// The reset itself happens on the page the emailed link opens (017); the
/// app never sees a reset token. The confirmation reads the same whether or
/// not the address has an account.
class ForgotPasswordScreen extends StatefulWidget {
  const ForgotPasswordScreen({super.key, required this.accountApi});

  final AccountApi accountApi;

  @override
  State<ForgotPasswordScreen> createState() => _ForgotPasswordScreenState();
}

class _ForgotPasswordScreenState extends State<ForgotPasswordScreen> {
  final _email = TextEditingController();
  final _emailFocus = FocusNode();

  bool _busy = false;
  String? _banner;
  String? _emailError;
  String? _sentTo;
  String _lastEmail = '';

  @override
  void initState() {
    super.initState();
    _email.addListener(_emailEdited);
  }

  @override
  void dispose() {
    _email.dispose();
    _emailFocus.dispose();
    super.dispose();
  }

  void _emailEdited() {
    if (_email.text == _lastEmail) return;
    _lastEmail = _email.text;
    if (_emailError != null) setState(() => _emailError = null);
  }

  Future<void> _submit() async {
    if (_busy) return;
    final l10n = AppLocalizations.of(context);
    final email = _email.text;
    if (email.trim().isEmpty) {
      setState(() {
        _banner = null;
        _emailError = l10n.emailRequired;
      });
      _emailFocus.requestFocus();
      return;
    }

    setState(() {
      _busy = true;
      _banner = null;
      _emailError = null;
    });
    try {
      await widget.accountApi.forgotPassword(email: email);
      if (!mounted) return;
      setState(() => _sentTo = email);
    } on Exception catch (e) {
      if (!mounted) return;
      final failure = presentFailure(e, l10n);
      setState(() {
        _banner = failure.message;
        _emailError = failure.emailError;
        _busy = false;
      });
      if (failure.emailError != null) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) _emailFocus.requestFocus();
        });
      }
    } finally {
      if (mounted && _busy) setState(() => _busy = false);
    }
  }

  void _backToLogIn() => context.go(Routes.login);

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final textTheme = Theme.of(context).textTheme;
    final sentTo = _sentTo;
    if (sentTo != null) {
      return AuthScaffold(
        title: l10n.checkEmailTitle,
        children: [
          Semantics(
            liveRegion: true,
            child: Text(
              l10n.forgotPasswordSent(sentTo),
              style: textTheme.bodyLarge,
            ),
          ),
          TextButton(onPressed: _backToLogIn, child: Text(l10n.backToLogIn)),
        ],
      );
    }

    final banner = _banner;
    return AuthScaffold(
      title: l10n.forgotPasswordTitle,
      children: [
        Text(l10n.forgotPasswordIntro, style: textTheme.bodyLarge),
        if (banner != null) FormErrorBanner(message: banner),
        AutofillGroup(
          child: AppTextField.email(
            controller: _email,
            focusNode: _emailFocus,
            errorText: _emailError,
            enabled: !_busy,
            textInputAction: TextInputAction.done,
            onSubmitted: (_) => _submit(),
          ),
        ),
        PrimaryButton(
          label: l10n.forgotPasswordButton,
          onPressed: _submit,
          busy: _busy,
        ),
        TextButton(
          onPressed: _busy ? null : _backToLogIn,
          child: Text(l10n.backToLogIn),
        ),
      ],
    );
  }
}

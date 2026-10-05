import 'package:flutter/material.dart';

import '../../l10n/app_localizations.dart';

/// A form field: an email address, a password, a line of text (a name) or
/// several lines of free text.
///
/// Presentation only: it neither trims nor validates. Pass [errorText] to show
/// an error for this field (e.g. one returned by the server); it wins over any
/// error from an enclosing `Form`.
class AppTextField extends StatefulWidget {
  /// An email address field: email keyboard, email autofill, no autocorrect.
  const AppTextField.email({
    super.key,
    this.controller,
    this.label,
    this.errorText,
    this.enabled = true,
    this.textInputAction,
    this.onSubmitted,
    this.focusNode,
  }) : _kind = _Kind.email,
       newPassword = false;

  /// A password field: obscured with a show/hide toggle, and with
  /// autocorrect, suggestions and keyboard learning off.
  ///
  /// Set [newPassword] when the user chooses a password (registration,
  /// reset) so password managers offer to generate and save one.
  const AppTextField.password({
    super.key,
    this.controller,
    this.label,
    this.errorText,
    this.enabled = true,
    this.textInputAction,
    this.onSubmitted,
    this.focusNode,
    this.newPassword = false,
  }) : _kind = _Kind.password;

  /// A single line of ordinary text, such as a person's name: capitalized
  /// words, the name keyboard, no autofill.
  const AppTextField.text({
    super.key,
    required String this.label,
    this.controller,
    this.errorText,
    this.enabled = true,
    this.textInputAction,
    this.onSubmitted,
    this.focusNode,
  }) : _kind = _Kind.text,
       newPassword = false;

  /// Several lines of free text: it starts three lines tall, grows with its
  /// content, and the keyboard's action key inserts a line break.
  const AppTextField.multiline({
    super.key,
    required String this.label,
    this.controller,
    this.errorText,
    this.enabled = true,
    this.focusNode,
  }) : _kind = _Kind.multiline,
       newPassword = false,
       textInputAction = TextInputAction.newline,
       onSubmitted = null;

  final TextEditingController? controller;

  /// Replaces the default label ("Email" or "Password"). Text and multiline
  /// fields have no default and always take one.
  final String? label;

  final String? errorText;
  final bool enabled;
  final TextInputAction? textInputAction;
  final ValueChanged<String>? onSubmitted;
  final FocusNode? focusNode;
  final bool newPassword;
  final _Kind _kind;

  @override
  State<AppTextField> createState() => _AppTextFieldState();
}

enum _Kind { email, password, text, multiline }

class _AppTextFieldState extends State<AppTextField> {
  bool _obscured = true;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    switch (widget._kind) {
      case _Kind.email:
        return TextFormField(
          controller: widget.controller,
          focusNode: widget.focusNode,
          enabled: widget.enabled,
          forceErrorText: widget.errorText,
          decoration: InputDecoration(
            labelText: widget.label ?? l10n.emailLabel,
          ),
          keyboardType: TextInputType.emailAddress,
          autofillHints: const [AutofillHints.email],
          autocorrect: false,
          textInputAction: widget.textInputAction,
          onFieldSubmitted: widget.onSubmitted,
        );
      case _Kind.password:
        final toggleLabel = _obscured ? l10n.showPassword : l10n.hidePassword;
        return TextFormField(
          controller: widget.controller,
          focusNode: widget.focusNode,
          enabled: widget.enabled,
          forceErrorText: widget.errorText,
          decoration: InputDecoration(
            labelText: widget.label ?? l10n.passwordLabel,
            suffixIcon: IconButton(
              tooltip: toggleLabel,
              icon: Icon(_obscured ? Icons.visibility : Icons.visibility_off),
              onPressed: widget.enabled
                  ? () => setState(() => _obscured = !_obscured)
                  : null,
            ),
          ),
          obscureText: _obscured,
          keyboardType: TextInputType.visiblePassword,
          autofillHints: [
            widget.newPassword
                ? AutofillHints.newPassword
                : AutofillHints.password,
          ],
          autocorrect: false,
          enableSuggestions: false,
          enableIMEPersonalizedLearning: false,
          smartDashesType: SmartDashesType.disabled,
          smartQuotesType: SmartQuotesType.disabled,
          textInputAction: widget.textInputAction,
          onFieldSubmitted: widget.onSubmitted,
        );
      case _Kind.text:
        return TextFormField(
          controller: widget.controller,
          focusNode: widget.focusNode,
          enabled: widget.enabled,
          forceErrorText: widget.errorText,
          decoration: InputDecoration(labelText: widget.label),
          keyboardType: TextInputType.name,
          textCapitalization: TextCapitalization.words,
          // Not the account holder's legal name: nothing to autofill.
          autofillHints: const [],
          textInputAction: widget.textInputAction,
          onFieldSubmitted: widget.onSubmitted,
        );
      case _Kind.multiline:
        return TextFormField(
          controller: widget.controller,
          focusNode: widget.focusNode,
          enabled: widget.enabled,
          forceErrorText: widget.errorText,
          decoration: InputDecoration(
            labelText: widget.label,
            alignLabelWithHint: true,
          ),
          keyboardType: TextInputType.multiline,
          textCapitalization: TextCapitalization.sentences,
          minLines: 3,
          maxLines: null,
          textInputAction: widget.textInputAction,
        );
    }
  }
}

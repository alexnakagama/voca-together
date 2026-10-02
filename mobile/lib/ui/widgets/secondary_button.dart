import 'package:flutter/material.dart';

/// A full-width outlined button for an action next to the main one, such as
/// "Send a new verification email" or "Log out".
///
/// The parent owns the request state: it passes [busy] while its request
/// runs, which shows a spinner, ignores taps and keeps the label laid out (and
/// announced) so the button keeps its size. The parent's handler must still
/// check its own busy flag: two taps can land before it rebuilds.
class SecondaryButton extends StatelessWidget {
  const SecondaryButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.busy = false,
  });

  final String label;
  final VoidCallback? onPressed;
  final bool busy;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return SizedBox(
      width: double.infinity,
      child: OutlinedButton(
        onPressed: busy ? null : onPressed,
        style: busy
            ? OutlinedButton.styleFrom(
                disabledForegroundColor: scheme.primary,
                side: BorderSide(color: scheme.outline),
              )
            : null,
        child: Stack(
          alignment: Alignment.center,
          children: [
            Opacity(
              opacity: busy ? 0 : 1,
              alwaysIncludeSemantics: true,
              child: Text(label, textAlign: TextAlign.center),
            ),
            if (busy)
              SizedBox.square(
                dimension: 20,
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  color: scheme.primary,
                ),
              ),
          ],
        ),
      ),
    );
  }
}

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

/// The full-width button that submits a form.
///
/// The parent owns the submission state: it passes [busy] while its request
/// runs, and a null [onPressed] when submitting isn't possible. A busy button
/// shows a spinner instead of [label] and ignores taps; it keeps its colors so
/// it reads as working rather than unavailable.
///
/// To guard against double submission, a tap is ignored if another one was
/// handled in the same frame (before the parent could rebuild with [busy]).
/// Other ways of submitting, such as the keyboard's done action, bypass the
/// button, so the parent's submit handler must check its own busy state too.
class PrimaryButton extends StatefulWidget {
  const PrimaryButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.busy = false,
  });

  final String label;
  final VoidCallback? onPressed;
  final bool busy;

  @override
  State<PrimaryButton> createState() => _PrimaryButtonState();
}

class _PrimaryButtonState extends State<PrimaryButton> {
  bool _latched = false;

  void _handleTap() {
    if (_latched) return;
    _latched = true;
    SchedulerBinding.instance
      ..addPostFrameCallback((_) => _latched = false)
      ..ensureVisualUpdate();
    widget.onPressed!();
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final busy = widget.busy;
    final enabled = widget.onPressed != null && !busy;

    return SizedBox(
      width: double.infinity,
      child: FilledButton(
        onPressed: enabled ? _handleTap : null,
        style: busy
            ? FilledButton.styleFrom(
                disabledBackgroundColor: scheme.primary,
                disabledForegroundColor: scheme.onPrimary,
              )
            : null,
        // The label stays laid out (and announced) while busy, so the button
        // keeps its size at any text scale.
        child: Stack(
          alignment: Alignment.center,
          children: [
            Opacity(
              opacity: busy ? 0 : 1,
              alwaysIncludeSemantics: true,
              child: Text(widget.label, textAlign: TextAlign.center),
            ),
            if (busy)
              SizedBox.square(
                dimension: 20,
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  color: scheme.onPrimary,
                ),
              ),
          ],
        ),
      ),
    );
  }
}

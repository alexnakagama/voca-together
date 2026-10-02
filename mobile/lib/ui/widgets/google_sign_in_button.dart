import 'package:flutter/material.dart';

import '../../l10n/app_localizations.dart';

/// The "Continue with Google" button, drawn to Google's sign-in branding
/// guidelines (decision 022). Presentation only: [onPressed] decides what
/// happens, and a null [onPressed] disables it.
///
/// Its colors are Google's light or dark button theme, following the app's
/// brightness, never the app's color scheme. The logo is Google's official
/// artwork with that theme's fill baked in, so it must not be resized,
/// recolored or placed on another background.
class GoogleSignInButton extends StatelessWidget {
  const GoogleSignInButton({super.key, required this.onPressed});

  final VoidCallback? onPressed;

  static const logoSize = 20.0;

  @override
  Widget build(BuildContext context) {
    final palette = Theme.of(context).brightness == Brightness.dark
        ? _GooglePalette.dark
        : _GooglePalette.light;
    final enabled = onPressed != null;

    final button = SizedBox(
      width: double.infinity,
      child: OutlinedButton(
        onPressed: onPressed,
        style: OutlinedButton.styleFrom(
          backgroundColor: palette.fill,
          foregroundColor: palette.text,
          disabledBackgroundColor: palette.fill,
          disabledForegroundColor: palette.text,
          side: BorderSide(color: palette.stroke),
          padding: const EdgeInsets.symmetric(horizontal: 12),
          textStyle: const TextStyle(
            fontSize: 14,
            height: 20 / 14,
            fontWeight: FontWeight.w500,
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            // Ink paints on the button's Material, so ripples and focus
            // highlights draw over the logo instead of stopping at its edges.
            Ink.image(
              image: AssetImage(palette.logo),
              width: logoSize,
              height: logoSize,
            ),
            const SizedBox(width: 10),
            Flexible(
              child: Text(
                AppLocalizations.of(context).continueWithGoogle,
                textAlign: TextAlign.center,
              ),
            ),
          ],
        ),
      ),
    );

    // Google defines no disabled state. Ours fades the whole button rather
    // than recoloring it, which the guidelines forbid for the logo.
    return enabled ? button : Opacity(opacity: 0.38, child: button);
  }
}

class _GooglePalette {
  const _GooglePalette({
    required this.fill,
    required this.stroke,
    required this.text,
    required this.logo,
  });

  final Color fill;
  final Color stroke;
  final Color text;
  final String logo;

  static const light = _GooglePalette(
    fill: Color(0xFFFFFFFF),
    stroke: Color(0xFF747775),
    text: Color(0xFF1F1F1F),
    logo: 'assets/google/g_logo_light.png',
  );

  static const dark = _GooglePalette(
    fill: Color(0xFF131314),
    stroke: Color(0xFF8E918F),
    text: Color(0xFFE3E3E3),
    logo: 'assets/google/g_logo_dark.png',
  );
}

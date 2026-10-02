import 'package:flutter/material.dart';

import '../../l10n/app_localizations.dart';
import '../theme.dart';

/// A neutral form-level notice, such as "we've sent you an email", shown
/// where a [FormErrorBanner] would be.
///
/// Like the error banner, an icon and the text carry the meaning, not only
/// the color, and screen readers announce the message when the banner
/// appears. Render it only while there is something to say.
class FormNoticeBanner extends StatelessWidget {
  const FormNoticeBanner({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Semantics(
      container: true,
      liveRegion: true,
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: scheme.secondaryContainer,
          borderRadius: BorderRadius.circular(Radii.card),
        ),
        child: Padding(
          padding: const EdgeInsets.all(Spacing.md),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(
                Icons.info_outline,
                color: scheme.onSecondaryContainer,
                semanticLabel: AppLocalizations.of(context).noticeLabel,
              ),
              const SizedBox(width: Spacing.sm),
              Expanded(
                child: Text(
                  message,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: scheme.onSecondaryContainer,
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

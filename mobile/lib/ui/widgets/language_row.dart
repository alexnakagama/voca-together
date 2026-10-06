import 'package:flutter/material.dart';

import '../theme.dart';

/// One language in an editable list: its [name] and [endonym], a button
/// showing the [level] that asks to change it, and a button that removes the
/// language.
///
/// Every text is passed in. The level button shows only [level], so screen
/// readers get [levelSemanticLabel] instead, which should name the language
/// too ("Spanish, level B2"); [removeLabel] is the remove button's tooltip
/// and label ("Remove Spanish"). The parent owns the list: a null callback
/// disables its button, as while a save runs.
class LanguageRow extends StatelessWidget {
  const LanguageRow({
    super.key,
    required this.name,
    required this.endonym,
    required this.level,
    required this.levelSemanticLabel,
    required this.removeLabel,
    required this.onLevelPressed,
    required this.onRemove,
  });

  final String name;
  final String endonym;
  final String level;
  final String levelSemanticLabel;
  final String removeLabel;
  final VoidCallback? onLevelPressed;
  final VoidCallback? onRemove;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    // A `Wrap`, so that when the name and the buttons don't fit side by side
    // (large text, a narrow screen) the buttons move below the name instead
    // of squeezing it.
    return Wrap(
      alignment: WrapAlignment.spaceBetween,
      crossAxisAlignment: WrapCrossAlignment.center,
      spacing: Spacing.sm,
      children: [
        MergeSemantics(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(name, style: theme.textTheme.titleMedium),
              // Some languages are named the same in English.
              if (endonym != name)
                Text(
                  endonym,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
            ],
          ),
        ),
        Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextButton.icon(
              onPressed: onLevelPressed,
              iconAlignment: IconAlignment.end,
              icon: const Icon(Icons.arrow_drop_down),
              label: Semantics(
                label: levelSemanticLabel,
                child: ExcludeSemantics(child: Text(level)),
              ),
            ),
            IconButton(
              onPressed: onRemove,
              tooltip: removeLabel,
              icon: const Icon(Icons.close),
            ),
          ],
        ),
      ],
    );
  }
}

import 'package:flutter/material.dart';

import '../theme.dart';

/// One language in an editable, ordered list: its [name] and [endonym], a
/// button showing the [level] that asks to change it, two buttons that move
/// the language up and down the list, and a button that removes it.
///
/// Every text is passed in. The level button shows only [level], so screen
/// readers get [levelSemanticLabel] instead, which should name the language
/// too ("Spanish, level B2"); [moveUpLabel], [moveDownLabel] and
/// [removeLabel] are their buttons' tooltips and labels ("Move Spanish up",
/// "Remove Spanish"). The parent owns the list: a null callback disables its
/// button, as while a save runs or for a move at the end of the list.
class LanguageRow extends StatelessWidget {
  const LanguageRow({
    super.key,
    required this.name,
    required this.endonym,
    required this.level,
    required this.levelSemanticLabel,
    required this.moveUpLabel,
    required this.moveDownLabel,
    required this.removeLabel,
    required this.onLevelPressed,
    required this.onMoveUp,
    required this.onMoveDown,
    required this.onRemove,
  });

  final String name;
  final String endonym;
  final String level;
  final String levelSemanticLabel;
  final String moveUpLabel;
  final String moveDownLabel;
  final String removeLabel;
  final VoidCallback? onLevelPressed;
  final VoidCallback? onMoveUp;
  final VoidCallback? onMoveDown;
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
        // A `Wrap` too: at large text the four buttons can be wider than
        // the screen.
        Wrap(
          crossAxisAlignment: WrapCrossAlignment.center,
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
              onPressed: onMoveUp,
              tooltip: moveUpLabel,
              icon: const Icon(Icons.arrow_upward),
            ),
            IconButton(
              onPressed: onMoveDown,
              tooltip: moveDownLabel,
              icon: const Icon(Icons.arrow_downward),
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

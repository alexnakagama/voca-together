import 'package:flutter/material.dart';

import '../theme.dart';

/// One language with the member's level in it, as a read-only pill: "Spanish
/// B2". Not a control: it takes no tap.
///
/// Both texts are passed in, the [name] from the catalog and the [level]
/// already localized. Screen readers get them as one item. A long name wraps
/// inside the pill rather than overflowing, so lay chips out in a `Wrap`.
class LanguageChip extends StatelessWidget {
  const LanguageChip({super.key, required this.name, required this.level});

  final String name;
  final String level;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final style = theme.textTheme.labelLarge?.copyWith(
      color: scheme.onSecondaryContainer,
    );
    return MergeSemantics(
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: scheme.secondaryContainer,
          borderRadius: BorderRadius.circular(Radii.card),
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(
            horizontal: Spacing.md,
            vertical: Spacing.sm,
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Flexible(child: Text(name, style: style)),
              const SizedBox(width: Spacing.sm),
              // The weight sets the level apart from the name, not a color.
              Text(level, style: style?.copyWith(fontWeight: FontWeight.w700)),
            ],
          ),
        ),
      ),
    );
  }
}

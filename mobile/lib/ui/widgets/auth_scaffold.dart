import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../theme.dart';

/// The page layout of the signed-out screens (log in, register, forgot
/// password): a [title] and the screen's [children] in one centered,
/// scrollable column.
///
/// The column scrolls when the keyboard, a small screen or large text leaves
/// too little room, and is centered vertically when everything fits.
class AuthScaffold extends StatelessWidget {
  const AuthScaffold({super.key, required this.title, required this.children});

  final String title;
  final List<Widget> children;

  /// Keeps lines readable on wide screens and in landscape.
  static const maxContentWidth = 480.0;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      body: SafeArea(
        child: LayoutBuilder(
          builder: (context, constraints) => SingleChildScrollView(
            keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
            padding: const EdgeInsets.all(Spacing.lg),
            child: ConstrainedBox(
              constraints: BoxConstraints(
                minHeight: math.max(0, constraints.maxHeight - 2 * Spacing.lg),
              ),
              child: Center(
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: maxContentWidth),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Semantics(
                        header: true,
                        child: Text(
                          title,
                          style: theme.textTheme.headlineSmall,
                        ),
                      ),
                      const SizedBox(height: Spacing.lg),
                      for (final (i, child) in children.indexed) ...[
                        if (i > 0) const SizedBox(height: Spacing.md),
                        child,
                      ],
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

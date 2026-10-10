import 'package:flutter/material.dart';

import '../ui/theme.dart';

/// Asks the user to confirm an action about another member (a block, an
/// unblock) and answers whether they did. Dismissing the dialog, by a tap
/// outside it or by back, is a "no".
///
/// The answers scroll with the text, as in the profile form's dialog:
/// `actions` stay outside what scrolls, and with large text on a small
/// screen they would cover it.
Future<bool> confirmDialog(
  BuildContext context, {
  required String title,
  required String message,
  required String cancel,
  required String confirm,
}) async {
  final answer = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      scrollable: true,
      title: Text(title),
      content: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(message),
          const SizedBox(height: Spacing.md),
          OverflowBar(
            alignment: MainAxisAlignment.end,
            overflowAlignment: OverflowBarAlignment.end,
            spacing: Spacing.sm,
            children: [
              TextButton(
                onPressed: () => Navigator.of(context).pop(false),
                child: Text(cancel),
              ),
              TextButton(
                onPressed: () => Navigator.of(context).pop(true),
                child: Text(confirm),
              ),
            ],
          ),
        ],
      ),
    ),
  );
  return answer ?? false;
}

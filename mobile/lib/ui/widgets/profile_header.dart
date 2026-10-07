import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../theme.dart';
import 'profile_avatar.dart';

/// The top of a profile: the picture or its placeholder, the member's
/// [name] as a heading and, when they wrote one, their [bio]. Read-only.
///
/// Every text is passed in: the name and the text as the server stored them,
/// [avatarLabel] already localized. An empty [bio] takes no space. Long
/// texts wrap, so put the header in something that scrolls.
class ProfileHeader extends StatelessWidget {
  const ProfileHeader({
    super.key,
    required this.name,
    required this.bio,
    required this.avatarLabel,
    this.image,
  });

  final String name;
  final String bio;

  /// What screen readers say for the picture or the placeholder.
  final String avatarLabel;

  /// The picture, or null for the placeholder.
  final Uint8List? image;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Center(
          child: ProfileAvatar(
            name: name,
            semanticLabel: avatarLabel,
            image: image,
          ),
        ),
        const SizedBox(height: Spacing.md),
        Semantics(
          header: true,
          child: Text(
            name,
            textAlign: TextAlign.center,
            style: theme.textTheme.headlineSmall,
          ),
        ),
        if (bio.isNotEmpty) ...[
          const SizedBox(height: Spacing.sm),
          Text(
            bio,
            style: theme.textTheme.bodyLarge?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ],
    );
  }
}

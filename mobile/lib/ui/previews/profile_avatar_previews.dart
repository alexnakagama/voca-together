import 'package:flutter/material.dart';

import '../theme.dart';
import '../widgets/profile_avatar.dart';
import 'preview_support.dart';

const _group = 'ProfileAvatar';

@VocaPreview(group: _group, name: 'Initial')
Widget profileAvatarInitial() => const Align(
  alignment: Alignment.centerLeft,
  child: ProfileAvatar(name: 'Ana', semanticLabel: 'Profile picture'),
);

@VocaPreview(group: _group, name: 'Picture')
Widget profileAvatarPicture() => Align(
  alignment: Alignment.centerLeft,
  child: ProfileAvatar(
    name: 'Ana',
    semanticLabel: 'Profile picture',
    image: previewPicture,
  ),
);

@VocaPreview(
  group: _group,
  name: 'Initials and no name, dark',
  brightness: Brightness.dark,
)
Widget profileAvatarInitials() => const Wrap(
  spacing: Spacing.sm,
  runSpacing: Spacing.sm,
  children: [
    ProfileAvatar(name: 'Ana', semanticLabel: 'Profile picture', size: 64),
    ProfileAvatar(name: '田中', semanticLabel: 'Profile picture', size: 64),
    ProfileAvatar(
      name: '👩‍👩‍👧 Li',
      semanticLabel: 'Profile picture',
      size: 64,
    ),
    ProfileAvatar(name: '', semanticLabel: 'No profile picture', size: 64),
  ],
);

@VocaPreview(
  group: _group,
  name: 'Large text',
  size: Size(320, double.infinity),
  textScaleFactor: 2,
)
Widget profileAvatarLargeText() => const Align(
  alignment: Alignment.centerLeft,
  child: ProfileAvatar(name: 'Ana', semanticLabel: 'Profile picture'),
);

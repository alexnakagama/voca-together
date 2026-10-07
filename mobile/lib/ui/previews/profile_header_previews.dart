import 'package:flutter/material.dart';

import '../widgets/profile_header.dart';
import 'preview_support.dart';

const _group = 'ProfileHeader';

@VocaPreview(group: _group, name: 'Picture, name and text')
Widget profileHeader() => ProfileHeader(
  name: 'Ana García',
  bio: 'Spanish teacher in Valencia. Learning Japanese for a trip next year.',
  avatarLabel: 'Profile picture of Ana García',
  image: previewPicture,
);

@VocaPreview(
  group: _group,
  name: 'No picture, no text, dark',
  brightness: Brightness.dark,
)
Widget profileHeaderNameOnly() => const ProfileHeader(
  name: 'Ana García',
  bio: '',
  avatarLabel: 'Profile picture of Ana García',
);

@VocaPreview(
  group: _group,
  name: 'Long name and text, large text',
  size: Size(320, double.infinity),
  textScaleFactor: 2,
)
Widget profileHeaderLargeText() => const ProfileHeader(
  name: 'Wolfeschlegelsteinhausenbergerdorff Maria-Magdalena',
  bio:
      'I teach German and Dutch in the evenings and I am looking for '
      'someone patient to practise Portuguese with.\n\nWeekends only.',
  avatarLabel: 'Profile picture',
);

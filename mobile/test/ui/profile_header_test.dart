import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/widgets/profile_avatar.dart';
import 'package:vocatogether/ui/widgets/profile_header.dart';

import '../support/pictures.dart';
import 'harness.dart';

const _label = 'Profile picture';
const _longName = 'Wolfeschlegelsteinhausenbergerdorff Maria-Magdalena';
const _longBio =
    'I teach German and Dutch in the evenings and I am looking for someone '
    'patient to practise Portuguese with, ideally twice a week.\n\n'
    'Pneumonoultramicroscopicsilicovolcanoconiosis is my favourite word.';

Finder get _header => find.byType(ProfileHeader);
ProfileAvatar _avatarOf(WidgetTester tester) =>
    tester.widget<ProfileAvatar>(find.byType(ProfileAvatar));

void main() {
  testWidgets('shows the placeholder, the name and the text', (tester) async {
    await pumpUi(
      tester,
      const ProfileHeader(
        name: 'Ana García',
        bio: 'Learning Japanese.',
        avatarLabel: _label,
      ),
    );

    expect(find.text('Ana García'), findsOneWidget);
    expect(find.text('Learning Japanese.'), findsOneWidget);
    final avatar = _avatarOf(tester);
    expect(avatar.name, 'Ana García');
    expect(avatar.semanticLabel, _label);
    expect(avatar.image, isNull);
    expect(find.text('A'), findsOneWidget);
    // The picture is above the name, and the text below it.
    final picture = tester.getRect(find.byType(ProfileAvatar));
    final name = tester.getRect(find.text('Ana García'));
    final bio = tester.getRect(find.text('Learning Japanese.'));
    expect(picture.bottom, lessThanOrEqualTo(name.top));
    expect(name.bottom, lessThanOrEqualTo(bio.top));
  });

  testWidgets('shows the picture it is given', (tester) async {
    await pumpUi(
      tester,
      ProfileHeader(
        name: 'Ana',
        bio: '',
        avatarLabel: _label,
        image: testPicture,
      ),
    );
    await pumpDecoded(tester, testPicture);

    expect(_avatarOf(tester).image, same(testPicture));
    expect(tester.widget<RawImage>(find.byType(RawImage)).image, isNotNull);
    expect(find.text('A'), findsNothing);
  });

  testWidgets('a picture that fails to decode leaves the rest as it is', (
    tester,
  ) async {
    await pumpUi(
      tester,
      ProfileHeader(
        name: 'Ana',
        bio: 'Learning Japanese.',
        avatarLabel: _label,
        image: brokenPicture,
      ),
    );
    await pumpDecoded(tester, brokenPicture);

    expect(find.text('A'), findsOneWidget);
    expect(find.text('Ana'), findsOneWidget);
    expect(find.text('Learning Japanese.'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('no text leaves no empty space or label', (tester) async {
    await pumpUi(
      tester,
      const ProfileHeader(name: 'Ana', bio: '', avatarLabel: _label),
    );

    // The initial and the name are the only texts.
    expect(
      find.descendant(of: _header, matching: find.byType(Text)),
      findsNWidgets(2),
    );
    expect(
      tester.getRect(_header).bottom,
      tester.getRect(find.text('Ana')).bottom,
    );
  });

  testWidgets('the text is shown as stored, line breaks included', (
    tester,
  ) async {
    const bio = '  Two spaces first.\n\nThen a second paragraph.';
    await pumpUi(
      tester,
      const ProfileHeader(name: 'Ana', bio: bio, avatarLabel: _label),
    );

    expect(tester.widget<Text>(find.text(bio)).maxLines, isNull);
    expect(find.text(bio), findsOneWidget);
  });

  testWidgets('the picture, then the name as a heading, then the text', (
    tester,
  ) async {
    final handle = tester.ensureSemantics();
    await pumpUi(
      tester,
      const ProfileHeader(
        name: 'Ana García',
        bio: 'Learning Japanese.',
        avatarLabel: 'Profile picture of Ana García',
      ),
    );

    expect(
      tester.getSemantics(find.byType(ProfileAvatar)),
      isSemantics(isImage: true, label: 'Profile picture of Ana García'),
    );
    expect(
      tester.getSemantics(find.text('Ana García')),
      isSemantics(isHeader: true, label: 'Ana García'),
    );
    expect(
      tester.getSemantics(find.text('Learning Japanese.')),
      isSemantics(isHeader: false, label: 'Learning Japanese.'),
    );
    handle.dispose();
  });

  for (final brightness in Brightness.values) {
    testWidgets('readable in the $brightness theme', (tester) async {
      final handle = tester.ensureSemantics();
      await pumpUi(
        tester,
        const ProfileHeader(
          name: 'Ana García',
          bio: 'Learning Japanese.',
          avatarLabel: _label,
        ),
        brightness: brightness,
      );
      final scheme = Theme.of(tester.element(_header)).colorScheme;

      for (final text in ['Ana García', 'Learning Japanese.']) {
        final color =
            tester.widget<Text>(find.text(text)).style?.color ??
            DefaultTextStyle.of(tester.element(find.text(text))).style.color!;
        expect(
          contrastRatio(color, scheme.surface),
          greaterThanOrEqualTo(4.5),
          reason: text,
        );
      }
      await expectLater(tester, meetsGuideline(textContrastGuideline));
      handle.dispose();
    });
  }

  for (final picture in [false, true]) {
    testWidgets('a long name and text wrap at large text on a small screen '
        '(${picture ? 'picture' : 'placeholder'})', (tester) async {
      await pumpUi(
        tester,
        SingleChildScrollView(
          child: ProfileHeader(
            name: _longName,
            bio: _longBio,
            avatarLabel: _label,
            image: picture ? testPicture : null,
          ),
        ),
        size: const Size(320, 480),
        textScale: 2,
      );
      if (picture) await pumpDecoded(tester, testPicture);

      expect(tester.takeException(), isNull);
      expect(tester.getSize(_header).width, lessThanOrEqualTo(320));
      for (final text in [_longName, _longBio]) {
        final rect = tester.getRect(find.text(text));
        expect(rect.left, greaterThanOrEqualTo(0), reason: text);
        expect(rect.right, lessThanOrEqualTo(320), reason: text);
      }
      // Taller than the screen: it scrolls, and the end can be reached.
      await tester.ensureVisible(find.text(_longBio));
      await tester.pump();
      expect(tester.takeException(), isNull);
    });
  }
}

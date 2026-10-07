import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter/semantics.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/widgets/profile_avatar.dart';

import '../support/pictures.dart';
import 'harness.dart';

const _label = 'Profile picture';

Finder get _avatar => find.byType(ProfileAvatar);

/// The decoded picture the avatar paints, or null while it shows none.
Object? _painted(WidgetTester tester) {
  final images = find.descendant(of: _avatar, matching: find.byType(RawImage));
  return images.evaluate().isEmpty
      ? null
      : tester.widget<RawImage>(images).image;
}

void main() {
  group('the picture', () {
    testWidgets('is shown from its bytes, round, with no initial', (
      tester,
    ) async {
      await pumpUi(
        tester,
        ProfileAvatar(name: 'Ana', semanticLabel: _label, image: testPicture),
      );
      await pumpDecoded(tester, testPicture);

      expect(_painted(tester), isNotNull);
      expect(find.text('A'), findsNothing);
      expect(
        find.descendant(of: _avatar, matching: find.byType(ClipOval)),
        findsOneWidget,
      );
      expect(tester.getSize(_avatar), const Size.square(96));
      expect(tester.takeException(), isNull);
    });

    testWidgets('fills the circle whatever its own size', (tester) async {
      await pumpUi(
        tester,
        ProfileAvatar(
          name: 'Ana',
          semanticLabel: _label,
          image: testPicture,
          size: 64,
        ),
      );
      await pumpDecoded(tester, testPicture);

      final image = tester.widget<Image>(
        find.descendant(of: _avatar, matching: find.byType(Image)),
      );
      expect(image.fit, BoxFit.cover);
      expect((image.width, image.height), (64, 64));
      expect(tester.getSize(_avatar), const Size.square(64));
    });

    testWidgets('bytes that fail to decode fall back to the initial', (
      tester,
    ) async {
      await pumpUi(
        tester,
        ProfileAvatar(name: 'Ana', semanticLabel: _label, image: brokenPicture),
      );
      await pumpDecoded(tester, brokenPicture);

      expect(find.text('A'), findsOneWidget);
      expect(_painted(tester), isNull);
      // The failure is the widget's to absorb: nothing is reported.
      expect(tester.takeException(), isNull);
    });

    testWidgets('empty bytes fall back to the initial', (tester) async {
      final empty = Uint8List(0);
      await pumpUi(
        tester,
        ProfileAvatar(name: 'Ana', semanticLabel: _label, image: empty),
      );
      await pumpDecoded(tester, empty);

      expect(find.text('A'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });
  });

  group('the initial', () {
    const names = {
      'Latin': ('Ana', 'A'),
      'lower case, kept as written': ('ana', 'a'),
      'CJK': ('田中 太郎', '田'),
      'an emoji': ('😀 Sam', '😀'),
      'an emoji made of several code points': ('👩‍👩‍👧 Li', '👩‍👩‍👧'),
      'a flag': ('🇯🇵 Yuki', '🇯🇵'),
      'a letter with a combining mark': ('élodie', 'é'),
      'Hangul written as jamo': ('한글', '한'),
      'leading spaces': ('  Ana', 'A'),
    };
    names.forEach((description, value) {
      final (name, initial) = value;
      testWidgets(description, (tester) async {
        await pumpUi(tester, ProfileAvatar(name: name, semanticLabel: _label));

        expect(
          find.descendant(of: _avatar, matching: find.text(initial)),
          findsOneWidget,
        );
        expect(_painted(tester), isNull);
        expect(tester.takeException(), isNull);
      });
    });

    for (final name in ['', '   ']) {
      testWidgets('no name ("$name") shows an icon, not a letter', (
        tester,
      ) async {
        await pumpUi(tester, ProfileAvatar(name: name, semanticLabel: _label));

        expect(
          find.descendant(of: _avatar, matching: find.byType(Text)),
          findsNothing,
        );
        expect(
          find.descendant(of: _avatar, matching: find.byIcon(Icons.person)),
          findsOneWidget,
        );
      });
    }

    testWidgets('a wide initial stays inside the circle', (tester) async {
      await pumpUi(
        tester,
        const ProfileAvatar(
          name: '👩‍👩‍👧 Li',
          semanticLabel: _label,
          size: 40,
        ),
      );

      final circle = tester.getRect(_avatar);
      final text = tester.getRect(find.text('👩‍👩‍👧'));
      expect(circle.size, const Size.square(40));
      expect(circle.contains(text.topLeft), isTrue);
      expect(circle.contains(text.bottomRight), isTrue);
      expect(tester.takeException(), isNull);
    });

    testWidgets('does not grow with the system text size', (tester) async {
      await pumpUi(
        tester,
        const ProfileAvatar(name: 'Ana', semanticLabel: _label),
      );
      final normal = tester.getSize(find.text('A'));

      await pumpUi(
        tester,
        const ProfileAvatar(name: 'Ana', semanticLabel: _label),
        textScale: 2,
        size: const Size(320, 480),
      );

      expect(tester.getSize(find.text('A')), normal);
      expect(tester.getSize(_avatar), const Size.square(96));
      expect(tester.takeException(), isNull);
    });
  });

  group('semantics', () {
    testWidgets('one image with the label it was given', (tester) async {
      final handle = tester.ensureSemantics();
      await pumpUi(
        tester,
        const ProfileAvatar(name: 'Ana', semanticLabel: 'Ana, profile picture'),
      );

      expect(
        tester.getSemantics(_avatar),
        isSemantics(isImage: true, label: 'Ana, profile picture'),
      );
      // The initial is part of the picture, not a text of its own.
      expect(find.bySemanticsLabel('A'), findsNothing);
      handle.dispose();
    });

    testWidgets('the same with a picture', (tester) async {
      final handle = tester.ensureSemantics();
      await pumpUi(
        tester,
        ProfileAvatar(name: 'Ana', semanticLabel: _label, image: testPicture),
      );
      await pumpDecoded(tester, testPicture);

      expect(
        tester.getSemantics(_avatar),
        isSemantics(isImage: true, label: _label),
      );
      handle.dispose();
    });

    testWidgets('is not a control', (tester) async {
      final handle = tester.ensureSemantics();
      await pumpUi(
        tester,
        const ProfileAvatar(name: 'Ana', semanticLabel: _label),
      );

      final data = tester.getSemantics(_avatar).getSemanticsData();
      expect(data.flagsCollection.isButton, isFalse);
      expect(data.hasAction(SemanticsAction.tap), isFalse);
      handle.dispose();
    });
  });

  for (final brightness in Brightness.values) {
    testWidgets('the placeholder is readable ($brightness)', (tester) async {
      await pumpUi(
        tester,
        const Wrap(
          children: [
            ProfileAvatar(name: 'Ana', semanticLabel: _label),
            ProfileAvatar(name: '', semanticLabel: _label),
          ],
        ),
        brightness: brightness,
      );
      final scheme = Theme.of(tester.element(_avatar.first)).colorScheme;

      for (final box in tester.widgetList<ColoredBox>(
        find.descendant(of: _avatar, matching: find.byType(ColoredBox)),
      )) {
        expect(box.color, scheme.primaryContainer);
      }
      final letter = tester.widget<Text>(find.text('A')).style!.color!;
      final icon = tester.widget<Icon>(find.byIcon(Icons.person)).color!;
      expect(letter, scheme.onPrimaryContainer);
      expect(icon, scheme.onPrimaryContainer);
      expect(
        contrastRatio(letter, scheme.primaryContainer),
        greaterThanOrEqualTo(4.5),
      );
      // The circle itself stands out from the page.
      expect(
        contrastRatio(scheme.primaryContainer, scheme.surface),
        greaterThan(1.1),
      );
    });
  }
}

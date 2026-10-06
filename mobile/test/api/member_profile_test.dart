import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/languages.dart';
import 'package:vocatogether/api/member_profile.dart';
import 'package:vocatogether/api/profile.dart';

import '../support/fakes.dart';

final _malformed = isA<ApiProtocolException>()
    .having((e) => e.failure, 'failure', ProtocolFailure.malformedBody)
    .having((e) => e.statusCode, 'statusCode', 200);

void main() {
  group('MemberProfile.fromJson', () {
    final body = memberProfileBody(
      displayName: 'Ana López',
      bio: 'Learning Japanese.',
      hasAvatar: true,
      languages: languagesBody(
        spoken: [('es', 'native'), ('en', 'c1')],
        learning: [('ja', 'a2')],
      ),
    );

    test('parses every public field, the languages in order', () {
      final profile = MemberProfile.fromJson(body);
      expect(profile.id, testMemberId);
      expect(profile.displayName, 'Ana López');
      expect(profile.bio, 'Learning Japanese.');
      expect(profile.hasAvatar, isTrue);
      expect(
        profile.languages,
        UserLanguages(
          spoken: const [
            UserLanguage('es', LanguageLevel.native),
            UserLanguage('en', LanguageLevel.c1),
          ],
          learning: const [UserLanguage('ja', LanguageLevel.a2)],
        ),
      );
    });

    test('parses what jsonDecode gives', () {
      final profile = MemberProfile.fromJson(jsonDecode(jsonEncode(body)));
      expect(profile.displayName, 'Ana López');
      expect(profile.languages.spoken, hasLength(2));
    });

    test('no text, no picture and no languages is a profile', () {
      final profile = MemberProfile.fromJson(memberProfileBody());
      expect(profile.bio, '');
      expect(profile.hasAvatar, isFalse);
      expect(profile.languages.spoken, isEmpty);
      expect(profile.languages.learning, isEmpty);
    });

    test('anything off-contract fails the whole response', () {
      Map<String, Object?> languages(Object? spoken, Object? learning) => {
        'spoken': spoken,
        'learning': learning,
      };
      const entry = {'language': 'es', 'level': 'b1'};
      final cases = <String, Object?>{
        'no id': {...body}..remove('id'),
        'an empty id': {...body, 'id': ''},
        'a null id': {...body, 'id': null},
        'a numeric id': {...body, 'id': 7},
        'no name': {...body}..remove('display_name'),
        'an empty name': {...body, 'display_name': ''},
        'no bio': {...body}..remove('bio'),
        'a null bio': {...body, 'bio': null},
        'no has_avatar': {...body}..remove('has_avatar'),
        'a null has_avatar': {...body, 'has_avatar': null},
        'has_avatar as text': {...body, 'has_avatar': 'true'},
        'has_avatar as a number': {...body, 'has_avatar': 1},
        'no languages': {...body}..remove('languages'),
        'null languages': {...body, 'languages': null},
        'languages as a list': {...body, 'languages': <Object?>[]},
        'no spoken list': {
          ...body,
          'languages': {'learning': <Object?>[]},
        },
        'a null list': {...body, 'languages': languages(null, <Object?>[])},
        'a null learning list': {
          ...body,
          'languages': languages(<Object?>[], null),
        },
        'an unknown level': {
          ...body,
          'languages': languages([
            entry,
            {'language': 'en', 'level': 'd1'},
          ], <Object?>[]),
        },
        'a level in another case': {
          ...body,
          'languages': languages(<Object?>[], [
            {'language': 'en', 'level': 'B1'},
          ]),
        },
        'an entry with no code': {
          ...body,
          'languages': languages([
            {'language': '', 'level': 'b1'},
          ], <Object?>[]),
        },
        'a list around it': <Object?>[body],
        'null': null,
      };
      cases.forEach((name, json) {
        expect(
          () => MemberProfile.fromJson(json),
          throwsA(_malformed),
          reason: name,
        );
      });
    });

    test('toString is redacted', () {
      final text = '${MemberProfile.fromJson(body)}';
      expect(text, 'MemberProfile(<redacted>)');
      for (final secret in [testMemberId, 'Ana', 'Japanese', 'es', 'native']) {
        expect(text, isNot(contains(secret)));
      }
    });
  });

  group('Profile.fromJson', () {
    test('holds the public identifier', () {
      expect(Profile.fromJson(profileBody()).id, testMemberId);
      expect(Profile.fromJson(profileBody(id: 'x')).id, 'x');
    });

    test('a missing or empty id fails the whole response', () {
      for (final json in [
        profileBody()..remove('id'),
        profileBody(id: ''),
        {...profileBody(), 'id': null},
        {...profileBody(), 'id': 12},
      ]) {
        expect(() => Profile.fromJson(json), throwsA(_malformed));
      }
    });

    test('toString is redacted', () {
      final text = '${Profile.fromJson(profileBody(displayName: 'Ana'))}';
      expect(text, 'Profile(<redacted>)');
      expect(text, isNot(contains(testMemberId)));
    });
  });
}

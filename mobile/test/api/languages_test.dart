import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/languages.dart';

final _malformed = throwsA(
  isA<ApiProtocolException>()
      .having((e) => e.failure, 'failure', ProtocolFailure.malformedBody)
      .having((e) => e.statusCode, 'statusCode', 200),
);

const _es = {'language': 'es', 'level': 'native'};
const _en = {'language': 'en', 'level': 'c1'};
const _ja = {'language': 'ja', 'level': 'a2'};

void main() {
  group('LanguageLevel', () {
    test('has the seven API identifiers, in the scale\'s order', () {
      expect(LanguageLevel.values.map((l) => l.wire), [
        'a1',
        'a2',
        'b1',
        'b2',
        'c1',
        'c2',
        'native',
      ]);
    });

    test('parses each identifier back', () {
      for (final level in LanguageLevel.values) {
        expect(LanguageLevel.tryParse(level.wire), level);
      }
    });

    // Identifiers, not text: nothing is trimmed or case-folded (029).
    test('knows nothing but the exact identifiers', () {
      for (final wire in ['', 'A1', ' a1', 'a1 ', 'Native', 'c3', 'a', '1']) {
        expect(LanguageLevel.tryParse(wire), isNull, reason: '"$wire"');
      }
    });
  });

  group('Language', () {
    const body = {
      'languages': [
        {'code': 'en', 'name': 'English', 'endonym': 'English'},
        {'code': 'ja', 'name': 'Japanese', 'endonym': '日本語'},
        {'code': 'yue', 'name': 'Cantonese', 'endonym': '粵語'},
      ],
    };

    test('parses the catalog in the server\'s order', () {
      final catalog = Language.catalogFromJson(body);
      expect(catalog.map((l) => l.code), ['en', 'ja', 'yue']);
      expect(catalog.map((l) => l.name), ['English', 'Japanese', 'Cantonese']);
      expect(catalog.map((l) => l.endonym), ['English', '日本語', '粵語']);
    });

    test('an empty catalog is a catalog', () {
      expect(Language.catalogFromJson({'languages': <Object?>[]}), isEmpty);
    });

    test('the parsed catalog can\'t be changed', () {
      final catalog = Language.catalogFromJson(body);
      expect(catalog.clear, throwsUnsupportedError);
    });

    test('rejects a malformed catalog', () {
      const entry = {'code': 'es', 'name': 'Spanish', 'endonym': 'Español'};
      for (final b in <Object?>[
        null,
        <Object?>[entry],
        <String, Object?>{},
        {'languages': null},
        {'languages': entry},
        {
          'languages': ['es'],
        },
        {
          'languages': [null],
        },
        for (final key in entry.keys) ...[
          {
            'languages': [
              {...entry}..remove(key),
            ],
          },
          {
            'languages': [
              {...entry, key: ''},
            ],
          },
          {
            'languages': [
              {...entry, key: 5},
            ],
          },
          {
            'languages': [
              {...entry, key: null},
            ],
          },
        ],
        // One bad entry spoils the whole answer.
        {
          'languages': [
            entry,
            {'code': 'en'},
          ],
        },
      ]) {
        expect(
          () => Language.catalogFromJson(b),
          _malformed,
          reason: jsonEncode(b),
        );
      }
    });
  });

  group('UserLanguages.fromJson', () {
    test('parses both lists in order', () {
      final languages = UserLanguages.fromJson({
        'spoken': [_es, _en],
        'learning': [_ja],
      });
      expect(languages.spoken.map((l) => l.code), ['es', 'en']);
      expect(languages.spoken.map((l) => l.level), [
        LanguageLevel.native,
        LanguageLevel.c1,
      ]);
      expect(languages.learning.single.code, 'ja');
      expect(languages.learning.single.level, LanguageLevel.a2);
    });

    // "None yet" is two empty lists, never a 404 or a null (029).
    test('either list, or both, may be empty', () {
      final onlySpoken = UserLanguages.fromJson({
        'spoken': [_es],
        'learning': <Object?>[],
      });
      expect(onlySpoken.spoken, hasLength(1));
      expect(onlySpoken.learning, isEmpty);

      final onlyLearning = UserLanguages.fromJson({
        'spoken': <Object?>[],
        'learning': [_ja],
      });
      expect(onlyLearning.spoken, isEmpty);
      expect(onlyLearning.learning, hasLength(1));

      final none = UserLanguages.fromJson({
        'spoken': <Object?>[],
        'learning': <Object?>[],
      });
      expect(none.spoken, isEmpty);
      expect(none.learning, isEmpty);
    });

    test('parses what jsonDecode gives', () {
      final languages = UserLanguages.fromJson(
        jsonDecode('{"spoken":[{"language":"es","level":"b2"}],"learning":[]}'),
      );
      expect(languages.spoken.single.level, LanguageLevel.b2);
    });

    test('rejects a malformed selection', () {
      for (final b in <Object?>[
        null,
        'es',
        <Object?>[_es],
        <String, Object?>{},
        // A missing or null list is never read as an empty one.
        {
          'spoken': [_es],
        },
        {
          'learning': [_ja],
        },
        {
          'spoken': [_es],
          'learning': null,
        },
        {
          'spoken': null,
          'learning': [_ja],
        },
        {'spoken': _es, 'learning': <Object?>[]},
        {'spoken': 'es', 'learning': <Object?>[]},
        for (final entry in <Object?>[
          null,
          'es',
          ['es', 'native'],
          {'language': 'es'},
          {'level': 'native'},
          {'language': '', 'level': 'native'},
          {'language': 5, 'level': 'native'},
          {'language': null, 'level': 'native'},
          {'language': 'es', 'level': null},
          {'language': 'es', 'level': 7},
          {'language': 'es', 'level': ''},
          // A level this app doesn't know, however close.
          {'language': 'es', 'level': 'd1'},
          {'language': 'es', 'level': 'Native'},
          {'language': 'es', 'level': ' b2'},
        ]) ...[
          {
            'spoken': [entry],
            'learning': <Object?>[],
          },
          {
            'spoken': [_es],
            'learning': [_ja, entry],
          },
        ],
      ]) {
        expect(
          () => UserLanguages.fromJson(b),
          _malformed,
          reason: jsonEncode(b),
        );
      }
    });
  });

  group('UserLanguages.toJson', () {
    const es = UserLanguage('es', LanguageLevel.native);
    const en = UserLanguage('en', LanguageLevel.c1);
    const ja = UserLanguage('ja', LanguageLevel.a2);

    // The body of the PUT: a missing or null list is a 400 there (029), so
    // both keys are always arrays (030).
    final cases = <String, (UserLanguages, String)>{
      'both lists filled': (
        UserLanguages(spoken: const [es, en], learning: const [ja]),
        '{"spoken":[{"language":"es","level":"native"},'
            '{"language":"en","level":"c1"}],'
            '"learning":[{"language":"ja","level":"a2"}]}',
      ),
      'no spoken language': (
        UserLanguages(spoken: const [], learning: const [ja]),
        '{"spoken":[],"learning":[{"language":"ja","level":"a2"}]}',
      ),
      'no learning language': (
        UserLanguages(spoken: const [es], learning: const []),
        '{"spoken":[{"language":"es","level":"native"}],"learning":[]}',
      ),
      'both lists empty': (
        UserLanguages(spoken: const [], learning: const []),
        '{"spoken":[],"learning":[]}',
      ),
    };

    cases.forEach((name, c) {
      final (languages, encoded) = c;
      test('$name: both keys, each an array', () {
        final json = languages.toJson();
        expect(json.keys, ['spoken', 'learning']);
        expect(json['spoken'], isA<List<Object?>>());
        expect(json['learning'], isA<List<Object?>>());
        expect(jsonEncode(json), encoded);
      });

      test('$name: survives a round trip', () {
        final again = UserLanguages.fromJson(
          jsonDecode(jsonEncode(languages.toJson())),
        );
        expect(jsonEncode(again.toJson()), encoded);
      });
    });

    test('every level is written as its identifier', () {
      for (final level in LanguageLevel.values) {
        expect(UserLanguage('es', level).toJson(), {
          'language': 'es',
          'level': level.wire,
        });
      }
    });
  });

  group('UserLanguages', () {
    test('keeps its own lists, which can\'t be changed', () {
      final spoken = [const UserLanguage('es', LanguageLevel.native)];
      final languages = UserLanguages(spoken: spoken, learning: const []);
      spoken.add(const UserLanguage('en', LanguageLevel.c1));

      expect(languages.spoken.map((l) => l.code), ['es']);
      expect(
        () => languages.spoken.add(const UserLanguage('fr', LanguageLevel.a1)),
        throwsUnsupportedError,
      );
      expect(languages.learning.clear, throwsUnsupportedError);
    });

    // What the editor compares to know whether anything changed (030).
    group('equality', () {
      const es = UserLanguage('es', LanguageLevel.native);
      const en = UserLanguage('en', LanguageLevel.c1);
      const ja = UserLanguage('ja', LanguageLevel.a2);

      test('an entry is its code and its level', () {
        final same = UserLanguage('e${'s'}', LanguageLevel.native);
        expect(same, es);
        expect(same.hashCode, es.hashCode);
        expect(es, isNot(const UserLanguage('es', LanguageLevel.c2)));
        expect(es, isNot(const UserLanguage('en', LanguageLevel.native)));
      });

      test('equal selections are equal, with equal hashes', () {
        final a = UserLanguages(spoken: const [es, en], learning: const [ja]);
        final b = UserLanguages(spoken: [es, en], learning: [ja]);
        expect(a, b);
        expect(a.hashCode, b.hashCode);
        final none = UserLanguages(spoken: const [], learning: const []);
        expect(none, UserLanguages(spoken: const [], learning: const []));
      });

      test('a level, the order, the list or an entry makes a difference', () {
        final base = UserLanguages(
          spoken: const [es, en],
          learning: const [ja],
        );
        for (final other in [
          UserLanguages(
            spoken: const [es, UserLanguage('en', LanguageLevel.c2)],
            learning: const [ja],
          ),
          UserLanguages(spoken: const [en, es], learning: const [ja]),
          UserLanguages(spoken: const [es], learning: const [en, ja]),
          UserLanguages(spoken: const [es, en], learning: const []),
          UserLanguages(spoken: const [es, en, ja], learning: const []),
          UserLanguages(spoken: const [ja], learning: const [es, en]),
        ]) {
          expect(other, isNot(base));
        }
      });
    });

    // A member's languages are personal data: never in a string.
    test('toString is redacted', () {
      const entry = UserLanguage('yue', LanguageLevel.native);
      final languages = UserLanguages(
        spoken: const [entry],
        learning: const [],
      );
      for (final s in ['$entry', '$languages', '${languages.spoken}']) {
        expect(s, isNot(contains('yue')));
        expect(s, isNot(contains('native')));
      }
    });
  });
}

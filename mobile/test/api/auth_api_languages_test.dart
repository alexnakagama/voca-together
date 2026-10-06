import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/api/auth_api.dart';
import 'package:vocatogether/api/languages.dart';

import '../support/fakes.dart';

Matcher _protocol(ProtocolFailure failure, int status) =>
    isA<ApiProtocolException>()
        .having((e) => e.failure, 'failure', failure)
        .having((e) => e.statusCode, 'statusCode', status);

const _es = UserLanguage('es', LanguageLevel.native);
const _en = UserLanguage('en', LanguageLevel.c1);
const _ja = UserLanguage('ja', LanguageLevel.a2);

void main() {
  late FakeServer server;
  late AuthApi api;
  final token = accessToken('1');

  setUp(() {
    server = FakeServer();
    api = authApiFor(server.client);
  });

  group('languageCatalog', () {
    test('GETs the catalog with the access token', () async {
      server.once(
        'GET',
        ApiPaths.languages,
        (_) => jsonResponse(200, catalogBody()),
      );

      final catalog = await api.languageCatalog(accessToken: token);
      expect(catalog.map((l) => l.code), ['en', 'ja', 'es']);
      expect(catalog[1].name, 'Japanese');
      expect(catalog[1].endonym, '日本語');
      final request = server.requests.single;
      expect(request.method, 'GET');
      expect(request.url.path, '/v1/languages');
      expect(request.headers['Authorization'], 'Bearer $token');
      expect(request.bodyBytes, isEmpty);
    });

    test('rejects a malformed catalog', () async {
      for (final b in <Object?>[
        <String, Object?>{},
        {'languages': null},
        {
          'languages': [
            {'code': 'es', 'name': 'Spanish'},
          ],
        },
        // The member's languages are not a catalog.
        languagesBody(),
      ]) {
        server.once('GET', ApiPaths.languages, (_) => jsonResponse(200, b));
        await expectLater(
          api.languageCatalog(accessToken: token),
          throwsA(_protocol(ProtocolFailure.malformedBody, 200)),
        );
      }
    });
  });

  group('languages', () {
    test('GETs the member\'s own languages with the access token', () async {
      server.once(
        'GET',
        ApiPaths.myLanguages,
        (_) => jsonResponse(
          200,
          languagesBody(
            spoken: [('es', 'native'), ('en', 'c1')],
            learning: [('ja', 'a2')],
          ),
        ),
      );

      final languages = await api.languages(accessToken: token);
      expect(languages.spoken.map((l) => l.code), ['es', 'en']);
      expect(languages.spoken.map((l) => l.level), [
        LanguageLevel.native,
        LanguageLevel.c1,
      ]);
      expect(languages.learning.single.code, 'ja');
      final request = server.requests.single;
      expect(request.method, 'GET');
      expect(request.url.path, '/v1/me/languages');
      expect(request.headers['Authorization'], 'Bearer $token');
      expect(request.bodyBytes, isEmpty);
    });

    test('none chosen yet is two empty lists', () async {
      server.once(
        'GET',
        ApiPaths.myLanguages,
        (_) => jsonResponse(200, languagesBody()),
      );
      final languages = await api.languages(accessToken: token);
      expect(languages.spoken, isEmpty);
      expect(languages.learning, isEmpty);
    });

    // Unlike the profile there is no "none saved" 404 (029): every 404 is a
    // broken deployment, never an empty selection.
    test('a 404 is an error, whatever its code', () async {
      for (final response in [
        http.Response('Not Found', 404),
        errorResponse(404, 'not_found'),
        errorResponse(404, 'profile_not_found'),
      ]) {
        server.once('GET', ApiPaths.myLanguages, (_) => response);
        await expectLater(
          api.languages(accessToken: token),
          throwsA(
            isA<ApiHttpException>().having((e) => e.statusCode, 'status', 404),
          ),
        );
      }
    });
  });

  group('saveLanguages', () {
    // A missing or null list is a 400 on the server (029), so the body
    // always holds both, each as an array (030).
    final cases = <String, (UserLanguages, String)>{
      'both lists filled': (
        UserLanguages(spoken: const [_es, _en], learning: const [_ja]),
        '{"spoken":[{"language":"es","level":"native"},'
            '{"language":"en","level":"c1"}],'
            '"learning":[{"language":"ja","level":"a2"}]}',
      ),
      'no spoken language': (
        UserLanguages(spoken: const [], learning: const [_ja]),
        '{"spoken":[],"learning":[{"language":"ja","level":"a2"}]}',
      ),
      'no learning language': (
        UserLanguages(spoken: const [_es], learning: const []),
        '{"spoken":[{"language":"es","level":"native"}],"learning":[]}',
      ),
      'both lists empty': (
        UserLanguages(spoken: const [], learning: const []),
        '{"spoken":[],"learning":[]}',
      ),
    };

    cases.forEach((name, c) {
      final (languages, body) = c;
      test('$name: PUTs the whole selection, both keys as arrays', () async {
        server.once(
          'PUT',
          ApiPaths.myLanguages,
          (r) => jsonResponse(200, jsonDecode(r.body)),
        );

        await api.saveLanguages(accessToken: token, languages: languages);
        final request = server.requests.single;
        expect(request.method, 'PUT');
        expect(request.url.path, '/v1/me/languages');
        expect(request.headers['Authorization'], 'Bearer $token');
        expect(request.headers['Content-Type'], 'application/json');
        expect(utf8.decode(request.bodyBytes), body);
      });
    });

    test('returns the selection as the server stored it', () async {
      server.once(
        'PUT',
        ApiPaths.myLanguages,
        (_) => jsonResponse(200, languagesBody(spoken: [('en', 'b2')])),
      );

      final saved = await api.saveLanguages(
        accessToken: token,
        languages: UserLanguages(spoken: const [_es], learning: const [_ja]),
      );
      expect(saved.spoken.single.code, 'en');
      expect(saved.spoken.single.level, LanguageLevel.b2);
      expect(saved.learning, isEmpty);
    });

    test('surfaces validation errors with their fields', () async {
      server.once(
        'PUT',
        ApiPaths.myLanguages,
        (_) => jsonResponse(422, {
          'error': {
            'code': 'validation_failed',
            'fields': [
              {'field': 'spoken', 'code': 'too_many'},
              {'field': 'learning', 'code': 'duplicate'},
            ],
          },
        }),
      );
      await expectLater(
        api.saveLanguages(
          accessToken: token,
          languages: UserLanguages(spoken: const [_es], learning: const [_es]),
        ),
        throwsA(
          isA<ApiHttpException>()
              .having((e) => e.code, 'code', 'validation_failed')
              .having(
                (e) => e.fields.map((f) => '${f.field}:${f.code}'),
                'fields',
                ['spoken:too_many', 'learning:duplicate'],
              ),
        ),
      );
    });
  });

  group('every call', () {
    final empty = UserLanguages(spoken: const [], learning: const []);

    test('requires exactly 200', () async {
      server
        ..once(
          'GET',
          ApiPaths.languages,
          (_) => jsonResponse(203, catalogBody()),
        )
        ..once(
          'GET',
          ApiPaths.myLanguages,
          (_) => jsonResponse(202, languagesBody()),
        )
        ..once(
          'PUT',
          ApiPaths.myLanguages,
          (_) => jsonResponse(201, languagesBody()),
        )
        ..once('PUT', ApiPaths.myLanguages, (_) => noContent());
      await expectLater(
        api.languageCatalog(accessToken: token),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 203)),
      );
      await expectLater(
        api.languages(accessToken: token),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 202)),
      );
      await expectLater(
        api.saveLanguages(accessToken: token, languages: empty),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 201)),
      );
      await expectLater(
        api.saveLanguages(accessToken: token, languages: empty),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 204)),
      );
    });

    test('rejects a malformed selection', () async {
      for (final b in <Object?>[
        <String, Object?>{},
        {'spoken': <Object?>[]},
        {'spoken': <Object?>[], 'learning': null},
        {
          'spoken': [
            {'language': 'es', 'level': 'fluent'},
          ],
          'learning': <Object?>[],
        },
        // The catalog is not a selection.
        catalogBody(),
      ]) {
        server
          ..once('GET', ApiPaths.myLanguages, (_) => jsonResponse(200, b))
          ..once('PUT', ApiPaths.myLanguages, (_) => jsonResponse(200, b));
        await expectLater(
          api.languages(accessToken: token),
          throwsA(_protocol(ProtocolFailure.malformedBody, 200)),
        );
        await expectLater(
          api.saveLanguages(accessToken: token, languages: empty),
          throwsA(_protocol(ProtocolFailure.malformedBody, 200)),
        );
      }
    });

    test('refuses a refresh token as bearer', () {
      final refresh = refreshToken('1');
      expect(
        () => api.languageCatalog(accessToken: refresh),
        throwsArgumentError,
      );
      expect(() => api.languages(accessToken: refresh), throwsArgumentError);
      expect(
        () => api.saveLanguages(accessToken: refresh, languages: empty),
        throwsArgumentError,
      );
      expect(server.requests, isEmpty);
    });
  });
}

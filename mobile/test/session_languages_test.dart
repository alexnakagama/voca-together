import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/api/languages.dart';
import 'package:vocatogether/session.dart';

import 'support/fakes.dart';

http.Response _unauthorized() => errorResponse(
  401,
  'invalid_access_token',
  headers: {'www-authenticate': 'Bearer'},
);

String? _bearer(http.Request r) => r.headers['Authorization'];

const _es = UserLanguage('es', LanguageLevel.native);
const _ja = UserLanguage('ja', LanguageLevel.a2);

void main() {
  late FakeServer server;
  late InMemoryTokenStore store;
  late FakeAuthClock clock;
  late SessionManager manager;

  setUp(() {
    server = FakeServer()..always('GET', ApiPaths.healthz, (_) => healthy());
    store = InMemoryTokenStore();
    clock = FakeAuthClock();
  });

  tearDown(() => manager.dispose());

  /// Answers the next refresh with tokens for [marker].
  void refreshTo(String marker) => server.once(
    'POST',
    ApiPaths.refresh,
    (_) =>
        jsonResponse(200, tokenBody(accessToken(marker), refreshToken(marker))),
  );

  UserLanguages selection() =>
      UserLanguages(spoken: const [_es], learning: const [_ja]);

  group('languageCatalog', () {
    test('returns the catalog', () async {
      manager = await signedInManager(server, store, clock);
      server.once(
        'GET',
        ApiPaths.languages,
        (_) => jsonResponse(200, catalogBody()),
      );

      final catalog = await manager.languageCatalog();
      expect(catalog.map((l) => l.code), ['en', 'ja', 'es']);
      expect(_bearer(server.requests.single), 'Bearer ${accessToken('1')}');
    });

    test('a 401 refreshes once and asks again', () async {
      manager = await signedInManager(server, store, clock);
      server
        ..once('GET', ApiPaths.languages, (_) => _unauthorized())
        ..once(
          'GET',
          ApiPaths.languages,
          (_) => jsonResponse(200, catalogBody()),
        );
      refreshTo('2');

      expect(await manager.languageCatalog(), hasLength(3));
      expect(server.count(ApiPaths.refresh), 1);
      expect(server.to(ApiPaths.languages).map(_bearer), [
        'Bearer ${accessToken('1')}',
        'Bearer ${accessToken('2')}',
      ]);
    });
  });

  group('languages', () {
    test('returns the member\'s languages', () async {
      manager = await signedInManager(server, store, clock);
      server.once(
        'GET',
        ApiPaths.myLanguages,
        (_) => jsonResponse(
          200,
          languagesBody(spoken: [('es', 'native')], learning: [('ja', 'a2')]),
        ),
      );

      final languages = await manager.languages();
      expect(languages.spoken.single.code, 'es');
      expect(languages.learning.single.level, LanguageLevel.a2);
      expect(_bearer(server.requests.single), 'Bearer ${accessToken('1')}');
    });

    test('none chosen yet is two empty lists, and the session stays', () async {
      manager = await signedInManager(server, store, clock);
      server.once(
        'GET',
        ApiPaths.myLanguages,
        (_) => jsonResponse(200, languagesBody()),
      );

      final languages = await manager.languages();
      expect(languages.spoken, isEmpty);
      expect(languages.learning, isEmpty);
      expect(manager.status, SessionStatus.signedIn);
      expect(server.count(ApiPaths.refresh), 0);
    });

    test('a 401 refreshes once and asks again', () async {
      manager = await signedInManager(server, store, clock);
      server
        ..once('GET', ApiPaths.myLanguages, (_) => _unauthorized())
        ..once(
          'GET',
          ApiPaths.myLanguages,
          (_) => jsonResponse(200, languagesBody()),
        );
      refreshTo('2');

      await manager.languages();
      expect(server.count(ApiPaths.refresh), 1);
      expect(server.to(ApiPaths.myLanguages).map(_bearer), [
        'Bearer ${accessToken('1')}',
        'Bearer ${accessToken('2')}',
      ]);
    });

    test('a token near expiry is refreshed first', () async {
      manager = await signedInManager(
        server,
        store,
        clock,
        remaining: const Duration(seconds: 10),
      );
      refreshTo('2');
      server
        ..once(
          'GET',
          ApiPaths.languages,
          (_) => jsonResponse(200, catalogBody()),
        )
        ..once(
          'GET',
          ApiPaths.myLanguages,
          (_) => jsonResponse(200, languagesBody()),
        );

      await manager.languageCatalog();
      await manager.languages();
      expect(server.count(ApiPaths.refresh), 1);
      expect(
        [
          ...server.to(ApiPaths.languages),
          ...server.to(ApiPaths.myLanguages),
        ].map(_bearer),
        ['Bearer ${accessToken('2')}', 'Bearer ${accessToken('2')}'],
      );
    });
  });

  group('saveLanguages', () {
    test('sends the whole selection and returns what was stored', () async {
      manager = await signedInManager(server, store, clock);
      server.once(
        'PUT',
        ApiPaths.myLanguages,
        (_) => jsonResponse(
          200,
          languagesBody(spoken: [('es', 'native')], learning: [('ja', 'a2')]),
        ),
      );

      final saved = await manager.saveLanguages(selection());
      expect(saved.spoken.single.code, 'es');
      expect(saved.learning.single.code, 'ja');
      final request = server.requests.single;
      expect(_bearer(request), 'Bearer ${accessToken('1')}');
      expect(jsonDecode(request.body), {
        'spoken': [
          {'language': 'es', 'level': 'native'},
        ],
        'learning': [
          {'language': 'ja', 'level': 'a2'},
        ],
      });
    });

    // Clearing is an ordinary save (030): both keys still travel, as arrays.
    test('an empty selection is sent as two empty arrays', () async {
      manager = await signedInManager(server, store, clock);
      server.once(
        'PUT',
        ApiPaths.myLanguages,
        (_) => jsonResponse(200, languagesBody()),
      );

      final saved = await manager.saveLanguages(
        UserLanguages(spoken: const [], learning: const []),
      );
      expect(saved.spoken, isEmpty);
      expect(saved.learning, isEmpty);
      expect(server.requests.single.body, '{"spoken":[],"learning":[]}');
    });

    test('a token near expiry is refreshed before the save', () async {
      manager = await signedInManager(
        server,
        store,
        clock,
        remaining: const Duration(seconds: 10),
      );
      refreshTo('2');
      server.once(
        'PUT',
        ApiPaths.myLanguages,
        (_) => jsonResponse(200, languagesBody()),
      );

      await manager.saveLanguages(selection());
      expect(server.count(ApiPaths.refresh), 1);
      expect(server.to(ApiPaths.myLanguages).map(_bearer), [
        'Bearer ${accessToken('2')}',
      ]);
    });

    // Safe because the save is idempotent on the server (029): the second
    // send stores the same selection.
    test('a 401 refreshes once and resends the same save once', () async {
      manager = await signedInManager(server, store, clock);
      server
        ..once('PUT', ApiPaths.myLanguages, (_) => _unauthorized())
        ..once(
          'PUT',
          ApiPaths.myLanguages,
          (_) => jsonResponse(200, languagesBody()),
        );
      refreshTo('2');

      await manager.saveLanguages(selection());
      expect(server.count(ApiPaths.refresh), 1);
      final sends = server.to(ApiPaths.myLanguages);
      expect(sends, hasLength(2));
      expect(sends.map(_bearer), [
        'Bearer ${accessToken('1')}',
        'Bearer ${accessToken('2')}',
      ]);
      expect(sends[1].body, sends[0].body);
      expect(jsonDecode(sends[1].body), selection().toJson());
    });

    test('a second 401 is rethrown: two sends, one refresh', () async {
      manager = await signedInManager(server, store, clock);
      server.always('PUT', ApiPaths.myLanguages, (_) => _unauthorized());
      refreshTo('2');

      await expectLater(
        manager.saveLanguages(selection()),
        throwsA(
          isA<ApiHttpException>().having((e) => e.statusCode, 'status', 401),
        ),
      );
      expect(server.count(ApiPaths.myLanguages), 2);
      expect(server.count(ApiPaths.refresh), 1);
    });

    test('other failures pass through once and keep the session', () async {
      manager = await signedInManager(server, store, clock);
      for (final response in [
        errorResponse(400, 'invalid_request'),
        jsonResponse(422, {
          'error': {
            'code': 'validation_failed',
            'fields': [
              {'field': 'learning', 'code': 'invalid_level'},
            ],
          },
        }),
        errorResponse(429, 'rate_limited', headers: {'retry-after': '6'}),
        errorResponse(
          503,
          'service_unavailable',
          headers: {'retry-after': '5'},
        ),
        errorResponse(500, 'internal_error'),
      ]) {
        final before = server.count(ApiPaths.myLanguages);
        server.once('PUT', ApiPaths.myLanguages, (_) => response);
        await expectLater(
          manager.saveLanguages(selection()),
          throwsA(
            isA<ApiHttpException>().having(
              (e) => e.statusCode,
              'status',
              response.statusCode,
            ),
          ),
        );
        // Never retried by the session: any retry is the user's.
        expect(server.count(ApiPaths.myLanguages), before + 1);
      }
      expect(manager.status, SessionStatus.signedIn);
      expect(server.count(ApiPaths.refresh), 0);
    });

    test('a timeout or a network failure is not retried', () async {
      manager = await signedInManager(server, store, clock);
      server.once('PUT', ApiPaths.myLanguages, networkFailure);

      await expectLater(
        manager.saveLanguages(selection()),
        throwsA(isA<ApiNetworkException>()),
      );
      expect(server.count(ApiPaths.myLanguages), 1);
      expect(manager.status, SessionStatus.signedIn);
    });
  });

  test('without a session nothing is sent', () async {
    manager = SessionManager(
      store: store,
      authApi: authApiFor(server.client),
      clock: clock,
    );
    await manager.restore();

    await expectLater(
      manager.languageCatalog(),
      throwsA(isA<SignedOutException>()),
    );
    await expectLater(manager.languages(), throwsA(isA<SignedOutException>()));
    await expectLater(
      manager.saveLanguages(selection()),
      throwsA(isA<SignedOutException>()),
    );
    expect(server.requests, isEmpty);
  });

  test('after dispose every call is refused', () async {
    manager = await signedInManager(server, store, clock);
    manager.dispose();
    addTearDown(
      () => manager = SessionManager(
        store: store,
        authApi: authApiFor(server.client),
        clock: clock,
      ),
    );

    expect(manager.languageCatalog, throwsStateError);
    expect(manager.languages, throwsStateError);
    expect(() => manager.saveLanguages(selection()), throwsStateError);
    expect(server.requests, isEmpty);
  });
}

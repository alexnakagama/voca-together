import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/session.dart';

import 'support/fakes.dart';

http.Response _unauthorized() => errorResponse(
  401,
  'invalid_access_token',
  headers: {'www-authenticate': 'Bearer'},
);

String? _bearer(http.Request r) => r.headers['Authorization'];

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

  group('profile', () {
    test('returns the saved profile', () async {
      manager = await signedInManager(server, store, clock);
      server.once(
        'GET',
        ApiPaths.profile,
        (_) => jsonResponse(200, profileBody(displayName: 'Ana', bio: 'Hi')),
      );

      final profile = await manager.profile();
      expect(profile!.displayName, 'Ana');
      expect(profile.bio, 'Hi');
      expect(_bearer(server.requests.single), 'Bearer ${accessToken('1')}');
    });

    test('is null when none was saved, and the session stays', () async {
      manager = await signedInManager(server, store, clock);
      server.once('GET', ApiPaths.profile, (_) => noProfile());

      expect(await manager.profile(), isNull);
      expect(manager.status, SessionStatus.signedIn);
      expect(server.count(ApiPaths.refresh), 0);
    });

    test('a 401 refreshes once and asks again', () async {
      manager = await signedInManager(server, store, clock);
      server
        ..once('GET', ApiPaths.profile, (_) => _unauthorized())
        ..once(
          'GET',
          ApiPaths.profile,
          (_) => jsonResponse(200, profileBody()),
        );
      refreshTo('2');

      expect(await manager.profile(), isNotNull);
      expect(server.count(ApiPaths.refresh), 1);
      expect(server.to(ApiPaths.profile).map(_bearer), [
        'Bearer ${accessToken('1')}',
        'Bearer ${accessToken('2')}',
      ]);
    });

    test('without a session nothing is sent', () async {
      manager = SessionManager(
        store: store,
        authApi: authApiFor(server.client),
        clock: clock,
      );
      await manager.restore();

      await expectLater(manager.profile(), throwsA(isA<SignedOutException>()));
      await expectLater(
        manager.saveProfile(displayName: 'Ana', bio: ''),
        throwsA(isA<SignedOutException>()),
      );
      expect(server.requests, isEmpty);
    });
  });

  group('saveProfile', () {
    test('sends what was typed and returns what was stored', () async {
      manager = await signedInManager(server, store, clock);
      server.once(
        'PUT',
        ApiPaths.profile,
        (_) => jsonResponse(200, profileBody(displayName: 'Ana', bio: 'Hi')),
      );

      final saved = await manager.saveProfile(displayName: ' Ana ', bio: 'Hi ');
      expect(saved.displayName, 'Ana');
      expect(saved.bio, 'Hi');
      final request = server.requests.single;
      expect(_bearer(request), 'Bearer ${accessToken('1')}');
      expect(jsonDecode(request.body), {'display_name': ' Ana ', 'bio': 'Hi '});
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
        ApiPaths.profile,
        (_) => jsonResponse(200, profileBody()),
      );

      await manager.saveProfile(displayName: 'Ana', bio: '');
      expect(server.count(ApiPaths.refresh), 1);
      expect(server.to(ApiPaths.profile).map(_bearer), [
        'Bearer ${accessToken('2')}',
      ]);
    });

    // Safe because the save is idempotent on the server (027): the second
    // send stores the same text.
    test('a 401 refreshes once and resends the same save once', () async {
      manager = await signedInManager(server, store, clock);
      server
        ..once('PUT', ApiPaths.profile, (_) => _unauthorized())
        ..once(
          'PUT',
          ApiPaths.profile,
          (_) => jsonResponse(200, profileBody()),
        );
      refreshTo('2');

      await manager.saveProfile(displayName: 'Ana', bio: 'Hi');
      expect(server.count(ApiPaths.refresh), 1);
      final sends = server.to(ApiPaths.profile);
      expect(sends, hasLength(2));
      expect(sends.map(_bearer), [
        'Bearer ${accessToken('1')}',
        'Bearer ${accessToken('2')}',
      ]);
      expect(sends[1].body, sends[0].body);
    });

    test('other failures pass through once and keep the session', () async {
      manager = await signedInManager(server, store, clock);
      for (final response in [
        jsonResponse(422, {
          'error': {
            'code': 'validation_failed',
            'fields': [
              {'field': 'display_name', 'code': 'required'},
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
        final before = server.count(ApiPaths.profile);
        server.once('PUT', ApiPaths.profile, (_) => response);
        await expectLater(
          manager.saveProfile(displayName: '', bio: ''),
          throwsA(
            isA<ApiHttpException>().having(
              (e) => e.statusCode,
              'status',
              response.statusCode,
            ),
          ),
        );
        // Never retried by the session: any retry is the user's.
        expect(server.count(ApiPaths.profile), before + 1);
      }
      expect(manager.status, SessionStatus.signedIn);
      expect(server.count(ApiPaths.refresh), 0);
    });
  });
}

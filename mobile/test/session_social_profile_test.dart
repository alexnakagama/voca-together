import 'dart:typed_data';

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

final _memberPath = ApiPaths.memberProfile(testMemberId);
final _memberAvatarPath = ApiPaths.memberAvatar(testMemberId);

/// Every byte value, so a resend that changed a byte would show.
final _upload = Uint8List.fromList([for (var i = 0; i < 256; i++) i]);
final _stored = [0xff, 0xd8, 0xff, 9, 8, 7];

/// One of the five methods: its request, a success answer, and the call.
typedef _Op = ({
  String method,
  String path,
  http.Response Function() ok,
  Future<Object?> Function(SessionManager) call,
});

final _ops = <String, _Op>{
  'avatar': (
    method: 'GET',
    path: ApiPaths.myAvatar,
    ok: () => imageResponse(_stored),
    call: (m) => m.avatar(),
  ),
  'saveAvatar': (
    method: 'PUT',
    path: ApiPaths.myAvatar,
    ok: () => imageResponse(_stored),
    call: (m) => m.saveAvatar(_upload),
  ),
  'removeAvatar': (
    method: 'DELETE',
    path: ApiPaths.myAvatar,
    ok: noContent,
    call: (m) => m.removeAvatar(),
  ),
  'memberProfile': (
    method: 'GET',
    path: _memberPath,
    ok: () => jsonResponse(200, memberProfileBody()),
    call: (m) => m.memberProfile(testMemberId),
  ),
  'memberAvatar': (
    method: 'GET',
    path: _memberAvatarPath,
    ok: () => imageResponse(_stored),
    call: (m) => m.memberAvatar(testMemberId),
  ),
};

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

  group('results', () {
    setUp(() async => manager = await signedInManager(server, store, clock));

    test('avatar returns the picture, or null when there is none', () async {
      server
        ..once('GET', ApiPaths.myAvatar, (_) => imageResponse(_stored))
        ..once('GET', ApiPaths.myAvatar, (_) => noAvatar());
      expect(await manager.avatar(), _stored);
      expect(await manager.avatar(), isNull);
      expect(manager.status, SessionStatus.signedIn);
      expect(server.count(ApiPaths.refresh), 0);
    });

    test('saveAvatar sends the bytes and returns what was stored', () async {
      server.once('PUT', ApiPaths.myAvatar, (_) => imageResponse(_stored));
      expect(await manager.saveAvatar(_upload), _stored);
      final request = server.requests.single;
      expect(request.bodyBytes, _upload);
      expect(_bearer(request), 'Bearer ${accessToken('1')}');
    });

    test('removeAvatar succeeds with or without a picture', () async {
      server.always('DELETE', ApiPaths.myAvatar, (_) => noContent());
      await manager.removeAvatar();
      await manager.removeAvatar();
      expect(server.to(ApiPaths.myAvatar).map((r) => r.method), [
        'DELETE',
        'DELETE',
      ]);
    });

    test('memberProfile returns the profile, or null for no profile', () async {
      server
        ..once(
          'GET',
          _memberPath,
          (_) => jsonResponse(
            200,
            memberProfileBody(displayName: 'Bea', hasAvatar: true),
          ),
        )
        ..once('GET', _memberPath, (_) => noProfile());
      final profile = await manager.memberProfile(testMemberId);
      expect(profile!.displayName, 'Bea');
      expect(profile.hasAvatar, isTrue);
      expect(await manager.memberProfile(testMemberId), isNull);
      expect(manager.status, SessionStatus.signedIn);
    });

    test('memberAvatar returns the picture, or null when none', () async {
      server
        ..once('GET', _memberAvatarPath, (_) => imageResponse(_stored))
        ..once('GET', _memberAvatarPath, (_) => noAvatar());
      expect(await manager.memberAvatar(testMemberId), _stored);
      expect(await manager.memberAvatar(testMemberId), isNull);
    });

    test('nothing is cached: every call asks the server', () async {
      server
        ..always('GET', ApiPaths.myAvatar, (_) => imageResponse(_stored))
        ..always(
          'GET',
          _memberPath,
          (_) => jsonResponse(200, memberProfileBody()),
        )
        ..always('GET', _memberAvatarPath, (_) => imageResponse(_stored));
      for (var i = 0; i < 2; i++) {
        await manager.avatar();
        await manager.memberProfile(testMemberId);
        await manager.memberAvatar(testMemberId);
      }
      expect(server.count(ApiPaths.myAvatar), 2);
      expect(server.count(_memberPath), 2);
      expect(server.count(_memberAvatarPath), 2);
    });

    test('a malformed member id is refused and nothing is sent', () {
      for (final id in ['', 'abc', testMemberId.toUpperCase()]) {
        expect(() => manager.memberProfile(id), throwsArgumentError);
        expect(() => manager.memberAvatar(id), throwsArgumentError);
      }
      expect(server.requests, isEmpty);
      expect(manager.status, SessionStatus.signedIn);
    });
  });

  // A caller's mistake costs no network and leaves the tokens alone: the
  // id is refused before the stale token would be refreshed.
  test('a malformed member id with a stale token refreshes nothing', () async {
    manager = await signedInManager(
      server,
      store,
      clock,
      remaining: const Duration(seconds: 10),
    );
    final stored = store.raw;
    for (final id in ['', 'abc', testMemberId.toUpperCase()]) {
      expect(() => manager.memberProfile(id), throwsArgumentError);
      expect(() => manager.memberAvatar(id), throwsArgumentError);
    }
    await settle();
    expect(server.count(ApiPaths.refresh), 0);
    expect(server.requests, isEmpty);
    expect(store.raw, stored);
    expect(manager.status, SessionStatus.signedIn);
  });

  _ops.forEach((name, op) {
    group(name, () {
      test('a token near expiry is refreshed before the request', () async {
        manager = await signedInManager(
          server,
          store,
          clock,
          remaining: const Duration(seconds: 10),
        );
        refreshTo('2');
        server.once(op.method, op.path, (_) => op.ok());

        await op.call(manager);
        expect(server.count(ApiPaths.refresh), 1);
        expect(server.to(op.path).map(_bearer), ['Bearer ${accessToken('2')}']);
      });

      // Safe for the two writes because both are idempotent on the server
      // (031): the same upload again changes nothing, and neither does a
      // second removal.
      test('a 401 refreshes once and resends the same request once', () async {
        manager = await signedInManager(server, store, clock);
        server
          ..once(op.method, op.path, (_) => _unauthorized())
          ..once(op.method, op.path, (_) => op.ok());
        refreshTo('2');

        await op.call(manager);
        expect(server.count(ApiPaths.refresh), 1);
        final sends = server.to(op.path);
        expect(sends, hasLength(2));
        expect(sends.map((r) => r.method), [op.method, op.method]);
        expect(sends.map(_bearer), [
          'Bearer ${accessToken('1')}',
          'Bearer ${accessToken('2')}',
        ]);
        expect(sends[1].bodyBytes, sends[0].bodyBytes);
        expect(
          sends[1].headers['Content-Type'],
          sends[0].headers['Content-Type'],
        );
        if (name == 'saveAvatar') expect(sends[1].bodyBytes, _upload);
      });

      test('a second 401 is rethrown: two sends, one refresh', () async {
        manager = await signedInManager(server, store, clock);
        server.always(op.method, op.path, (_) => _unauthorized());
        refreshTo('2');

        await expectLater(
          op.call(manager),
          throwsA(
            isA<ApiHttpException>().having((e) => e.statusCode, 'status', 401),
          ),
        );
        expect(server.count(op.path), 2);
        expect(server.count(ApiPaths.refresh), 1);
        // A 401 alone never signs the member out.
        expect(manager.status, SessionStatus.signedIn);
      });

      test('other failures pass through once and keep the session', () async {
        manager = await signedInManager(server, store, clock);
        final failures = <(Responder, Matcher)>[
          for (final response in [
            jsonResponse(422, {
              'error': {
                'code': 'validation_failed',
                'fields': [
                  {'field': 'avatar', 'code': 'invalid_image'},
                ],
              },
            }),
            errorResponse(429, 'rate_limited', headers: {'retry-after': '6'}),
            errorResponse(
              503,
              'service_unavailable',
              headers: {'retry-after': '5'},
            ),
            errorResponse(400, 'invalid_request'),
            errorResponse(500, 'internal_error'),
            errorResponse(404, 'not_found'),
          ])
            (
              (_) => response,
              isA<ApiHttpException>().having(
                (e) => e.statusCode,
                'status',
                response.statusCode,
              ),
            ),
          (networkFailure, isA<ApiNetworkException>()),
          (
            (_) => http.Response('<html></html>', 200),
            isA<ApiProtocolException>(),
          ),
        ];
        for (final (responder, matcher) in failures) {
          final before = server.count(op.path);
          server.once(op.method, op.path, responder);
          await expectLater(op.call(manager), throwsA(matcher));
          // Never retried by the session: any retry is the member's.
          expect(server.count(op.path), before + 1);
        }
        expect(manager.status, SessionStatus.signedIn);
        expect(server.count(ApiPaths.refresh), 0);
      });

      test('without a session nothing is sent', () async {
        manager = SessionManager(
          store: store,
          authApi: authApiFor(server.client),
          clock: clock,
        );
        await manager.restore();

        await expectLater(op.call(manager), throwsA(isA<SignedOutException>()));
        expect(server.requests, isEmpty);
      });

      test('a session that ends during the refresh sends nothing', () async {
        manager = await signedInManager(server, store, clock);
        server
          ..once(op.method, op.path, (_) => _unauthorized())
          ..once(
            'POST',
            ApiPaths.refresh,
            (_) => errorResponse(401, 'invalid_refresh_token'),
          );

        await expectLater(op.call(manager), throwsA(isA<SignedOutException>()));
        expect(server.count(op.path), 1);
        expect(manager.status, SessionStatus.signedOut);
      });

      test('a disposed manager refuses the call', () async {
        final disposed = await signedInManager(server, store, clock);
        disposed.dispose();
        expect(() => op.call(disposed), throwsStateError);
        expect(server.requests, isEmpty);
        // For tearDown, which disposes `manager`.
        manager = SessionManager(
          store: InMemoryTokenStore(),
          authApi: authApiFor(server.client),
          clock: clock,
        );
      });
    });
  });
}

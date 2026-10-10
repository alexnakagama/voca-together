import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/api/report_reason.dart';
import 'package:vocatogether/session.dart';

import 'support/fakes.dart';

http.Response _unauthorized() => errorResponse(
  401,
  'invalid_access_token',
  headers: {'www-authenticate': 'Bearer'},
);

String? _bearer(http.Request r) => r.headers['Authorization'];

final _blockPath = ApiPaths.myBlock(otherMemberId);
final _reportPath = ApiPaths.myReport(otherMemberId);

/// As a member types it: nothing in it may change on the way.
const _details = '  Keeps writing.\r\n\tAgain.  ';

/// One of the four methods: its request, a success answer, and the call.
typedef _Op = ({
  String method,
  String path,
  http.Response Function() ok,
  Future<Object?> Function(SessionManager) call,
});

final _ops = <String, _Op>{
  'blockedMembers': (
    method: 'GET',
    path: ApiPaths.myBlocks,
    ok: () => jsonResponse(200, blocksBody([(otherMemberId, 'Bea')])),
    call: (m) => m.blockedMembers(),
  ),
  'blockMember': (
    method: 'PUT',
    path: _blockPath,
    ok: noContent,
    call: (m) => m.blockMember(otherMemberId),
  ),
  'unblockMember': (
    method: 'DELETE',
    path: _blockPath,
    ok: noContent,
    call: (m) => m.unblockMember(otherMemberId),
  ),
  'reportMember': (
    method: 'PUT',
    path: _reportPath,
    ok: noContent,
    call: (m) => m.reportMember(
      otherMemberId,
      reason: ReportReason.harassment,
      details: _details,
    ),
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

  /// The three calls that name a member, for [id].
  List<Future<void> Function()> named(String id) => [
    () => manager.blockMember(id),
    () => manager.unblockMember(id),
    () => manager.reportMember(id, reason: ReportReason.spam, details: ''),
  ];

  group('results', () {
    setUp(() async => manager = await signedInManager(server, store, clock));

    test('blockedMembers returns the list in the server\'s order', () async {
      server
        ..once(
          'GET',
          ApiPaths.myBlocks,
          (_) => jsonResponse(
            200,
            blocksBody([(otherMemberId, 'Bea'), (testMemberId, 'Chidi')]),
          ),
        )
        ..once(
          'GET',
          ApiPaths.myBlocks,
          (_) => jsonResponse(200, blocksBody()),
        );
      final members = await manager.blockedMembers();
      expect(members.map((m) => m.displayName), ['Bea', 'Chidi']);
      expect(members.map((m) => m.id), [otherMemberId, testMemberId]);
      expect(await manager.blockedMembers(), isEmpty);
      expect(server.count(ApiPaths.refresh), 0);
    });

    test('blockMember and unblockMember send their one request', () async {
      server
        ..once('PUT', _blockPath, (_) => noContent())
        ..once('DELETE', _blockPath, (_) => noContent());
      await manager.blockMember(otherMemberId);
      await manager.unblockMember(otherMemberId);
      expect(server.requests.map((r) => '${r.method} ${r.url.path}'), [
        'PUT $_blockPath',
        'DELETE $_blockPath',
      ]);
      for (final request in server.requests) {
        expect(request.bodyBytes, isEmpty);
        expect(_bearer(request), 'Bearer ${accessToken('1')}');
      }
    });

    // An unblock names the member by id alone: it needs nothing the list
    // returned, and no list request.
    test('unblockMember needs no list', () async {
      server.once('DELETE', _blockPath, (_) => noContent());
      await manager.unblockMember(otherMemberId);
      expect(server.count(ApiPaths.myBlocks), 0);
      expect(server.requests, hasLength(1));
    });

    test('reportMember sends the reason and the details as typed', () async {
      server.once('PUT', _reportPath, (_) => noContent());
      await manager.reportMember(
        otherMemberId,
        reason: ReportReason.harassment,
        details: _details,
      );
      final request = server.requests.single;
      expect(jsonDecode(request.body), {
        'reason': 'harassment',
        'details': _details,
      });
      expect(_bearer(request), 'Bearer ${accessToken('1')}');
    });

    test('nothing is cached: every call asks the server', () async {
      server.always(
        'GET',
        ApiPaths.myBlocks,
        (_) => jsonResponse(200, blocksBody()),
      );
      await manager.blockedMembers();
      await manager.blockedMembers();
      expect(server.count(ApiPaths.myBlocks), 2);
    });

    test('a malformed member id is refused and nothing is sent', () {
      for (final id in ['', 'abc', otherMemberId.toUpperCase()]) {
        for (final call in named(id)) {
          expect(call, throwsArgumentError, reason: id);
        }
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
    for (final id in ['', 'abc', otherMemberId.toUpperCase()]) {
      for (final call in named(id)) {
        expect(call, throwsArgumentError, reason: id);
      }
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

      // Safe for the three writes because each is idempotent on the server
      // (033): a block that exists is kept, removing none is not an error,
      // and the same report again changes nothing.
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
        expect(sends[1].url, sends[0].url);
        expect(sends[1].bodyBytes, sends[0].bodyBytes);
        expect(
          sends[1].headers['Content-Type'],
          sends[0].headers['Content-Type'],
        );
        if (name == 'reportMember') {
          expect(jsonDecode(sends[1].body), {
            'reason': 'harassment',
            'details': _details,
          });
        }
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
            fieldError('blocks', 'too_many'),
            fieldError('details', 'too_long'),
            fieldError('member', 'self'),
            errorResponse(429, 'rate_limited', headers: {'retry-after': '6'}),
            errorResponse(
              503,
              'service_unavailable',
              headers: {'retry-after': '5'},
            ),
            errorResponse(400, 'invalid_request'),
            errorResponse(500, 'internal_error'),
            errorResponse(404, 'profile_not_found'),
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

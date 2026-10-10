import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/auth_api.dart';
import 'package:vocatogether/api/report_reason.dart';

import '../support/fakes.dart';

Matcher _protocol(ProtocolFailure failure, int status) =>
    isA<ApiProtocolException>()
        .having((e) => e.failure, 'failure', failure)
        .having((e) => e.statusCode, 'statusCode', status);

Matcher _http(int status, String? code) => isA<ApiHttpException>()
    .having((e) => e.statusCode, 'status', status)
    .having((e) => e.code, 'code', code);

const _blocksPath = '/v1/me/blocks';
const _blockPath = '/v1/me/blocks/$otherMemberId';
const _reportPath = '/v1/me/reports/$otherMemberId';

void main() {
  late FakeServer server;
  late AuthApi api;
  final token = accessToken('1');

  setUp(() {
    server = FakeServer();
    api = authApiFor(server.client);
  });

  /// The one request sent, checked for what every call here has in common.
  http.Request sentOnce(String method, String path) {
    final request = server.requests.single;
    expect(request.method, method);
    expect(request.url.toString(), 'https://api.test.example$path');
    expect(request.headers['Authorization'], 'Bearer $token');
    return request;
  }

  Future<void> block() =>
      api.blockMember(accessToken: token, id: otherMemberId);
  Future<void> unblock() =>
      api.unblockMember(accessToken: token, id: otherMemberId);
  Future<void> report({
    ReportReason reason = ReportReason.spam,
    String details = '',
  }) => api.reportMember(
    accessToken: token,
    id: otherMemberId,
    reason: reason,
    details: details,
  );

  group('blockedMembers', () {
    test('GETs the list, in the server\'s order', () async {
      server.once(
        'GET',
        _blocksPath,
        (_) => jsonResponse(
          200,
          blocksBody([(otherMemberId, 'Bea'), (testMemberId, 'Chidi')]),
        ),
      );
      final members = await api.blockedMembers(accessToken: token);
      expect(members.map((m) => m.id), [otherMemberId, testMemberId]);
      expect(members.map((m) => m.displayName), ['Bea', 'Chidi']);
      final request = sentOnce('GET', _blocksPath);
      expect(request.bodyBytes, isEmpty);
    });

    test('nobody blocked is an empty list', () async {
      server.once('GET', _blocksPath, (_) => jsonResponse(200, blocksBody()));
      expect(await api.blockedMembers(accessToken: token), isEmpty);
    });

    // Unlike the profile there is no "none" 404: a broken deployment never
    // looks like an empty list.
    test('a 404 is an error, whatever its code', () async {
      for (final (response, code) in [
        (noProfile(), 'profile_not_found'),
        (http.Response('Not Found', 404), null),
      ]) {
        server.once('GET', _blocksPath, (_) => response);
        await expectLater(
          api.blockedMembers(accessToken: token),
          throwsA(_http(404, code)),
        );
      }
    });

    test('requires exactly 200 and a well-formed list', () async {
      server
        ..once('GET', _blocksPath, (_) => jsonResponse(203, blocksBody()))
        ..once('GET', _blocksPath, (_) => noContent())
        ..once('GET', _blocksPath, (_) => jsonResponse(200, {'blocks': null}))
        ..once(
          'GET',
          _blocksPath,
          (_) => jsonResponse(200, {
            'blocks': [
              {'id': otherMemberId, 'display_name': 'Bea'},
              {'id': testMemberId},
            ],
          }),
        );
      await expectLater(
        api.blockedMembers(accessToken: token),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 203)),
      );
      await expectLater(
        api.blockedMembers(accessToken: token),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 204)),
      );
      for (var i = 0; i < 2; i++) {
        await expectLater(
          api.blockedMembers(accessToken: token),
          throwsA(_protocol(ProtocolFailure.malformedBody, 200)),
        );
      }
    });
  });

  group('blockMember', () {
    test('PUTs the member\'s path with the bearer and no body', () async {
      server.once('PUT', _blockPath, (_) => noContent());
      await block();
      final request = sentOnce('PUT', _blockPath);
      expect(request.bodyBytes, isEmpty);
      expect(request.headers.containsKey('Content-Type'), isFalse);
      expect(request.url.hasQuery, isFalse);
    });

    test('requires exactly 204', () async {
      server
        ..once('PUT', _blockPath, (_) => jsonResponse(200, {}))
        ..once('PUT', _blockPath, (_) => http.Response('', 201));
      await expectLater(
        block(),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 200)),
      );
      await expectLater(
        block(),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 201)),
      );
    });

    test('the limit is the server\'s 422, unchanged', () async {
      server.once('PUT', _blockPath, (_) => fieldError('blocks', 'too_many'));
      await expectLater(
        block(),
        throwsA(
          isA<ApiHttpException>()
              .having((e) => e.statusCode, 'status', 422)
              .having(
                (e) => e.fields.map((f) => '${f.field}:${f.code}'),
                'fields',
                ['blocks:too_many'],
              ),
        ),
      );
    });

    test('a 404 is an error, whatever its code', () async {
      server.once('PUT', _blockPath, (_) => noProfile());
      await expectLater(block(), throwsA(_http(404, 'profile_not_found')));
    });
  });

  group('unblockMember', () {
    test('DELETEs the member\'s path with the bearer and no body', () async {
      server.once('DELETE', _blockPath, (_) => noContent());
      await unblock();
      final request = sentOnce('DELETE', _blockPath);
      expect(request.bodyBytes, isEmpty);
      expect(request.headers.containsKey('Content-Type'), isFalse);
      expect(request.url.hasQuery, isFalse);
    });

    test('requires exactly 204', () async {
      server.once('DELETE', _blockPath, (_) => jsonResponse(200, {}));
      await expectLater(
        unblock(),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 200)),
      );
    });

    test('a 404 is an error, whatever its code', () async {
      server.once('DELETE', _blockPath, (_) => noProfile());
      await expectLater(unblock(), throwsA(_http(404, 'profile_not_found')));
    });
  });

  group('reportMember', () {
    test('PUTs the reason\'s wire code and the details as typed', () async {
      // Nothing is trimmed, normalized or cut: every rule is the server's.
      const typed = '  He keeps writing.\r\n\tAgain.  ';
      server.once('PUT', _reportPath, (_) => noContent());
      await report(reason: ReportReason.inappropriateContent, details: typed);
      final request = sentOnce('PUT', _reportPath);
      expect(request.headers['Content-Type'], startsWith('application/json'));
      expect(jsonDecode(request.body), {
        'reason': 'inappropriate_content',
        'details': typed,
      });
      expect(request.url.hasQuery, isFalse);
    });

    test('sends each reason as its wire code', () async {
      server.always('PUT', _reportPath, (_) => noContent());
      for (final reason in ReportReason.values) {
        await report(reason: reason);
      }
      expect(
        server.requests.map((r) => (jsonDecode(r.body) as Map)['reason']),
        [
          'harassment',
          'inappropriate_content',
          'spam',
          'impersonation',
          'other',
        ],
      );
    });

    test('no details is an empty text, and both keys are sent', () async {
      server.once('PUT', _reportPath, (_) => noContent());
      await report();
      expect(jsonDecode(sentOnce('PUT', _reportPath).body), {
        'reason': 'spam',
        'details': '',
      });
    });

    test('applies no rule of its own: a long text is sent whole', () async {
      final long = 'é' * 5000;
      server.once('PUT', _reportPath, (_) => fieldError('details', 'too_long'));
      await expectLater(
        report(details: long),
        throwsA(
          isA<ApiHttpException>()
              .having((e) => e.statusCode, 'status', 422)
              .having(
                (e) => e.fields.map((f) => '${f.field}:${f.code}'),
                'fields',
                ['details:too_long'],
              ),
        ),
      );
      expect((jsonDecode(server.requests.single.body) as Map)['details'], long);
    });

    test('requires exactly 204', () async {
      server
        ..once('PUT', _reportPath, (_) => jsonResponse(200, {}))
        ..once('PUT', _reportPath, (_) => jsonResponse(202, {}));
      await expectLater(
        report(),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 200)),
      );
      await expectLater(
        report(),
        throwsA(_protocol(ProtocolFailure.unexpectedStatus, 202)),
      );
    });
  });

  group('every call', () {
    test('refuses a non-canonical member id before any request', () async {
      for (final id in [
        '',
        'abc',
        otherMemberId.toUpperCase(),
        otherMemberId.replaceAll('-', ''),
        ' $otherMemberId',
        '$otherMemberId/x',
      ]) {
        await expectLater(
          api.blockMember(accessToken: token, id: id),
          throwsArgumentError,
          reason: id,
        );
        await expectLater(
          api.unblockMember(accessToken: token, id: id),
          throwsArgumentError,
          reason: id,
        );
        await expectLater(
          api.reportMember(
            accessToken: token,
            id: id,
            reason: ReportReason.other,
            details: '',
          ),
          throwsArgumentError,
          reason: id,
        );
      }
      expect(server.requests, isEmpty);
    });

    test('surfaces the server\'s failures unchanged, sent once', () async {
      final calls = <(String, String, Future<Object?> Function())>[
        ('GET', _blocksPath, () => api.blockedMembers(accessToken: token)),
        ('PUT', _blockPath, block),
        ('DELETE', _blockPath, unblock),
        ('PUT', _reportPath, report),
      ];
      for (final (method, path, call) in calls) {
        final before = server.requests.length;
        server
          ..once(
            method,
            path,
            (_) => errorResponse(
              429,
              'rate_limited',
              headers: {'retry-after': '6'},
            ),
          )
          ..once(
            method,
            path,
            (_) => errorResponse(
              401,
              'invalid_access_token',
              headers: {'www-authenticate': 'Bearer'},
            ),
          )
          ..once(method, path, networkFailure);
        await expectLater(
          call(),
          throwsA(
            isA<ApiHttpException>()
                .having((e) => e.code, 'code', 'rate_limited')
                .having(
                  (e) => e.retryAfter,
                  'retryAfter',
                  const Duration(seconds: 6),
                ),
          ),
          reason: '$method $path',
        );
        await expectLater(call(), throwsA(_http(401, 'invalid_access_token')));
        await expectLater(call(), throwsA(isA<ApiNetworkException>()));
        expect(server.requests.length, before + 3);
      }
    });

    test('refuses a refresh token as bearer', () {
      final refresh = refreshToken('1');
      expect(
        () => api.blockedMembers(accessToken: refresh),
        throwsArgumentError,
      );
      expect(
        () => api.blockMember(accessToken: refresh, id: otherMemberId),
        throwsArgumentError,
      );
      expect(
        () => api.unblockMember(accessToken: refresh, id: otherMemberId),
        throwsArgumentError,
      );
      expect(
        () => api.reportMember(
          accessToken: refresh,
          id: otherMemberId,
          reason: ReportReason.spam,
          details: '',
        ),
        throwsArgumentError,
      );
      expect(server.requests, isEmpty);
    });
  });
}

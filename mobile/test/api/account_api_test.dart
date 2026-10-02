import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/account_api.dart';
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/api_paths.dart';

import '../support/fakes.dart';

Object? _body(http.Request r) =>
    r.bodyBytes.isEmpty ? null : jsonDecode(utf8.decode(r.bodyBytes));

void main() {
  late FakeServer server;
  late AccountApi api;

  setUp(() {
    server = FakeServer();
    api = accountApiFor(server.client);
  });

  final accepted = jsonResponse(202, {'status': 'accepted'});

  group('202 operations', () {
    final ops = <String, (String, Future<void> Function(AccountApi), Object)>{
      'register': (
        ApiPaths.register,
        (a) => a.register(email: ' A@b.c ', password: 'pässwörd 1'),
        {'email': ' A@b.c ', 'password': 'pässwörd 1'},
      ),
      'resendVerification': (
        ApiPaths.resendVerification,
        (a) => a.resendVerification(email: 'a@b.c'),
        {'email': 'a@b.c'},
      ),
      'forgotPassword': (
        ApiPaths.forgotPassword,
        (a) => a.forgotPassword(email: 'a@b.c'),
        {'email': 'a@b.c'},
      ),
    };

    ops.forEach((name, op) {
      final (path, call, body) = op;

      test('$name posts exactly its body, unmodified', () async {
        server.once('POST', path, (_) => accepted);
        await call(api);
        final r = server.requests.single;
        expect(r.method, 'POST');
        expect(r.url.path, path);
        expect(r.url.hasQuery, isFalse);
        expect(_body(r), body);
        expect(r.headers.containsKey('Authorization'), isFalse);
      });

      test('$name requires exactly 202 accepted', () async {
        for (final response in [
          jsonResponse(200, {'status': 'accepted'}),
          jsonResponse(202, {'status': 'verified'}),
          jsonResponse(202, <String, Object?>{}),
          http.Response('', 202),
        ]) {
          server.once('POST', path, (_) => response);
          await expectLater(
            call(api),
            throwsA(isA<ApiProtocolException>()),
            reason: '${response.statusCode} ${response.body}',
          );
        }
      });

      test('$name surfaces validation errors', () async {
        server.once(
          'POST',
          path,
          (_) => jsonResponse(422, {
            'error': {
              'code': 'validation_failed',
              'fields': [
                {'field': 'email', 'code': 'invalid'},
              ],
            },
          }),
        );
        await expectLater(
          call(api),
          throwsA(
            isA<ApiHttpException>()
                .having((e) => e.statusCode, 'status', 422)
                .having((e) => e.fields.single.field, 'field', 'email'),
          ),
        );
      });
    });
  });
}

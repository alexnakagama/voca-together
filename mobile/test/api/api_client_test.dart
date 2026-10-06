import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:fake_async/fake_async.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:vocatogether/api/api_client.dart';
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/config.dart';

import '../support/fakes.dart';

const _timeout = Duration(seconds: 15);

Matcher _protocol(ProtocolFailure failure, [int? status]) =>
    isA<ApiProtocolException>()
        .having((e) => e.failure, 'failure', failure)
        .having((e) => e.statusCode, 'statusCode', status);

void main() {
  late List<http.Request> sent;

  ApiClient clientAnswering(FutureOr<http.Response> Function(http.Request) f) {
    sent = [];
    return apiClientFor(
      MockClient((r) async {
        sent.add(r);
        return f(r);
      }),
    );
  }

  group('construction', () {
    test('rejects a base URL that is not a bare origin', () {
      for (final url in [
        'https://api.test.example/v1',
        'https://api.test.example?x=1',
        'https://user:pw@api.test.example',
        'ftp://api.test.example',
      ]) {
        expect(
          () => ApiClient(
            baseUrl: Uri.parse(url),
            httpClient: MockClient((_) async => http.Response('', 200)),
            requireHttps: false,
          ),
          throwsA(isA<ConfigException>()),
          reason: url,
        );
      }
    });

    test('requires https when asked to', () {
      final http.Client c = MockClient((_) async => http.Response('', 200));
      expect(
        () => ApiClient(
          baseUrl: Uri.parse('http://10.0.2.2:8080'),
          httpClient: c,
          requireHttps: true,
        ),
        throwsA(isA<ConfigException>()),
      );
      expect(
        ApiClient(
          baseUrl: Uri.parse('http://10.0.2.2:8080'),
          httpClient: c,
          requireHttps: false,
        ).baseUrl.toString(),
        'http://10.0.2.2:8080',
      );
    });
  });

  group('URL', () {
    test('joins fixed paths to the origin', () async {
      final api = clientAnswering((_) => healthy());
      await api.send('GET', '/healthz', timeout: _timeout);
      await api.send('GET', '/v1/auth/forgot-password', timeout: _timeout);
      expect(sent.map((r) => r.url.toString()), [
        'https://api.test.example/healthz',
        'https://api.test.example/v1/auth/forgot-password',
      ]);
    });

    test('rejects anything that is not an API path', () {
      final api = clientAnswering((_) => healthy());
      for (final path in [
        '',
        '/',
        'v1/me',
        '/v1',
        '/v1/',
        '/v1/me/',
        '/v1/../admin',
        '/v1/./me',
        '/v1//me',
        '/v1/me?token=x',
        '/v1/me#x',
        '/V1/me',
        '/v1/Me',
        '/v1/me%2f',
        '//evil.example/v1/me',
        'https://evil.example/v1/me',
        '/healthz/x',
      ]) {
        expect(
          () => api.send('GET', path, timeout: _timeout),
          throwsArgumentError,
          reason: path,
        );
      }
      expect(sent, isEmpty);
    });

    test('rejects methods other than GET, POST, PUT and DELETE', () {
      final api = clientAnswering((_) => healthy());
      for (final method in ['PATCH', 'HEAD', 'OPTIONS', 'put', 'delete']) {
        expect(
          () => api.send(method, '/v1/me', timeout: _timeout),
          throwsArgumentError,
          reason: method,
        );
      }
    });

    test('sends PUT with its JSON body', () async {
      final api = clientAnswering((_) => healthy());
      await api.send(
        'PUT',
        '/v1/me/profile',
        json: {'display_name': 'Ana'},
        timeout: _timeout,
      );
      expect(sent.single.method, 'PUT');
      expect(sent.single.url.path, '/v1/me/profile');
      expect(sent.single.headers['Content-Type'], 'application/json');
      expect(sent.single.body, '{"display_name":"Ana"}');
    });

    test('sends DELETE with no body', () async {
      final api = clientAnswering((_) => noContent());
      final r = await api.send(
        'DELETE',
        '/v1/me/avatar',
        bearer: accessToken('1'),
        timeout: _timeout,
      );
      expect(r.statusCode, 204);
      expect(r.json, isNull);
      final request = sent.single;
      expect(request.method, 'DELETE');
      expect(request.url.toString(), 'https://api.test.example/v1/me/avatar');
      expect(request.headers['Authorization'], 'Bearer ${accessToken('1')}');
      expect(request.headers['Accept'], 'application/json');
      expect(request.headers.containsKey('Content-Type'), isFalse);
      expect(request.bodyBytes, isEmpty);
      expect(request.followRedirects, isFalse);
    });

    test('accepts a member identifier as a path segment', () async {
      final api = clientAnswering((_) => healthy());
      await api.send('GET', '/v1/profiles/$testMemberId', timeout: _timeout);
      await api.send(
        'GET',
        '/v1/profiles/$testMemberId/avatar',
        timeout: _timeout,
      );
      expect(sent.map((r) => r.url.path), [
        '/v1/profiles/$testMemberId',
        '/v1/profiles/$testMemberId/avatar',
      ]);
    });
  });

  group('byte bodies', () {
    // Every byte value, so nothing is read as text on the way out.
    final bytes = Uint8List.fromList([for (var i = 0; i < 256; i++) i]);

    test('send PUTs the bytes as they are', () async {
      final api = clientAnswering((_) => healthy());
      await api.send(
        'PUT',
        '/v1/me/avatar',
        bytes: bytes,
        bearer: accessToken('1'),
        timeout: _timeout,
      );
      final request = sent.single;
      expect(request.method, 'PUT');
      expect(request.url.toString(), 'https://api.test.example/v1/me/avatar');
      expect(request.headers['Content-Type'], 'application/octet-stream');
      expect(request.headers['Authorization'], 'Bearer ${accessToken('1')}');
      expect(request.bodyBytes, bytes);
    });

    test('putForImage PUTs the bytes and returns the image answered', () async {
      final api = clientAnswering((_) => imageResponse([0xff, 0xd8, 0xff, 1]));
      final image = await api.putForImage(
        '/v1/me/avatar',
        bytes: bytes,
        bearer: accessToken('1'),
        timeout: _timeout,
      );
      expect(image, [0xff, 0xd8, 0xff, 1]);
      final request = sent.single;
      expect(request.method, 'PUT');
      expect(request.url.toString(), 'https://api.test.example/v1/me/avatar');
      expect(request.headers['Content-Type'], 'application/octet-stream');
      expect(request.headers['Accept'], 'image/jpeg, application/json');
      expect(request.headers['User-Agent'], 'VocaTogether-Android');
      expect(request.headers['Authorization'], 'Bearer ${accessToken('1')}');
      expect(request.bodyBytes, bytes);
      expect(request.followRedirects, isFalse);
    });

    test('an empty byte body is sent as given', () async {
      final api = clientAnswering((_) => healthy());
      await api.send(
        'PUT',
        '/v1/me/avatar',
        bytes: Uint8List(0),
        timeout: _timeout,
      );
      expect(sent.single.headers['Content-Type'], 'application/octet-stream');
      expect(sent.single.bodyBytes, isEmpty);
    });

    test('json and bytes together are refused', () {
      final api = clientAnswering((_) => healthy());
      expect(
        () => api.send(
          'PUT',
          '/v1/me/avatar',
          json: {'a': 1},
          bytes: bytes,
          timeout: _timeout,
        ),
        throwsArgumentError,
      );
      expect(sent, isEmpty);
    });
  });

  group('image responses', () {
    Future<Uint8List> get(ApiClient api) =>
        api.getImage('/v1/me/avatar', timeout: _timeout);

    test('getImage sends a GET and returns the bytes', () async {
      final api = clientAnswering((_) => imageResponse([0xff, 0xd8, 0, 255]));
      final image = await api.getImage(
        '/v1/me/avatar',
        bearer: accessToken('1'),
        timeout: _timeout,
      );
      expect(image, [0xff, 0xd8, 0, 255]);
      final request = sent.single;
      expect(request.method, 'GET');
      expect(request.headers['Accept'], 'image/jpeg, application/json');
      expect(request.headers['Authorization'], 'Bearer ${accessToken('1')}');
      expect(request.headers.containsKey('Content-Type'), isFalse);
      expect(request.bodyBytes, isEmpty);
      expect(request.followRedirects, isFalse);
    });

    test('an image at the cap is read, one byte over is refused', () async {
      final atCap = clientAnswering(
        (_) => imageResponse(Uint8List(ApiClient.maxImageBytes)),
      );
      expect(await get(atCap), hasLength(1024 * 1024));

      final over = clientAnswering(
        (_) => imageResponse(Uint8List(ApiClient.maxImageBytes + 1)),
      );
      await expectLater(
        get(over),
        throwsA(_protocol(ProtocolFailure.bodyTooLarge, 200)),
      );
    });

    test('a 2xx that is not image/jpeg is a protocol failure', () async {
      for (final type in [
        null,
        'application/json',
        'image/png',
        'text/html',
        'application/octet-stream',
        'garbage;;',
      ]) {
        final api = clientAnswering(
          (_) => http.Response.bytes(
            [0xff, 0xd8, 0xff],
            200,
            headers: {'content-type': ?type},
          ),
        );
        await expectLater(
          get(api),
          throwsA(_protocol(ProtocolFailure.notImage, 200)),
          reason: '$type',
        );
        await expectLater(
          api.putForImage(
            '/v1/me/avatar',
            bytes: Uint8List(1),
            timeout: _timeout,
          ),
          throwsA(_protocol(ProtocolFailure.notImage, 200)),
          reason: '$type',
        );
      }
    });

    test('an image type with parameters is an image', () async {
      final api = clientAnswering(
        (_) => http.Response.bytes(
          [1, 2, 3],
          200,
          headers: {'content-type': 'Image/JPEG; q=1'},
        ),
      );
      expect(await get(api), [1, 2, 3]);
    });

    test('an empty image is malformed', () {
      final api = clientAnswering((_) => imageResponse(const []));
      expect(get(api), throwsA(_protocol(ProtocolFailure.malformedBody, 200)));
    });

    test('any other 2xx is unexpected, also with an image in it', () async {
      for (final status in [201, 202, 204, 206]) {
        final api = clientAnswering(
          (_) => imageResponse(status == 204 ? const [] : [1], status: status),
        );
        await expectLater(
          get(api),
          throwsA(_protocol(ProtocolFailure.unexpectedStatus, status)),
        );
      }
    });

    test('error bodies are still parsed as JSON', () async {
      final api = clientAnswering(
        (_) => jsonResponse(
          422,
          {
            'error': {
              'code': 'validation_failed',
              'fields': [
                {'field': 'avatar', 'code': 'too_large'},
              ],
            },
          },
          headers: {'retry-after': '9'},
        ),
      );
      await expectLater(
        api.putForImage(
          '/v1/me/avatar',
          bytes: Uint8List(1),
          timeout: _timeout,
        ),
        throwsA(
          isA<ApiHttpException>()
              .having((e) => e.statusCode, 'status', 422)
              .having((e) => e.code, 'code', 'validation_failed')
              .having(
                (e) => e.fields.map((f) => '${f.field}:${f.code}'),
                'fields',
                ['avatar:too_large'],
              )
              .having(
                (e) => e.retryAfter,
                'retryAfter',
                const Duration(seconds: 9),
              ),
        ),
      );
    });

    test('an error body is not an image, whatever it says it is', () async {
      final api = clientAnswering(
        (_) =>
            imageResponse(utf8.encode('{"error":{"code":"x"}}'), status: 404),
      );
      await expectLater(
        get(api),
        throwsA(
          isA<ApiHttpException>()
              .having((e) => e.statusCode, 'status', 404)
              .having((e) => e.code, 'code', isNull),
        ),
      );
    });

    test('an error body keeps the 64 KiB cap', () {
      final api = clientAnswering(
        (_) => http.Response('x' * (64 * 1024 + 1), 500),
      );
      expect(get(api), throwsA(_protocol(ProtocolFailure.bodyTooLarge, 500)));
    });

    test('redirects are still refused, and nothing is resent', () async {
      for (final status in [301, 302, 307, 308]) {
        final api = clientAnswering(
          (_) => http.Response(
            '',
            status,
            headers: {'location': 'https://evil.example/steal'},
          ),
        );
        await expectLater(
          api.putForImage(
            '/v1/me/avatar',
            bytes: Uint8List(3),
            bearer: accessToken('1'),
            timeout: _timeout,
          ),
          throwsA(_protocol(ProtocolFailure.redirect, status)),
        );
        await expectLater(
          get(api),
          throwsA(_protocol(ProtocolFailure.redirect, status)),
        );
        expect(sent, hasLength(2));
      }
    });

    test('path, bearer and transport rules are those of send', () async {
      final api = clientAnswering((_) => imageResponse([1]));
      expect(
        () => api.getImage('/v1/me/avatar?x=1', timeout: _timeout),
        throwsArgumentError,
      );
      expect(
        () => api.getImage(
          '/v1/me/avatar',
          bearer: refreshToken('SECRET'),
          timeout: _timeout,
        ),
        throwsA(
          isA<ArgumentError>().having(
            (e) => e.toString(),
            'toString',
            isNot(contains('SECRET')),
          ),
        ),
      );
      expect(sent, isEmpty);

      final offline = apiClientFor(
        MockClient((_) async => throw const SocketException('down')),
      );
      await expectLater(get(offline), throwsA(isA<ApiNetworkException>()));
    });

    test('a hung image request times out', () {
      fakeAsync((async) {
        final api = apiClientFor(
          MockClient.streaming(
            (request, body) => Completer<http.StreamedResponse>().future,
          ),
        );
        Object? error;
        api
            .getImage('/v1/me/avatar', timeout: const Duration(seconds: 5))
            .catchError((Object e) {
              error = e;
              return Uint8List(0);
            });
        async.elapse(const Duration(seconds: 6));
        expect(error, isA<ApiTimeoutException>());
      });
    });
  });

  group('headers', () {
    test('without a body or token', () async {
      final api = clientAnswering((_) => healthy());
      await api.send('GET', '/healthz', timeout: _timeout);
      final r = sent.single;
      expect(r.headers['Accept'], 'application/json');
      expect(r.headers['User-Agent'], 'VocaTogether-Android');
      expect(r.headers.containsKey('Content-Type'), isFalse);
      expect(r.headers.containsKey('Authorization'), isFalse);
      expect(r.bodyBytes, isEmpty);
      expect(r.followRedirects, isFalse);
    });

    test('with a JSON body', () async {
      final api = clientAnswering((_) => healthy());
      await api.send(
        'POST',
        '/v1/auth/login',
        json: {'email': 'ñ@example.com', 'password': 'pässwörd'},
        timeout: _timeout,
      );
      final r = sent.single;
      expect(r.headers['Content-Type'], 'application/json');
      expect(jsonDecode(utf8.decode(r.bodyBytes)), {
        'email': 'ñ@example.com',
        'password': 'pässwörd',
      });
    });

    test('with a bearer token', () async {
      final api = clientAnswering((_) => healthy());
      await api.send(
        'GET',
        '/v1/me',
        bearer: accessToken('1'),
        timeout: _timeout,
      );
      expect(
        sent.single.headers['Authorization'],
        'Bearer ${accessToken('1')}',
      );
    });

    test(
      'refuses a bearer that is not an access token, without echoing it',
      () {
        final api = clientAnswering((_) => healthy());
        for (final bad in [refreshToken('SECRET'), 'SECRET', '']) {
          expect(
            () => api.send('GET', '/v1/me', bearer: bad, timeout: _timeout),
            throwsA(
              isA<ArgumentError>().having(
                (e) => e.toString(),
                'toString',
                isNot(contains('SECRET')),
              ),
            ),
          );
        }
        expect(sent, isEmpty);
      },
    );
  });

  group('success responses', () {
    test('decode JSON', () async {
      final api = clientAnswering((_) => jsonResponse(202, {'status': 'x'}));
      final r = await api.send('GET', '/healthz', timeout: _timeout);
      expect(r.statusCode, 202);
      expect(r.json, {'status': 'x'});
    });

    test('accept a JSON content type with parameters', () async {
      final api = clientAnswering(
        (_) => http.Response(
          '{"a":1}',
          200,
          headers: {'content-type': 'Application/JSON; charset=utf-8'},
        ),
      );
      expect((await api.send('GET', '/healthz', timeout: _timeout)).json, {
        'a': 1,
      });
    });

    test('an empty 204 has no JSON', () async {
      final api = clientAnswering((_) => noContent());
      final r = await api.send('POST', '/v1/auth/logout', timeout: _timeout);
      expect(r.statusCode, 204);
      expect(r.json, isNull);
    });

    test('a 204 with a body is malformed', () {
      final api = clientAnswering((_) => http.Response('{}', 204));
      expect(
        api.send('POST', '/v1/auth/logout', timeout: _timeout),
        throwsA(_protocol(ProtocolFailure.malformedBody, 204)),
      );
    });

    test('a non-JSON body is refused', () {
      for (final type in [null, 'text/html', 'text/plain', 'garbage;;']) {
        final api = clientAnswering(
          (_) => http.Response.bytes(
            utf8.encode('{}'),
            200,
            headers: {'content-type': ?type},
          ),
        );
        expect(
          api.send('GET', '/healthz', timeout: _timeout),
          throwsA(_protocol(ProtocolFailure.notJson, 200)),
          reason: type,
        );
      }
    });

    test('invalid JSON or UTF-8 is malformed', () {
      for (final body in [
        utf8.encode('{"a":'),
        utf8.encode('null'),
        [0xff, 0xfe, 0x7b],
      ]) {
        final api = clientAnswering(
          (_) => http.Response.bytes(
            body,
            200,
            headers: {'content-type': 'application/json'},
          ),
        );
        expect(
          api.send('GET', '/healthz', timeout: _timeout),
          throwsA(_protocol(ProtocolFailure.malformedBody, 200)),
        );
      }
    });

    test('a body over 64 KiB is refused', () async {
      final api = clientAnswering(
        (_) => http.Response(
          '"${'a' * (64 * 1024)}"',
          200,
          headers: {'content-type': 'application/json'},
        ),
      );
      await expectLater(
        api.send('GET', '/healthz', timeout: _timeout),
        throwsA(_protocol(ProtocolFailure.bodyTooLarge, 200)),
      );
      final exact = clientAnswering(
        (_) => http.Response(
          '"${'a' * (64 * 1024 - 2)}"',
          200,
          headers: {'content-type': 'application/json'},
        ),
      );
      expect(
        (await exact.send('GET', '/healthz', timeout: _timeout)).json,
        isA<String>(),
      );
    });

    test('an oversized error body is refused too', () {
      final api = clientAnswering(
        (_) => http.Response('x' * (64 * 1024 + 1), 500),
      );
      expect(
        api.send('GET', '/healthz', timeout: _timeout),
        throwsA(_protocol(ProtocolFailure.bodyTooLarge, 500)),
      );
    });
  });

  group('other statuses', () {
    test('3xx is a redirect, never followed', () async {
      for (final status in [301, 302, 303, 307, 308, 304]) {
        final api = clientAnswering(
          (_) => http.Response(
            '',
            status,
            headers: {'location': 'https://evil.example/steal'},
          ),
        );
        await expectLater(
          api.send(
            'POST',
            '/v1/auth/refresh',
            json: {'refresh_token': refreshToken('1')},
            timeout: _timeout,
          ),
          throwsA(_protocol(ProtocolFailure.redirect, status)),
        );
        expect(sent, hasLength(1));
      }
    });

    test('1xx and out-of-range statuses are unexpected', () {
      for (final status in [100, 101, 199, 600, 999]) {
        final api = clientAnswering((_) => http.Response('', status));
        expect(
          api.send('GET', '/healthz', timeout: _timeout),
          throwsA(_protocol(ProtocolFailure.unexpectedStatus, status)),
        );
      }
    });

    test('4xx/5xx carry the parsed error and Retry-After', () async {
      final api = clientAnswering(
        (_) =>
            errorResponse(429, 'rate_limited', headers: {'retry-after': '7'}),
      );
      await expectLater(
        api.send('GET', '/healthz', timeout: _timeout),
        throwsA(
          isA<ApiHttpException>()
              .having((e) => e.statusCode, 'status', 429)
              .having((e) => e.code, 'code', 'rate_limited')
              .having(
                (e) => e.retryAfter,
                'retryAfter',
                const Duration(seconds: 7),
              ),
        ),
      );
    });

    test('4xx/5xx with a non-JSON or broken body keep only the status', () {
      final responses = [
        http.Response(
          '404 page not found\n',
          404,
          headers: {'content-type': 'text/plain; charset=utf-8'},
        ),
        http.Response(
          '<html>Bad gateway</html>',
          502,
          headers: {'content-type': 'text/html'},
        ),
        http.Response('', 503),
        http.Response(
          '{"error":',
          500,
          headers: {'content-type': 'application/json'},
        ),
        // A JSON-looking body under a non-JSON type is not trusted.
        http.Response(
          '{"error":{"code":"x"}}',
          500,
          headers: {'content-type': 'text/plain'},
        ),
      ];
      for (final response in responses) {
        final api = clientAnswering((_) => response);
        expect(
          api.send('GET', '/healthz', timeout: _timeout),
          throwsA(
            isA<ApiHttpException>()
                .having((e) => e.statusCode, 'status', response.statusCode)
                .having((e) => e.code, 'code', isNull),
          ),
        );
      }
    });
  });

  group('transport failures', () {
    test('ClientException and IOExceptions are network errors', () {
      for (final error in <Object>[
        http.ClientException('Connection refused', Uri.parse('https://x')),
        const SocketException('Failed host lookup'),
        const HandshakeException('bad cert'),
      ]) {
        final api = apiClientFor(MockClient((_) async => throw error));
        expect(
          api.send('GET', '/healthz', timeout: _timeout),
          throwsA(
            isA<ApiNetworkException>().having(
              (e) => e.toString(),
              'toString',
              'ApiNetworkException',
            ),
          ),
        );
      }
    });

    test('programming errors are not disguised as network errors', () {
      final api = apiClientFor(MockClient((_) async => throw StateError('x')));
      expect(api.send('GET', '/healthz', timeout: _timeout), throwsStateError);
    });

    test('a hung request times out and aborts the request', () {
      fakeAsync((async) {
        http.BaseRequest? seen;
        var aborted = false;
        final api = apiClientFor(
          MockClient.streaming((request, body) {
            seen = request;
            (request as http.Abortable).abortTrigger!.then((_) {
              aborted = true;
            });
            return Completer<http.StreamedResponse>().future;
          }),
        );
        Object? error;
        api
            .send('GET', '/healthz', timeout: const Duration(seconds: 5))
            .catchError((Object e) {
              error = e;
              return const ApiResult(0, null);
            });
        async.elapse(const Duration(seconds: 4));
        expect(error, isNull);
        expect(aborted, isFalse);
        async.elapse(const Duration(seconds: 2));
        expect(seen, isNotNull);
        expect(aborted, isTrue);
        expect(error, isA<ApiTimeoutException>());
      });
    });

    test('a body that stalls also times out', () {
      fakeAsync((async) {
        final api = apiClientFor(
          MockClient.streaming((request, body) async {
            final never = StreamController<List<int>>();
            return http.StreamedResponse(
              never.stream,
              200,
              headers: {'content-type': 'application/json'},
            );
          }),
        );
        Object? error;
        api
            .send('GET', '/healthz', timeout: const Duration(seconds: 5))
            .catchError((Object e) {
              error = e;
              return const ApiResult(0, null);
            });
        async.elapse(const Duration(seconds: 6));
        expect(error, isA<ApiTimeoutException>());
      });
    });

    test('an aborted request is a timeout', () {
      final api = apiClientFor(
        MockClient((r) async => throw http.RequestAbortedException(r.url)),
      );
      expect(
        api.send('GET', '/healthz', timeout: _timeout),
        throwsA(isA<ApiTimeoutException>()),
      );
    });
  });
}

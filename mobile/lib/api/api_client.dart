import 'dart:async';
import 'dart:convert';
import 'dart:io' show IOException;
import 'dart:typed_data';

import 'package:http/http.dart' as http;

import '../auth/auth_tokens.dart';
import '../config.dart';
import 'api_exception.dart';

/// A successful (2xx) response. [json] is null for an empty body.
final class ApiResult {
  const ApiResult(this.statusCode, this.json);

  final int statusCode;
  final Object? json;
}

/// Transport for the VocaTogether API: URL construction, headers, JSON,
/// timeouts, redirects, size limits and error mapping.
///
/// Holds no authentication state: a call that needs an access token is given
/// one. Never retries and never logs. Every request is built from
/// [baseUrl], which is the only cleartext control for Dart traffic (Flutter
/// doesn't apply Android's network security config to `dart:io`).
class ApiClient {
  /// Throws [ConfigException] if [baseUrl] isn't a bare http(s) origin, or
  /// isn't https when [requireHttps] (release builds).
  ApiClient({
    required Uri baseUrl,
    required http.Client httpClient,
    required bool requireHttps,
    this.userAgent = defaultUserAgent,
  }) : baseUrl = parseApiBaseUrl(
         baseUrl.toString(),
         requireHttps: requireHttps,
       ),
       _http = httpClient;

  static const defaultUserAgent = 'VocaTogether-Android';

  /// Responses are tiny; anything larger is refused before it is buffered.
  static const maxBodyBytes = 64 * 1024;

  final Uri baseUrl;
  final String userAgent;
  final http.Client _http;

  static final _path = RegExp(r'^/(healthz|v1(/[a-z0-9-]+)+)$');

  /// Sends one request and returns its 2xx result, or throws an
  /// [ApiException].
  ///
  /// [path] must be one of the fixed API paths (no query, fragment, dots or
  /// encoded characters). [json] is sent as the body. [bearer], if given,
  /// must be a well-formed access token and is sent as
  /// `Authorization: Bearer`; nothing else ever is.
  Future<ApiResult> send(
    String method,
    String path, {
    Map<String, Object?>? json,
    String? bearer,
    required Duration timeout,
  }) async {
    // PUT is only for idempotent replacements (the profile, 027; the user's
    // languages, 029).
    if (method != 'GET' && method != 'POST' && method != 'PUT') {
      throw ArgumentError.value(method, 'method', 'unsupported');
    }
    final url = _url(path);
    if (bearer != null && !isAccessToken(bearer)) {
      // The value is never echoed: it might be another kind of secret.
      throw ArgumentError('bearer is not an access token');
    }

    final abort = Completer<void>();
    final request =
        http.AbortableRequest(method, url, abortTrigger: abort.future)
          ..followRedirects = false
          ..persistentConnection = true;
    request.headers['Accept'] = 'application/json';
    request.headers['User-Agent'] = userAgent;
    if (bearer != null) request.headers['Authorization'] = 'Bearer $bearer';
    if (json != null) {
      request.headers['Content-Type'] = 'application/json';
      request.bodyBytes = utf8.encode(jsonEncode(json));
    }

    // The abort trigger closes a real socket; the Dart-side timeout also
    // bounds clients that ignore aborts (MockClient).
    final timer = Timer(timeout, () {
      if (!abort.isCompleted) abort.complete();
    });
    try {
      return await _exchange(request).timeout(timeout);
    } on TimeoutException {
      throw const ApiTimeoutException();
    } on http.RequestAbortedException {
      throw const ApiTimeoutException();
    } on http.ClientException {
      throw const ApiNetworkException();
    } on IOException {
      // TLS handshake failures and anything else dart:io raises unwrapped.
      throw const ApiNetworkException();
    } finally {
      timer.cancel();
      if (!abort.isCompleted) abort.complete();
    }
  }

  Uri _url(String path) {
    if (!_path.hasMatch(path)) {
      throw ArgumentError.value(path, 'path', 'not an API path');
    }
    final url = baseUrl.replace(path: path);
    if (url.origin != baseUrl.origin || url.hasQuery || url.hasFragment) {
      throw StateError('API URL left the configured origin');
    }
    return url;
  }

  Future<ApiResult> _exchange(http.BaseRequest request) async {
    final response = await _http.send(request);
    final status = response.statusCode;

    if (status >= 300 && status < 400) {
      await _discard(response.stream);
      throw ApiProtocolException(ProtocolFailure.redirect, statusCode: status);
    }
    if (status < 200 || status >= 600) {
      await _discard(response.stream);
      throw ApiProtocolException(
        ProtocolFailure.unexpectedStatus,
        statusCode: status,
      );
    }

    final bytes = await _readCapped(response.stream, status);
    final isJson = _isJson(response.headers['content-type']);

    if (status >= 400) {
      throw ApiHttpException.fromBody(
        status,
        isJson ? _tryDecode(bytes) : null,
        retryAfter: parseRetryAfter(response.headers['retry-after']),
      );
    }
    if (bytes.isEmpty) return ApiResult(status, null);
    if (status == 204) {
      throw ApiProtocolException(
        ProtocolFailure.malformedBody,
        statusCode: status,
      );
    }
    if (!isJson) {
      throw ApiProtocolException(ProtocolFailure.notJson, statusCode: status);
    }
    final json = _tryDecode(bytes);
    if (json == null) {
      throw ApiProtocolException(
        ProtocolFailure.malformedBody,
        statusCode: status,
      );
    }
    return ApiResult(status, json);
  }

  static Future<Uint8List> _readCapped(
    Stream<List<int>> stream,
    int status,
  ) async {
    final builder = BytesBuilder(copy: false);
    // Leaving the loop early (throwing) cancels the subscription, which
    // closes the connection instead of downloading the rest.
    await for (final chunk in stream) {
      if (builder.length + chunk.length > maxBodyBytes) {
        throw ApiProtocolException(
          ProtocolFailure.bodyTooLarge,
          statusCode: status,
        );
      }
      builder.add(chunk);
    }
    return builder.takeBytes();
  }

  static Future<void> _discard(Stream<List<int>> stream) =>
      stream.listen(null).cancel();

  static bool _isJson(String? contentType) {
    if (contentType == null) return false;
    try {
      return http.MediaType.parse(contentType).mimeType == 'application/json';
    } on FormatException {
      return false;
    }
  }

  /// The decoded JSON value, or null if [bytes] aren't UTF-8 JSON. (A literal
  /// `null` body is also null, which no caller accepts.)
  static Object? _tryDecode(Uint8List bytes) {
    try {
      return jsonDecode(utf8.decode(bytes));
    } on FormatException {
      return null;
    }
  }
}

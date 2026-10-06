/// Failures of a call to the VocaTogether API.
///
/// Every exception here is safe to log or show: none holds a token, password,
/// email, request or response body, header, URL or underlying error message.
/// Server-provided codes are kept only when they are plain identifiers.
sealed class ApiException implements Exception {
  const ApiException();
}

/// The server answered with a 4xx or 5xx status.
///
/// [code] and [fields] come from the `{"error":{"code","fields"}}` body
/// (docs/decisions.md 011); they are null and empty when the body is missing
/// or malformed (a proxy's error page, Go's text/plain 404), so [statusCode]
/// is the only reliable value.
final class ApiHttpException extends ApiException {
  ApiHttpException({
    required this.statusCode,
    this.code,
    List<FieldError> fields = const [],
    this.retryAfter,
  }) : fields = List.unmodifiable(fields);

  /// Builds the exception from a decoded error body, which may be anything.
  factory ApiHttpException.fromBody(
    int statusCode,
    Object? body, {
    Duration? retryAfter,
  }) {
    String? code;
    final fields = <FieldError>[];
    if (body case {'error': final Map<String, Object?> error}) {
      code = _identifier(error['code']);
      if (error['fields'] case final List<Object?> list) {
        for (final item in list) {
          if (item case {'field': final Object? f, 'code': final Object? c}) {
            final field = _identifier(f);
            final fieldCode = _identifier(c);
            if (field != null && fieldCode != null) {
              fields.add(FieldError(field, fieldCode));
            }
          }
        }
      }
    }
    return ApiHttpException(
      statusCode: statusCode,
      code: code,
      fields: fields,
      retryAfter: retryAfter,
    );
  }

  final int statusCode;

  /// The backend's error code, e.g. `invalid_credentials`, or null.
  final String? code;

  /// The failing fields of a `validation_failed` (422) response.
  final List<FieldError> fields;

  /// How long to wait before retrying, from `Retry-After` (429 and 503).
  final Duration? retryAfter;

  @override
  String toString() => code == null
      ? 'ApiHttpException($statusCode)'
      : 'ApiHttpException($statusCode, $code)';
}

/// One entry of a `validation_failed` response's `fields`.
final class FieldError {
  const FieldError(this.field, this.code);

  final String field;
  final String code;

  @override
  String toString() => 'FieldError($field, $code)';
}

/// The request failed in transport (DNS, connect, TLS, connection reset).
///
/// The request may or may not have reached the server.
final class ApiNetworkException extends ApiException {
  const ApiNetworkException();

  @override
  String toString() => 'ApiNetworkException';
}

/// No complete response arrived within the request's timeout.
///
/// The request may or may not have reached the server.
final class ApiTimeoutException extends ApiException {
  const ApiTimeoutException();

  @override
  String toString() => 'ApiTimeoutException';
}

/// Why a response broke the API contract.
enum ProtocolFailure {
  /// A status the operation doesn't expect (1xx, an unexpected 2xx, …).
  unexpectedStatus,

  /// A 3xx. Redirects are never followed.
  redirect,

  /// A body that isn't valid JSON or lacks a required field.
  malformedBody,

  /// A body over the client's size limit.
  bodyTooLarge,

  /// A non-empty success body that isn't `application/json`.
  notJson,

  /// A success where an image is expected that isn't `image/jpeg`.
  notImage,
}

/// The server answered, but not as the contract says.
final class ApiProtocolException extends ApiException {
  const ApiProtocolException(this.failure, {this.statusCode});

  final ProtocolFailure failure;
  final int? statusCode;

  @override
  String toString() => statusCode == null
      ? 'ApiProtocolException(${failure.name})'
      : 'ApiProtocolException(${failure.name}, $statusCode)';
}

/// Parses `Retry-After` as whole seconds, clamped to [1 s, 24 h].
///
/// The backend sends only delay-seconds (decision 018); an HTTP date or
/// anything else gives null.
Duration? parseRetryAfter(String? raw) {
  if (raw == null) return null;
  final value = raw.trim();
  if (!_digits.hasMatch(value)) return null;
  // At most 18 digits fit an int; anything longer is over the cap anyway.
  final seconds = value.length > 18 ? _maxRetryAfter : int.parse(value);
  return Duration(seconds: seconds.clamp(1, _maxRetryAfter));
}

const _maxRetryAfter = 24 * 60 * 60;
final _digits = RegExp(r'^[0-9]+$');
final _identifierPattern = RegExp(r'^[a-z][a-z0-9_]{0,63}$');

String? _identifier(Object? value) =>
    value is String && _identifierPattern.hasMatch(value) ? value : null;

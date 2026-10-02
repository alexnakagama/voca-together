import '../api/api_exception.dart';

/// VocaTogether tokens are a prefix plus 32 random bytes in strict unpadded
/// base64url (backend `internal/auth/token.go`): 43 characters, the last of
/// which carries only 4 bits, so its two low bits are zero. Checking the
/// exact shape keeps a refresh token out of the Authorization header, an
/// access token out of the refresh body, and junk out of storage.
final _accessToken = RegExp(r'^vt_at_[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$');
final _refreshToken = RegExp(r'^vt_rt_[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$');

bool isAccessToken(String value) => _accessToken.hasMatch(value);

bool isRefreshToken(String value) => _refreshToken.hasMatch(value);

/// Longest access-token lifetime accepted from the server (it issues 15 min).
const maxAccessLifetime = Duration(hours: 24);

/// A token response from login, Google sign-in or refresh (decision 013):
/// `{"access_token","token_type":"Bearer","expires_in","refresh_token"}`.
///
/// Never leaves the auth/session layer; [toString] is redacted.
final class AuthTokens {
  const AuthTokens._(this.accessToken, this.refreshToken, this.expiresIn);

  /// Validates a decoded 200 body. Anything off-contract is a
  /// [ApiProtocolException] with [ProtocolFailure.malformedBody].
  factory AuthTokens.fromJson(Object? json) {
    if (json
        case {
          'access_token': final String access,
          'token_type': 'Bearer',
          'expires_in': final int expiresIn,
          'refresh_token': final String refresh,
        }
        when isAccessToken(access) &&
            isRefreshToken(refresh) &&
            expiresIn >= 1 &&
            expiresIn <= maxAccessLifetime.inSeconds) {
      return AuthTokens._(access, refresh, Duration(seconds: expiresIn));
    }
    throw const ApiProtocolException(
      ProtocolFailure.malformedBody,
      statusCode: 200,
    );
  }

  final String accessToken;
  final String refreshToken;

  /// The access token's lifetime, relative to when the request was sent.
  final Duration expiresIn;

  @override
  String toString() => 'AuthTokens(<redacted>)';
}

/// What [TokenStore] persists: both tokens and when the access token expires
/// by the wall clock, together, so they can never be stored apart.
///
/// [toString] is redacted.
final class StoredSession {
  const StoredSession({
    required this.accessToken,
    required this.refreshToken,
    required this.accessExpiresAt,
    required this.accessLifetime,
  });

  final String accessToken;
  final String refreshToken;

  /// UTC wall-clock expiry of the access token (send time + `expires_in`).
  final DateTime accessExpiresAt;

  /// The `expires_in` it was issued with. On restore, more time remaining
  /// than this means the wall clock moved backwards.
  final Duration accessLifetime;

  @override
  String toString() => 'StoredSession(<redacted>)';
}

import 'dart:convert';

import '../auth/auth_tokens.dart';
import 'api_client.dart';
import 'api_exception.dart';
import 'api_paths.dart';
import 'me.dart';

/// The token-bearing calls: sign-in, refresh, logout, `GET /v1/me` and the
/// reachability probe.
///
/// **Internal to the session layer** (decision 023): only `main` builds it
/// and only `SessionManager` holds it. It returns [AuthTokens] and takes raw
/// tokens, so it must never reach screens or widgets;
/// `test/architecture_test.dart` enforces that. Screens use `AccountApi` and
/// `SessionManager` instead.
///
/// Stateless transport: it knows paths, bodies and expected statuses, and
/// nothing about sessions. Tokens go in only where the backend reads them:
/// the access token in `Authorization` (logout, me), the refresh token in the
/// refresh body, the Google ID token in the google body. Nothing is retried;
/// failures are [ApiException]s.
class AuthApi {
  AuthApi(this._client);

  final ApiClient _client;

  /// Bounds each call; above the server's 10 s request deadline (018).
  static const requestTimeout = Duration(seconds: 15);
  static const logoutTimeout = Duration(seconds: 10);
  static const healthzTimeout = Duration(seconds: 5);

  /// The backend's verifier refuses longer ID tokens (020).
  static const maxIdTokenBytes = 4096;

  /// `POST /v1/auth/login` → 200 tokens.
  Future<AuthTokens> login({required String email, required String password}) =>
      _tokens(ApiPaths.login, {'email': email, 'password': password});

  /// `POST /v1/auth/google` → 200 tokens. The ID token travels only in this
  /// body (020).
  Future<AuthTokens> google({required String idToken}) {
    if (idToken.isEmpty || utf8.encode(idToken).length > maxIdTokenBytes) {
      throw ArgumentError('idToken is empty or too long');
    }
    return _tokens(ApiPaths.google, {'id_token': idToken});
  }

  /// `POST /v1/auth/refresh` → 200 rotated tokens (014).
  Future<AuthTokens> refresh({required String refreshToken}) {
    if (!isRefreshToken(refreshToken)) {
      throw ArgumentError('refreshToken is not a refresh token');
    }
    return _tokens(ApiPaths.refresh, {'refresh_token': refreshToken});
  }

  /// `POST /v1/auth/logout` with the access token → 204 (015).
  Future<void> logout({required String accessToken}) async {
    final r = await _client.send(
      'POST',
      ApiPaths.logout,
      bearer: accessToken,
      timeout: logoutTimeout,
    );
    _expectStatus(r, 204);
  }

  /// `GET /v1/me` with the access token → 200 (016).
  Future<Me> me({required String accessToken}) async {
    final r = await _client.send(
      'GET',
      ApiPaths.me,
      bearer: accessToken,
      timeout: requestTimeout,
    );
    _expectStatus(r, 200);
    return Me.fromJson(r.json);
  }

  /// `GET /healthz`: whether the API answers at all. Any 2xx is success.
  Future<void> healthz() =>
      _client.send('GET', ApiPaths.healthz, timeout: healthzTimeout);

  Future<AuthTokens> _tokens(String path, Map<String, Object?> body) async {
    final r = await _client.send(
      'POST',
      path,
      json: body,
      timeout: requestTimeout,
    );
    _expectStatus(r, 200);
    return AuthTokens.fromJson(r.json);
  }

  static void _expectStatus(ApiResult r, int expected) {
    if (r.statusCode != expected) {
      throw ApiProtocolException(
        ProtocolFailure.unexpectedStatus,
        statusCode: r.statusCode,
      );
    }
  }
}

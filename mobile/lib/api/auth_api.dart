import 'dart:convert';
import 'dart:typed_data';

import '../auth/auth_tokens.dart';
import 'api_client.dart';
import 'api_exception.dart';
import 'api_paths.dart';
import 'languages.dart';
import 'me.dart';
import 'member_profile.dart';
import 'profile.dart';

/// The token-bearing calls: sign-in, refresh, logout, `GET /v1/me`, the
/// user's profile, picture and languages, the language catalog, a member's
/// public profile and picture, and the reachability probe.
///
/// **Internal to the session layer** (decision 023): only `main` builds it
/// and only `SessionManager` holds it. It returns [AuthTokens] and takes raw
/// tokens, so it must never reach screens or widgets;
/// `test/architecture_test.dart` enforces that. Screens use `AccountApi` and
/// `SessionManager` instead.
///
/// Stateless transport: it knows paths, bodies and expected statuses, and
/// nothing about sessions. Tokens go in only where the backend reads them:
/// the access token in `Authorization` (logout, me, profile, languages,
/// pictures, member profiles), the refresh token in the refresh body, the
/// Google ID token in the google body. Nothing is retried; failures are
/// [ApiException]s.
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

  /// `GET /v1/me/profile` with the access token → 200, or null when the user
  /// hasn't saved a profile (027).
  ///
  /// Null only for the backend's own 404 `profile_not_found`. Any other 404
  /// (a proxy's page, a backend without the route) stays an error, so a
  /// broken deployment never looks like an empty profile.
  Future<Profile?> profile({required String accessToken}) async {
    final ApiResult r;
    try {
      r = await _client.send(
        'GET',
        ApiPaths.profile,
        bearer: accessToken,
        timeout: requestTimeout,
      );
    } on ApiHttpException catch (e) {
      if (e.statusCode == 404 && e.code == 'profile_not_found') return null;
      rethrow;
    }
    _expectStatus(r, 200);
    return Profile.fromJson(r.json);
  }

  /// `PUT /v1/me/profile` with the access token → 200 with the profile as
  /// stored (027). Replaces the whole profile, creating it if there is none.
  /// The text is sent as given; the backend normalizes and validates it.
  ///
  /// Idempotent on the server, so sending the same save twice is harmless.
  Future<Profile> saveProfile({
    required String accessToken,
    required String displayName,
    required String bio,
  }) async {
    final r = await _client.send(
      'PUT',
      ApiPaths.profile,
      json: {'display_name': displayName, 'bio': bio},
      bearer: accessToken,
      timeout: requestTimeout,
    );
    _expectStatus(r, 200);
    return Profile.fromJson(r.json);
  }

  /// `GET /v1/languages` with the access token → 200 with the catalog,
  /// ordered by English name (029).
  Future<List<Language>> languageCatalog({required String accessToken}) async {
    final r = await _client.send(
      'GET',
      ApiPaths.languages,
      bearer: accessToken,
      timeout: requestTimeout,
    );
    _expectStatus(r, 200);
    return Language.catalogFromJson(r.json);
  }

  /// `GET /v1/me/languages` with the access token → 200 with the user's own
  /// languages (029).
  ///
  /// A user who has chosen none gets two empty lists: unlike [profile] there
  /// is no "none saved" 404, so every 404 here stays an error.
  Future<UserLanguages> languages({required String accessToken}) async {
    final r = await _client.send(
      'GET',
      ApiPaths.myLanguages,
      bearer: accessToken,
      timeout: requestTimeout,
    );
    _expectStatus(r, 200);
    return UserLanguages.fromJson(r.json);
  }

  /// `PUT /v1/me/languages` with the access token → 200 with the languages
  /// as stored (029). Replaces the user's whole selection with [languages];
  /// the body is its [UserLanguages.toJson], which always holds both lists.
  /// It is sent as given; the backend validates it.
  ///
  /// Idempotent on the server, so sending the same save twice is harmless.
  Future<UserLanguages> saveLanguages({
    required String accessToken,
    required UserLanguages languages,
  }) async {
    final r = await _client.send(
      'PUT',
      ApiPaths.myLanguages,
      json: languages.toJson(),
      bearer: accessToken,
      timeout: requestTimeout,
    );
    _expectStatus(r, 200);
    return UserLanguages.fromJson(r.json);
  }

  /// `GET /v1/me/avatar` with the access token → 200 with the user's own
  /// picture, a JPEG, or null when they have none (031).
  ///
  /// Null only for the backend's own 404 `avatar_not_found`; any other 404
  /// stays an error, as for [profile].
  Future<Uint8List?> avatar({required String accessToken}) =>
      _imageOrNull(ApiPaths.myAvatar, accessToken);

  /// `PUT /v1/me/avatar` with the access token → 200 with the picture as
  /// stored (031). [image] is the whole body, sent as given: the backend
  /// decides what it accepts and stores its own re-encoding of it.
  ///
  /// Idempotent on the server, so sending the same bytes twice is harmless.
  Future<Uint8List> saveAvatar({
    required String accessToken,
    required Uint8List image,
  }) => _client.putForImage(
    ApiPaths.myAvatar,
    bytes: image,
    bearer: accessToken,
    timeout: requestTimeout,
  );

  /// `DELETE /v1/me/avatar` with the access token → 204, also when there
  /// was no picture (031), so sending it twice is harmless.
  Future<void> removeAvatar({required String accessToken}) async {
    final r = await _client.send(
      'DELETE',
      ApiPaths.myAvatar,
      bearer: accessToken,
      timeout: requestTimeout,
    );
    _expectStatus(r, 204);
  }

  /// `GET /v1/profiles/{id}` with the access token → 200 with the public
  /// profile [id] names, or null when it names none (031).
  ///
  /// Null only for the backend's own 404 `profile_not_found`; any other 404
  /// stays an error, as for [profile]. An [id] that isn't a canonical public
  /// identifier is an [ArgumentError] and nothing is sent.
  Future<MemberProfile?> memberProfile({
    required String accessToken,
    required String id,
  }) async {
    final path = ApiPaths.memberProfile(id);
    final ApiResult r;
    try {
      r = await _client.send(
        'GET',
        path,
        bearer: accessToken,
        timeout: requestTimeout,
      );
    } on ApiHttpException catch (e) {
      if (e.statusCode == 404 && e.code == 'profile_not_found') return null;
      rethrow;
    }
    _expectStatus(r, 200);
    return MemberProfile.fromJson(r.json);
  }

  /// `GET /v1/profiles/{id}/avatar` with the access token → 200 with the
  /// picture of the member [id] names, a JPEG, or null when that member has
  /// none (031).
  ///
  /// Null only for the backend's own 404 `avatar_not_found`. Any other 404
  /// stays an error, `profile_not_found` included: whether [id] names a
  /// profile is [memberProfile]'s answer. [id] as for [memberProfile].
  Future<Uint8List?> memberAvatar({
    required String accessToken,
    required String id,
  }) async => _imageOrNull(ApiPaths.memberAvatar(id), accessToken);

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

  Future<Uint8List?> _imageOrNull(String path, String accessToken) async {
    try {
      return await _client.getImage(
        path,
        bearer: accessToken,
        timeout: requestTimeout,
      );
    } on ApiHttpException catch (e) {
      if (e.statusCode == 404 && e.code == 'avatar_not_found') return null;
      rethrow;
    }
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

import 'package:flutter/foundation.dart';

/// Build-time configuration, compiled in with
/// `--dart-define-from-file=config/<env>.json`.
///
/// Every value here is public: anything compiled into the APK can be read
/// from it. Never put a secret here.
class AppConfig {
  const AppConfig({required this.apiBaseUrl, this.googleServerClientId});

  /// Reads and validates the compile-time environment.
  ///
  /// Throws a [ConfigException] when a value is missing or invalid, so a
  /// misconfigured build fails at startup instead of on its first request.
  /// Release builds require https and the Google client ID.
  factory AppConfig.fromEnvironment() {
    return AppConfig(
      apiBaseUrl: parseApiBaseUrl(
        const String.fromEnvironment('API_BASE_URL'),
        requireHttps: kReleaseMode,
      ),
      googleServerClientId: parseGoogleServerClientId(
        const String.fromEnvironment('GOOGLE_SERVER_CLIENT_ID'),
        required: kReleaseMode,
      ),
    );
  }

  /// The VocaTogether API origin, e.g. `https://api.example.com`, without a
  /// path.
  final Uri apiBaseUrl;

  /// The Web OAuth client ID that Google ID tokens are issued for: the same
  /// value as the backend's `GOOGLE_CLIENT_ID` (docs/decisions.md 020, 025).
  /// Public, like every client ID; never an Android client ID, and never a
  /// client secret. Null only in a debug build without it, where Google
  /// sign-in is off.
  final String? googleServerClientId;
}

/// Thrown when the build-time configuration is missing or invalid.
///
/// Messages name the variable but never echo its value.
class ConfigException implements Exception {
  const ConfigException(this.message);

  final String message;

  @override
  String toString() => 'ConfigException: $message';
}

/// Validates `API_BASE_URL`: an absolute http(s) origin with a host and
/// nothing else (no credentials, path, query or fragment; the client appends
/// paths such as `/v1/me`). With [requireHttps], only https is accepted.
Uri parseApiBaseUrl(String raw, {required bool requireHttps}) {
  const name = 'API_BASE_URL';
  if (raw.isEmpty) {
    throw const ConfigException(
      '$name is required; run with --dart-define-from-file=config/dev.json',
    );
  }
  if (raw.contains(RegExp(r'\s'))) {
    throw const ConfigException('$name must not contain whitespace');
  }
  final uri = Uri.tryParse(raw);
  if (uri == null || !uri.hasScheme || !uri.hasAuthority) {
    throw const ConfigException('$name must be an absolute URL');
  }
  if (uri.scheme != 'https' && uri.scheme != 'http') {
    throw const ConfigException('$name must use http or https');
  }
  if (requireHttps && uri.scheme != 'https') {
    throw const ConfigException('$name must use https in release builds');
  }
  if (uri.host.isEmpty) {
    throw const ConfigException('$name must include a host');
  }
  if (uri.userInfo.isNotEmpty) {
    throw const ConfigException('$name must not include credentials');
  }
  if (uri.path.isNotEmpty || uri.hasQuery || uri.hasFragment) {
    throw const ConfigException(
      '$name must not include a path, query or fragment',
    );
  }
  return uri;
}

/// Validates `GOOGLE_SERVER_CLIENT_ID` with the backend's rule for its
/// `GOOGLE_CLIENT_ID` (`googleid.ValidClientID`): at most 255 characters of
/// printable ASCII without spaces, ending in `.apps.googleusercontent.com`
/// after a non-empty prefix. Google documents no grammar for the prefix, so
/// nothing more is assumed. This catches misconfiguration (a client secret,
/// stray whitespace or quotes); the security check is the backend's exact
/// `aud` match.
///
/// An empty value gives null unless [required]; an invalid one always throws.
String? parseGoogleServerClientId(String raw, {required bool required}) {
  const name = 'GOOGLE_SERVER_CLIENT_ID';
  const suffix = '.apps.googleusercontent.com';
  const maxLength = 255;
  if (raw.isEmpty) {
    if (!required) return null;
    throw const ConfigException(
      '$name is required in release builds: the Web OAuth client ID',
    );
  }
  if (raw.length > maxLength ||
      raw.codeUnits.any((c) => c < 0x21 || c > 0x7e)) {
    throw const ConfigException(
      '$name must be at most $maxLength printable ASCII characters '
      'without spaces',
    );
  }
  if (raw.length <= suffix.length || !raw.endsWith(suffix)) {
    throw const ConfigException(
      '$name must be a Google OAuth client ID ending in $suffix',
    );
  }
  return raw;
}

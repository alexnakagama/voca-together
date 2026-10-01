import 'package:flutter/foundation.dart';

/// Build-time configuration, compiled in with
/// `--dart-define-from-file=config/<env>.json`.
///
/// Every value here is public: anything compiled into the APK can be read
/// from it. Never put a secret here.
class AppConfig {
  const AppConfig({required this.apiBaseUrl});

  /// Reads and validates the compile-time environment.
  ///
  /// Throws a [ConfigException] when a value is missing or invalid, so a
  /// misconfigured build fails at startup instead of on its first request.
  /// Release builds require https.
  factory AppConfig.fromEnvironment() {
    return AppConfig(
      apiBaseUrl: parseApiBaseUrl(
        const String.fromEnvironment('API_BASE_URL'),
        requireHttps: kReleaseMode,
      ),
    );
  }

  /// The VocaTogether API origin, e.g. `https://api.example.com`, without a
  /// path.
  final Uri apiBaseUrl;
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

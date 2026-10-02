import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/io_client.dart';

import 'api/api_client.dart';
import 'api/auth_api.dart';
import 'app.dart';
import 'auth/token_store.dart';
import 'config.dart';
import 'session.dart';

/// The composition root: every long-lived object is built here and passed
/// down through constructors.
void main() {
  // The token store uses a platform channel during restore.
  WidgetsFlutterBinding.ensureInitialized();
  // Validate configuration before anything else, so a misconfigured build
  // fails at once.
  final config = AppConfig.fromEnvironment();

  // One HTTP client for the life of the process. Our User-Agent header
  // replaces dart:io's default one.
  final httpClient = IOClient(
    HttpClient()
      ..connectionTimeout = const Duration(seconds: 10)
      ..userAgent = null,
  );
  final apiClient = ApiClient(
    baseUrl: config.apiBaseUrl,
    httpClient: httpClient,
    requireHttps: kReleaseMode,
  );
  final session = SessionManager(
    store: SecureTokenStore(const FlutterSecureStorage()),
    authApi: AuthApi(apiClient),
  );

  runApp(VocaTogetherApp(config: config, session: session));
  // The splash screen shows until the stored session has been read.
  unawaited(session.restore());
}

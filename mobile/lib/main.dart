import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/io_client.dart';

import 'api/account_api.dart';
import 'api/api_client.dart';
import 'api/auth_api.dart';
import 'app.dart';
import 'auth/google_identity_plugin.dart';
import 'auth/token_store.dart';
import 'config.dart';
import 'media/photo_source_plugin.dart';
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
  // Google sign-in exists only with a client ID, which release builds
  // require (config.dart). Nothing touches Google until the user asks to.
  final googleClientId = config.googleServerClientId;
  final session = SessionManager(
    store: SecureTokenStore(const FlutterSecureStorage()),
    authApi: AuthApi(apiClient),
    google: googleClientId == null
        ? null
        : PluginGoogleIdentity(serverClientId: googleClientId),
  );

  // The token-free account calls screens may make (decision 023).
  final accountApi = AccountApi(apiClient);

  // The system's photo chooser (decision 032). Nothing opens it until the
  // member asks to.
  final photoSource = PluginPhotoSource();

  runApp(
    VocaTogetherApp(
      config: config,
      session: session,
      accountApi: accountApi,
      photoSource: photoSource,
    ),
  );
  // The splash screen shows until the stored session has been read.
  unawaited(session.restore());
}

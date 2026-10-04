import 'package:google_sign_in/google_sign_in.dart' as gsi;

import 'google_identity.dart';
import 'google_identity_exception.dart';

/// [GoogleIdentity] over the `google_sign_in` plugin (Credential Manager on
/// Android), and the only library that imports it (decision 025).
///
/// It asks Google for one thing, an ID token whose audience is
/// [serverClientId], and reads nothing else from the answer: no email, name,
/// photo or account id. It requests no scopes, so no Google access token or
/// server auth code ever exists, and it never signs in silently
/// (`attemptLightweightAuthentication`) or listens to the plugin's events.
/// No nonce and no hosted domain are set (020). Nothing is cached here, and
/// nothing the plugin reports is logged or kept. Google itself may return
/// the same ID token on a later call while that token is valid; the backend
/// accepts it again (026).
class PluginGoogleIdentity implements GoogleIdentity {
  PluginGoogleIdentity({required this.serverClientId});

  /// The Web OAuth client ID (`GOOGLE_SERVER_CLIENT_ID`), which Google puts
  /// in the token's `aud` and the backend matches exactly. Public.
  final String serverClientId;

  // Three base64url segments: the shape of a signed JWT. Nothing is decoded;
  // verifying the token is the backend's job.
  static final _jwtShape = RegExp(
    r'^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$',
  );

  final gsi.GoogleSignIn _plugin = gsi.GoogleSignIn.instance;
  Future<void>? _initialized;
  bool _running = false;

  /// The plugin must be initialized exactly once before any other call
  /// (calling it again is undefined behavior), so the attempt is made on
  /// first use and its outcome, a failure included, is final for the
  /// process.
  Future<void> _initialize() =>
      _initialized ??= _plugin.initialize(serverClientId: serverClientId);

  @override
  Future<String> idToken() async {
    if (_running) throw StateError('a Google sign-in is already in progress');
    _running = true;
    try {
      try {
        await _initialize();
      } on Object {
        throw const GoogleIdentityException(GoogleIdentityFailure.unavailable);
      }
      if (!_plugin.supportsAuthenticate()) {
        throw const GoogleIdentityException(GoogleIdentityFailure.unavailable);
      }
      final account = await _plugin.authenticate();
      final token = account.authentication.idToken;
      if (token == null ||
          token.length > GoogleIdentity.maxIdTokenBytes ||
          !_jwtShape.hasMatch(token)) {
        throw const GoogleIdentityException(GoogleIdentityFailure.malformed);
      }
      return token;
    } on GoogleIdentityException {
      rethrow;
    } on gsi.GoogleSignInException catch (e) {
      throw GoogleIdentityException(_failureOf(e.code));
    } on Object {
      // A PlatformException, a missing plugin, an Error from the plugin:
      // none of it is the caller's to interpret, and its text isn't kept.
      throw const GoogleIdentityException(GoogleIdentityFailure.unknown);
    } finally {
      _running = false;
    }
  }

  @override
  Future<void> clear() async {
    try {
      await _initialize();
      await _plugin.signOut();
    } on Object {
      // Best effort: the caller's logout doesn't depend on Google.
    }
  }

  static GoogleIdentityFailure _failureOf(
    gsi.GoogleSignInExceptionCode code,
  ) => switch (code) {
    gsi.GoogleSignInExceptionCode.canceled => GoogleIdentityFailure.cancelled,
    gsi.GoogleSignInExceptionCode.interrupted =>
      GoogleIdentityFailure.interrupted,
    gsi.GoogleSignInExceptionCode.uiUnavailable ||
    gsi.GoogleSignInExceptionCode.providerConfigurationError =>
      GoogleIdentityFailure.unavailable,
    gsi.GoogleSignInExceptionCode.clientConfigurationError =>
      GoogleIdentityFailure.misconfigured,
    gsi.GoogleSignInExceptionCode.userMismatch ||
    gsi.GoogleSignInExceptionCode.unknownError => GoogleIdentityFailure.unknown,
  };
}

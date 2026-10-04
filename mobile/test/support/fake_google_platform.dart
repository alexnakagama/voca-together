import 'dart:async';

import 'package:google_sign_in_platform_interface/google_sign_in_platform_interface.dart';

const fakeGoogleEmail = 'leak.email@example.com';

/// The platform side of `google_sign_in`, scripted. `GoogleSignIn.instance`
/// is a process-wide singleton that forwards everything here.
class FakeGooglePlatform extends GoogleSignInPlatform {
  final inits = <InitParameters>[];
  Object? initError;
  bool supports = true;

  final authenticates = <AuthenticateParameters>[];
  final _results = <Future<AuthenticationResults> Function()>[];

  int signOuts = 0;
  Object? signOutError;

  /// When set, [signOut] waits for it: a slow `clearCredentialState`.
  Completer<void>? signOutGate;

  /// Every call, in order: `authenticate`, `signOut:start`, `signOut:end`.
  final log = <String>[];

  /// Calls this app must never make.
  final forbidden = <String>[];

  void answer(String? idToken) => _results.add(
    () async => AuthenticationResults(
      user: const GoogleSignInUserData(email: fakeGoogleEmail, id: 'LEAKsub'),
      authenticationTokens: AuthenticationTokenData(idToken: idToken),
    ),
  );

  void fail(Object error) => _results.add(() async => throw error);

  void wait(Completer<AuthenticationResults> gate) =>
      _results.add(() => gate.future);

  @override
  Future<void> init(InitParameters params) async {
    inits.add(params);
    if (initError case final e?) throw e;
  }

  @override
  bool supportsAuthenticate() => supports;

  @override
  Future<AuthenticationResults> authenticate(AuthenticateParameters params) {
    authenticates.add(params);
    log.add('authenticate');
    return _results.removeAt(0)();
  }

  @override
  Future<void> signOut(SignOutParams params) async {
    signOuts++;
    log.add('signOut:start');
    try {
      await signOutGate?.future;
      if (signOutError case final e?) throw e;
    } finally {
      log.add('signOut:end');
    }
  }

  @override
  Future<AuthenticationResults?>? attemptLightweightAuthentication(
    AttemptLightweightAuthenticationParameters params,
  ) {
    forbidden.add('attemptLightweightAuthentication');
    return null;
  }

  @override
  bool authorizationRequiresUserInteraction() {
    forbidden.add('authorizationRequiresUserInteraction');
    return false;
  }

  @override
  Future<ClientAuthorizationTokenData?> clientAuthorizationTokensForScopes(
    ClientAuthorizationTokensForScopesParameters params,
  ) async {
    forbidden.add('clientAuthorizationTokensForScopes');
    return null;
  }

  @override
  Future<ServerAuthorizationTokenData?> serverAuthorizationTokensForScopes(
    ServerAuthorizationTokensForScopesParameters params,
  ) async {
    forbidden.add('serverAuthorizationTokensForScopes');
    return null;
  }

  @override
  Future<void> disconnect(DisconnectParams params) async {
    forbidden.add('disconnect');
  }
}

/// Where the session gets Google ID tokens (decision 025).
///
/// **Internal to the session layer**, like `AuthApi`: only `main` builds an
/// implementation and only `SessionManager` holds it, because [idToken]
/// returns a raw credential. `test/architecture_test.dart` enforces that.
abstract interface class GoogleIdentity {
  /// The backend's verifier refuses longer ID tokens (020); equal to
  /// `AuthApi.maxIdTokenBytes`.
  static const maxIdTokenBytes = 4096;

  /// Asks the user to choose a Google account and returns the ID token
  /// Google gives for it. That may be the same token as an earlier call's
  /// (Google reuses one while it is valid, and the backend accepts it again,
  /// 026), but the caller never relies on that: the result is used for one
  /// request and never kept.
  ///
  /// Throws `GoogleIdentityException` when there is no usable token, and
  /// [StateError] if a call is already running.
  Future<String> idToken();

  /// Forgets the Google account chosen on this device, best effort. Never
  /// throws. It doesn't revoke anything at Google.
  Future<void> clear();
}

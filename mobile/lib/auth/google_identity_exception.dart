/// Why Google gave the app no usable ID token.
enum GoogleIdentityFailure {
  /// The user closed Google's account chooser.
  cancelled,

  /// The flow was interrupted by something other than the user.
  interrupted,

  /// Google's sign-in can't run on this device right now (no Play services,
  /// no UI to show it on, initialization failed).
  unavailable,

  /// Google refused this app's configuration (client ID, package name or
  /// signing certificate).
  misconfigured,

  /// Google answered without an ID token, or with something that can't be
  /// one.
  malformed,

  /// Anything else.
  unknown,
}

/// Google sign-in ended without an ID token (decision 025).
///
/// Safe to log or show: it holds only [failure]. Whatever the plugin said
/// (its messages can name the account or the client) is dropped where this
/// is created.
final class GoogleIdentityException implements Exception {
  const GoogleIdentityException(this.failure);

  final GoogleIdentityFailure failure;

  @override
  String toString() => 'GoogleIdentityException(${failure.name})';
}

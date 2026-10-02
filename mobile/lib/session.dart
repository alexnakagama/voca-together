import 'package:flutter/foundation.dart';

/// Whether the app has a signed-in user.
enum SessionStatus {
  /// Startup has not yet determined whether a session exists.
  unknown,

  /// No user is signed in.
  signedOut,

  /// A user is signed in.
  signedIn,
}

/// The app's session state, observed by the router.
///
/// A state holder only: it stores no tokens and does no I/O. Whatever learns
/// the session's status (startup restore, sign-in, sign-out) reports it here,
/// and listeners are notified only when the status actually changes.
///
/// Owned by `main`, which keeps it for the life of the process; widgets
/// listen to it but never dispose it.
class Session extends ChangeNotifier {
  Session({SessionStatus initial = SessionStatus.unknown}) : _status = initial;

  SessionStatus _status;

  SessionStatus get status => _status;

  void markSignedIn() => _set(SessionStatus.signedIn);

  void markSignedOut() => _set(SessionStatus.signedOut);

  void _set(SessionStatus status) {
    if (status == _status) return;
    _status = status;
    notifyListeners();
  }
}

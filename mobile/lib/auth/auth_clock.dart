/// The two clocks access-token expiry is judged by.
///
/// The wall clock survives process restarts but the user can change it; the
/// monotonic clock can't be changed but restarts with the process and, on
/// Android, may stop while the device sleeps. `SessionManager` treats a token
/// as stale when either says so, and a 401 from the server always wins.
class AuthClock {
  AuthClock() : _stopwatch = Stopwatch()..start();

  final Stopwatch _stopwatch;

  /// The current wall-clock time, in UTC.
  DateTime now() => DateTime.now().toUtc();

  /// Monotonic time since this clock was created.
  Duration elapsed() => _stopwatch.elapsed;
}

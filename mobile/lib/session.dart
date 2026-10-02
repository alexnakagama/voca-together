import 'dart:async';

import 'package:flutter/foundation.dart';

import 'api/api_exception.dart';
import 'api/auth_api.dart';
import 'api/me.dart';
import 'auth/auth_clock.dart';
import 'auth/auth_tokens.dart';
import 'auth/token_store.dart';

/// Whether the app has a signed-in user.
enum SessionStatus {
  /// Startup hasn't restored the stored session yet (the splash screen).
  unknown,

  /// No user is signed in.
  signedOut,

  /// A user is signed in.
  signedIn,
}

/// The call needs a session and there is none, or it ended (logout, a
/// refused refresh) while the call was in progress.
class SignedOutException implements Exception {
  const SignedOutException();

  @override
  String toString() => 'SignedOutException';
}

/// The session: its status, which the router observes, and every policy that
/// involves tokens (docs/decisions.md 014, 015, 018, 023).
///
/// The only holder of tokens in memory, and the only user of [TokenStore]
/// and [AuthApi]. Its public API neither takes nor returns a token: screens
/// get [status], `void` from sign-in and logout, typed results such as [Me],
/// and exceptions. Tokens leave it only as arguments of its own [AuthApi]
/// calls, inside this library (decision 023).
///
/// Concurrency (one isolate, so races exist only across `await`s):
/// - Every token change creates a new session object with a new generation.
///   A 401 for a request sent with an older generation means the tokens
///   changed in flight: retry with the current ones, don't refresh.
/// - At most one refresh runs at a time; concurrent callers share its
///   Future, so N simultaneous 401s rotate the refresh token once.
/// - Logout waits for a running refresh or sign-in and blocks new ones, then
///   clears memory and storage before it calls the server, so nothing can
///   bring the session back.
///
/// Owned by `main` for the life of the process; widgets listen to it but
/// never dispose it.
class SessionManager extends ChangeNotifier {
  SessionManager({
    required this._store,
    required AuthApi authApi,
    AuthClock? clock,
  }) : _api = authApi,
       _clock = clock ?? AuthClock();

  /// An access token this close to expiry is refreshed before use, so it
  /// doesn't expire in flight (requests time out after 15 s).
  static const refreshMargin = Duration(seconds: 30);

  /// Startup gives up on a storage read that hangs and starts signed out.
  static const restoreTimeout = Duration(seconds: 10);

  /// Used when a 429 from refresh lacks a valid `Retry-After` (the backend
  /// always sends one).
  static const defaultRetryAfter = Duration(seconds: 5);

  final TokenStore _store;
  final AuthApi _api;
  final AuthClock _clock;

  SessionStatus _status = SessionStatus.unknown;
  _Session? _session;
  int _generation = 0;

  Future<void>? _restoring;
  Future<void>? _signingIn;
  Future<_Session>? _refreshing;
  Future<void>? _loggingOut;

  /// Monotonic time before which a refresh must not be sent (after a 429).
  Duration? _refreshNotBefore;
  bool _disposed = false;

  SessionStatus get status => _status;

  /// Restores the stored session, once, without using the network, so an
  /// offline start still opens signed in. Never throws: any storage problem
  /// starts signed out. Calling it again returns the same Future.
  Future<void> restore() {
    _checkNotDisposed();
    return _restoring ??= _restore();
  }

  Future<void> _restore() async {
    StoredSession? stored;
    try {
      stored = await _store.read().timeout(restoreTimeout);
    } on TokenStoreCorruptException {
      await _clearStore();
    } on Exception {
      // Storage failed or hung: start signed out and leave the data alone,
      // so the next launch can try again.
    }
    if (_disposed || _status != SessionStatus.unknown) return;
    if (stored != null) {
      _session = _Session.restored(stored, ++_generation, _clock);
      _setStatus(SessionStatus.signedIn);
    } else {
      _setStatus(SessionStatus.signedOut);
    }
  }

  /// Signs in with email and password (`POST /v1/auth/login`).
  ///
  /// Only while signed out with no other sign-in or logout running
  /// (otherwise [StateError]). API failures are rethrown unchanged for the UI
  /// to explain; nothing is retried. If the tokens can't be stored, throws
  /// [TokenStoreException] and stays signed out.
  Future<void> signIn({required String email, required String password}) =>
      _signIn(() => _api.login(email: email, password: password));

  /// Signs in with a Google ID token (`POST /v1/auth/google`), like [signIn].
  /// After a 500, 503 or timeout, retry only with a new ID token (020).
  Future<void> signInWithGoogle({required String idToken}) =>
      _signIn(() => _api.google(idToken: idToken));

  Future<void> _signIn(Future<AuthTokens> Function() request) {
    _checkNotDisposed();
    if (_status != SessionStatus.signedOut ||
        _signingIn != null ||
        _loggingOut != null) {
      throw StateError(
        'sign-in needs a signed-out session and no other '
        'sign-in or logout in progress',
      );
    }
    return _signingIn = _singleFlight(
      _runSignIn(request),
      () => _signingIn = null,
    );
  }

  Future<void> _runSignIn(Future<AuthTokens> Function() request) async {
    final sentWall = _clock.now();
    final sentMono = _clock.elapsed();
    final tokens = await request();
    final session = _Session.issued(tokens, sentWall, sentMono, ++_generation);
    // Stored first: a session that can't be persisted isn't started.
    await _store.write(session.toStored());
    if (_disposed) return;
    _session = session;
    _refreshNotBefore = null;
    _setStatus(SessionStatus.signedIn);
  }

  /// The signed-in user (`GET /v1/me`), with the request-level token
  /// handling of [_authorized]. Throws [SignedOutException] without a session.
  Future<Me> me() {
    _checkNotDisposed();
    return _authorized((token) => _api.me(accessToken: token));
  }

  /// Runs [call] with a valid access token and returns its result.
  ///
  /// Private on purpose: [call] receives the raw access token, so only this
  /// library's typed methods (such as [me]) may pass one, each a single
  /// [AuthApi] request that sends the token as given. Per decision 014:
  /// - a token at or near expiry is refreshed first;
  /// - on a 401, if the tokens changed while the request was in flight, it is
  ///   resent once with the new token and no refresh; otherwise the session
  ///   refreshes (shared with any concurrent caller) and it is resent once;
  /// - at most two sends and one refresh per call; a second 401 is rethrown.
  ///
  /// Throws [SignedOutException] when there is no session or it ends
  /// meanwhile, the refresh's error when it fails (see [_runRefresh]), and
  /// any other failure of [call] unchanged.
  Future<T> _authorized<T>(Future<T> Function(String accessToken) call) async {
    var sent = _requireSession();
    var refreshed = false;
    if (_isStale(sent) && !_refreshBlocked) {
      await _refresh(sent.generation);
      // Re-checked after every wait: a logout may have begun meanwhile.
      sent = _requireSession();
      refreshed = true;
    }

    try {
      return await call(sent.accessToken);
    } on ApiHttpException catch (e) {
      if (e.statusCode != 401) rethrow;
      final current = _requireSession();
      if (current.generation == sent.generation) {
        if (refreshed) {
          // A token fresh from a refresh was refused. Don't loop: the next
          // call refreshes, and a revoked session then ends there.
          current.forceStale = true;
          rethrow;
        }
        await _refresh(sent.generation);
        sent = _requireSession();
      } else {
        sent = current;
      }
    }

    try {
      return await call(sent.accessToken);
    } on ApiHttpException catch (e) {
      if (e.statusCode == 401) {
        final current = _session;
        if (current != null && current.generation == sent.generation) {
          current.forceStale = true;
        }
      }
      rethrow;
    }
  }

  /// Signs out (decision 015): waits for any refresh or sign-in, deletes the
  /// tokens from memory and storage, goes signed out, then revokes the
  /// session on the server with the latest access token. The server's answer,
  /// or its absence, changes nothing. Never throws.
  Future<void> logout() {
    _checkNotDisposed();
    return _loggingOut ??= _runLogout();
  }

  Future<void> _runLogout() async {
    String? access;
    // `_loggingOut` covers only the local phase: it blocks sign-in and token
    // use until storage is cleared, and is released before the server call
    // so a new sign-in needn't wait for it. The `finally` runs after the
    // first `await`, i.e. after `logout` has assigned it.
    try {
      await _restoring;
      await _signingIn?.then((_) {}, onError: (Object _) {});
      await _refreshing?.then((_) {}, onError: (Object _) {});
      access = _session?.accessToken;
      _session = null;
      _generation++;
      _refreshNotBefore = null;
      if (!await _clearStore()) await _clearStore();
      _setStatus(SessionStatus.signedOut);
    } finally {
      _loggingOut = null;
    }
    if (access != null && !_disposed) {
      try {
        await _api.logout(accessToken: access);
      } on ApiException {
        // Every outcome is final: the local session is already gone.
      }
    }
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }

  /// The current session, or [SignedOutException].
  _Session _requireSession() {
    final session = _session;
    if (_loggingOut != null ||
        _status != SessionStatus.signedIn ||
        session == null) {
      throw const SignedOutException();
    }
    return session;
  }

  bool _isStale(_Session s) =>
      s.forceStale ||
      _clock.elapsed() >= s.monoDeadline - refreshMargin ||
      !_clock.now().isBefore(s.wallDeadline.subtract(refreshMargin));

  bool get _refreshBlocked {
    final notBefore = _refreshNotBefore;
    return notBefore != null && _clock.elapsed() < notBefore;
  }

  /// Refreshes the session last seen at [fromGeneration], at most once at a
  /// time. If the tokens already changed since, returns the current session.
  Future<_Session> _refresh(int fromGeneration) {
    final current = _requireSession();
    if (current.generation != fromGeneration) return Future.value(current);
    final running = _refreshing;
    if (running != null) return running;
    final notBefore = _refreshNotBefore;
    if (notBefore != null) {
      final wait = notBefore - _clock.elapsed();
      if (wait > Duration.zero) {
        return Future.error(
          ApiHttpException(
            statusCode: 429,
            code: 'rate_limited',
            retryAfter: Duration(seconds: (wait.inMilliseconds / 1000).ceil()),
          ),
        );
      }
    }
    return _refreshing = _singleFlight(
      _runRefresh(current),
      () => _refreshing = null,
    );
  }

  /// Mirrors [work] in a new Future, running [release] (which frees the
  /// caller's single-flight slot) before that Future completes. So no
  /// waiter ever sees the slot still taken by finished work, even if [work]
  /// failed synchronously, before the slot was assigned.
  static Future<T> _singleFlight<T>(Future<T> work, void Function() release) {
    final done = Completer<T>();
    work.then(
      (value) {
        release();
        done.complete(value);
      },
      onError: (Object error, StackTrace stack) {
        release();
        done.completeError(error, stack);
      },
    );
    return done.future;
  }

  /// One refresh, following the matrix of decision 023:
  /// - the `/healthz` probe fails: its error, tokens kept (nothing was sent
  ///   that could rotate them);
  /// - 200: the rotated tokens replace the old ones;
  /// - 429: its error, tokens kept, no refresh until `Retry-After` has passed,
  ///   then the same refresh token may be sent again (018);
  /// - anything else (401, other 4xx, 5xx, timeout, network error, a
  ///   malformed or unexpected response): the rotation may have committed,
  ///   and resending the old token would be reuse (014), so the session ends:
  ///   [SignedOutException].
  Future<_Session> _runRefresh(_Session current) async {
    await _api.healthz();

    final sentWall = _clock.now();
    final sentMono = _clock.elapsed();
    final AuthTokens tokens;
    try {
      tokens = await _api.refresh(refreshToken: current.refreshToken);
    } on ApiHttpException catch (e) {
      if (e.statusCode == 429) {
        _refreshNotBefore =
            _clock.elapsed() + (e.retryAfter ?? defaultRetryAfter);
        rethrow;
      }
      await _endAfterFailedRefresh(current);
      throw const SignedOutException();
    } on ApiException {
      await _endAfterFailedRefresh(current);
      throw const SignedOutException();
    }

    if (_disposed || !identical(_session, current)) {
      throw const SignedOutException();
    }
    final next = _Session.issued(tokens, sentWall, sentMono, ++_generation);
    _session = next;
    _refreshNotBefore = null;
    try {
      await _store.write(next.toStored());
    } on Exception {
      // Keep working on the new tokens in memory, but don't leave the
      // rotated-out refresh token on disk: presenting it after a restart
      // would be reuse and revoke this session (014).
      await _clearStore();
    }
    return next;
  }

  Future<void> _endAfterFailedRefresh(_Session failed) async {
    if (!identical(_session, failed)) return;
    _session = null;
    _generation++;
    _refreshNotBefore = null;
    await _clearStore();
    _setStatus(SessionStatus.signedOut);
  }

  /// Deletes the stored session; false if storage failed.
  Future<bool> _clearStore() async {
    try {
      await _store.clear();
      return true;
    } on Exception {
      return false;
    }
  }

  void _setStatus(SessionStatus status) {
    if (status == _status) return;
    _status = status;
    if (!_disposed) notifyListeners();
  }

  void _checkNotDisposed() {
    if (_disposed) throw StateError('SessionManager was disposed');
  }
}

/// One generation of tokens and the deadlines of its access token.
final class _Session {
  _Session._({
    required this.accessToken,
    required this.refreshToken,
    required this.generation,
    required this.monoDeadline,
    required this.wallDeadline,
    required this.lifetime,
  });

  /// Tokens just issued; both deadlines count from when the request was
  /// sent, which errs on the early side.
  factory _Session.issued(
    AuthTokens tokens,
    DateTime sentWall,
    Duration sentMono,
    int generation,
  ) => _Session._(
    accessToken: tokens.accessToken,
    refreshToken: tokens.refreshToken,
    generation: generation,
    monoDeadline: sentMono + tokens.expiresIn,
    wallDeadline: sentWall.add(tokens.expiresIn),
    lifetime: tokens.expiresIn,
  );

  /// A session from storage. Only the wall clock spans a restart: if it
  /// says the token expired, or that more than its whole lifetime remains
  /// (the clock went backwards), the token is treated as stale.
  factory _Session.restored(
    StoredSession stored,
    int generation,
    AuthClock clock,
  ) {
    final now = clock.now();
    final mono = clock.elapsed();
    final remaining = stored.accessExpiresAt.difference(now);
    final plausible =
        remaining > Duration.zero && remaining <= stored.accessLifetime;
    return _Session._(
      accessToken: stored.accessToken,
      refreshToken: stored.refreshToken,
      generation: generation,
      monoDeadline: plausible ? mono + remaining : mono,
      wallDeadline: plausible ? stored.accessExpiresAt : now,
      lifetime: stored.accessLifetime,
    );
  }

  final String accessToken;
  final String refreshToken;
  final int generation;
  final Duration monoDeadline;
  final DateTime wallDeadline;
  final Duration lifetime;

  /// Set when the server refused this access token although the clocks say
  /// it is valid: the next call refreshes first.
  bool forceStale = false;

  StoredSession toStored() => StoredSession(
    accessToken: accessToken,
    refreshToken: refreshToken,
    accessExpiresAt: wallDeadline,
    accessLifetime: lifetime,
  );

  @override
  String toString() => '_Session(<redacted>)';
}

import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:vocatogether/api/account_api.dart';
import 'package:vocatogether/api/api_client.dart';
import 'package:vocatogether/api/auth_api.dart';
import 'package:vocatogether/auth/auth_clock.dart';
import 'package:vocatogether/auth/auth_tokens.dart';
import 'package:vocatogether/auth/token_store.dart';
import 'package:vocatogether/session.dart';

/// A well-formed access token whose body starts with [marker], so tests can
/// search for it in requests, strings and logs.
String accessToken(String marker) => _token('vt_at_', 'AT$marker');

/// A well-formed refresh token whose body starts with [marker].
String refreshToken(String marker) => _token('vt_rt_', 'RT$marker');

String _token(String prefix, String marker) {
  assert(RegExp(r'^[A-Za-z0-9_-]{1,40}$').hasMatch(marker));
  // 42 free characters, then one whose low two bits are zero (strict
  // base64url of 32 bytes).
  return '$prefix${marker.padRight(42, 'x')}A';
}

/// A login/refresh/google 200 body.
Map<String, Object?> tokenBody(
  String access,
  String refresh, {
  int expiresIn = 900,
}) => {
  'access_token': access,
  'token_type': 'Bearer',
  'expires_in': expiresIn,
  'refresh_token': refresh,
};

http.Response jsonResponse(
  int status,
  Object? body, {
  Map<String, String> headers = const {},
}) => http.Response(
  jsonEncode(body),
  status,
  headers: {'content-type': 'application/json', ...headers},
);

http.Response errorResponse(
  int status,
  String code, {
  Map<String, String> headers = const {},
}) => jsonResponse(status, {
  'error': {'code': code},
}, headers: headers);

http.Response noContent() => http.Response('', 204);

http.Response healthy() => jsonResponse(200, {'status': 'ok'});

typedef Responder = FutureOr<http.Response> Function(http.Request request);

/// A scripted backend behind a [MockClient]: every request is recorded and
/// answered by the next responder queued for its method and path, or by a
/// standing responder. An unscripted request fails the test.
class FakeServer {
  FakeServer() {
    client = MockClient(_handle);
  }

  late final MockClient client;
  final requests = <http.Request>[];
  final _queued = <String, List<Responder>>{};
  final _standing = <String, Responder>{};

  /// Answers the next `method path` request with [responder].
  void once(String method, String path, Responder responder) =>
      _queued.putIfAbsent('$method $path', () => []).add(responder);

  /// Answers every `method path` request without a queued responder.
  void always(String method, String path, Responder responder) =>
      _standing['$method $path'] = responder;

  /// The recorded requests to [path].
  List<http.Request> to(String path) =>
      requests.where((r) => r.url.path == path).toList();

  int count(String path) => to(path).length;

  Future<http.Response> _handle(http.Request request) async {
    requests.add(request);
    final key = '${request.method} ${request.url.path}';
    final queue = _queued[key];
    final responder = (queue != null && queue.isNotEmpty)
        ? queue.removeAt(0)
        : _standing[key];
    if (responder == null) {
      throw StateError('unscripted request: $key');
    }
    return responder(request);
  }
}

final testBaseUrl = Uri.parse('https://api.test.example');

ApiClient apiClientFor(http.Client client) =>
    ApiClient(baseUrl: testBaseUrl, httpClient: client, requireHttps: true);

AuthApi authApiFor(http.Client client) => AuthApi(apiClientFor(client));

AccountApi accountApiFor(http.Client client) =>
    AccountApi(apiClientFor(client));

/// A clock tests move by hand. [advance] moves both clocks; the wall clock
/// can also jump on its own, like a user changing the device time.
class FakeAuthClock extends AuthClock {
  FakeAuthClock({DateTime? wall})
    : wall = wall ?? DateTime.utc(2026, 10, 2, 12);

  DateTime wall;
  Duration monotonic = Duration.zero;

  void advance(Duration d) {
    wall = wall.add(d);
    monotonic += d;
  }

  @override
  DateTime now() => wall;

  @override
  Duration elapsed() => monotonic;
}

/// A deterministic in-memory [TokenStore] that holds the same encoded value
/// [SecureTokenStore] would, so corrupt data can be planted, and can be made
/// to fail or hang.
class InMemoryTokenStore implements TokenStore {
  InMemoryTokenStore({this.raw});

  /// The stored value, exactly as the secure store would hold it.
  String? raw;

  Exception? readError;
  Exception? writeError;

  /// How many of the next [clear] calls fail.
  int failingClears = 0;

  /// When set, [read] waits for it.
  Completer<void>? readGate;

  /// When set, [write] waits for it before storing.
  Completer<void>? writeGate;

  int reads = 0;
  int writes = 0;
  int clears = 0;

  StoredSession? get session =>
      raw == null ? null : StoredSessionCodec.decode(raw!);

  @override
  Future<StoredSession?> read() async {
    reads++;
    await readGate?.future;
    if (readError case final e?) throw e;
    final value = raw;
    return value == null ? null : StoredSessionCodec.decode(value);
  }

  @override
  Future<void> write(StoredSession session) async {
    writes++;
    await writeGate?.future;
    if (writeError case final e?) throw e;
    raw = StoredSessionCodec.encode(session);
  }

  @override
  Future<void> clear() async {
    clears++;
    if (failingClears > 0) {
      failingClears--;
      throw const TokenStoreException('clear');
    }
    raw = null;
  }
}

/// A stored session for [marker]'s tokens whose access token expires
/// [remaining] after [clock]'s wall time.
String storedRaw(
  FakeAuthClock clock,
  String marker, {
  Duration remaining = const Duration(minutes: 10),
  Duration lifetime = const Duration(minutes: 15),
}) => StoredSessionCodec.encode(
  StoredSession(
    accessToken: accessToken(marker),
    refreshToken: refreshToken(marker),
    accessExpiresAt: clock.now().add(remaining),
    accessLifetime: lifetime,
  ),
);

/// Lets every pending microtask and zero-delay timer run.
Future<void> settle() async {
  for (var i = 0; i < 20; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

/// A signed-in manager over [server] and [store], restored from storage.
Future<SessionManager> signedInManager(
  FakeServer server,
  InMemoryTokenStore store,
  FakeAuthClock clock, {
  String marker = '1',
  Duration remaining = const Duration(minutes: 10),
}) async {
  store.raw ??= storedRaw(clock, marker, remaining: remaining);
  final manager = SessionManager(
    store: store,
    authApi: authApiFor(server.client),
    clock: clock,
  );
  await manager.restore();
  assert(manager.status == SessionStatus.signedIn);
  return manager;
}

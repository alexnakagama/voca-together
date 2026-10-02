import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import 'auth_tokens.dart';

/// The only persistent location of the app's tokens.
///
/// Both tokens and the access expiry are one [StoredSession], read and
/// written as a unit. Implementations know nothing about HTTP or sessions.
abstract interface class TokenStore {
  /// The stored session, or null if there is none.
  ///
  /// Throws [TokenStoreCorruptException] if what is stored can't be a
  /// session, and [TokenStoreException] if storage fails.
  Future<StoredSession?> read();

  /// Replaces the stored session. Throws [TokenStoreException].
  Future<void> write(StoredSession session);

  /// Deletes the stored session. Throws [TokenStoreException].
  Future<void> clear();
}

/// Storage failed. Carries only the operation name, never the platform's
/// message.
class TokenStoreException implements Exception {
  const TokenStoreException(this.operation);

  final String operation;

  @override
  String toString() => 'TokenStoreException($operation)';
}

/// The stored value isn't a valid session.
class TokenStoreCorruptException implements Exception {
  const TokenStoreCorruptException();

  @override
  String toString() => 'TokenStoreCorruptException';
}

/// The persisted form of a [StoredSession]: one JSON object,
/// `{"v":1,"at","rt","exp":<epoch ms, UTC>,"ttl":<seconds>}`.
///
/// Decoding is strict: any other version, key set, type or token shape is
/// [TokenStoreCorruptException], never a partly valid session.
abstract final class StoredSessionCodec {
  static const version = 1;
  static const _keys = {'v', 'at', 'rt', 'exp', 'ttl'};

  /// Latest representable instant (year 275760); past it is corrupt.
  static const _maxEpochMs = 8640000000000000;

  static String encode(StoredSession s) => jsonEncode({
    'v': version,
    'at': s.accessToken,
    'rt': s.refreshToken,
    'exp': s.accessExpiresAt.toUtc().millisecondsSinceEpoch,
    'ttl': s.accessLifetime.inSeconds,
  });

  static StoredSession decode(String raw) {
    final Object? json;
    try {
      json = jsonDecode(raw);
    } on FormatException {
      throw const TokenStoreCorruptException();
    }
    if (json
        case {
          'v': version,
          'at': final String at,
          'rt': final String rt,
          'exp': final int exp,
          'ttl': final int ttl,
        }
        when json.length == _keys.length &&
            isAccessToken(at) &&
            isRefreshToken(rt) &&
            exp > 0 &&
            exp <= _maxEpochMs &&
            ttl >= 1 &&
            ttl <= maxAccessLifetime.inSeconds) {
      return StoredSession(
        accessToken: at,
        refreshToken: rt,
        accessExpiresAt: DateTime.fromMillisecondsSinceEpoch(exp, isUtc: true),
        accessLifetime: Duration(seconds: ttl),
      );
    }
    throw const TokenStoreCorruptException();
  }
}

/// [TokenStore] on `flutter_secure_storage` (Android: AES-GCM data key
/// wrapped by an RSA key in the Android Keystore).
///
/// The whole session is one value under one key, so it is written with a
/// single plugin call: the tokens can't be split. On Android the plugin
/// persists with `SharedPreferences.apply()`: the write is atomic but reaches
/// disk asynchronously, so a process killed right after it may keep the
/// previous value (docs/decisions.md 023).
class SecureTokenStore implements TokenStore {
  SecureTokenStore(this._storage);

  final FlutterSecureStorage _storage;

  static const key = 'vt_session_v1';

  /// Spelled out instead of relying on the plugin's defaults (checked in
  /// flutter_secure_storage 11.2.0), so a default changing in an update
  /// can't silently change how tokens are protected.
  ///
  /// [AndroidOptions.resetOnError] wipes data the plugin can't decrypt (e.g.
  /// after a restore onto another device's Keystore), which reads as signed
  /// out. No biometrics: the app is unlocked by the device lock.
  static const androidOptions = AndroidOptions(
    resetOnError: true,
    migrateOnAlgorithmChange: true,
    migrateWithBackup: false,
    enforceBiometrics: false,
    requireBiometricsPerOperation: false,
    keyCipherAlgorithm:
        KeyCipherAlgorithm.RSA_ECB_OAEPwithSHA_256andMGF1Padding,
    storageCipherAlgorithm: StorageCipherAlgorithm.AES_GCM_NoPadding,
  );

  @override
  Future<StoredSession?> read() async {
    final String? raw;
    try {
      raw = await _storage.read(key: key, aOptions: androidOptions);
    } on Exception {
      throw const TokenStoreException('read');
    }
    return raw == null ? null : StoredSessionCodec.decode(raw);
  }

  @override
  Future<void> write(StoredSession session) async {
    final raw = StoredSessionCodec.encode(session);
    try {
      await _storage.write(key: key, value: raw, aOptions: androidOptions);
    } on Exception {
      throw const TokenStoreException('write');
    }
  }

  @override
  Future<void> clear() async {
    try {
      await _storage.delete(key: key, aOptions: androidOptions);
    } on Exception {
      throw const TokenStoreException('clear');
    }
  }
}

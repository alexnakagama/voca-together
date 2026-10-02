import 'dart:convert';

import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/auth/auth_tokens.dart';
import 'package:vocatogether/auth/token_store.dart';

import '../support/fakes.dart';

/// Records what [SecureTokenStore] asks of the plugin.
class _FakeSecureStorage implements FlutterSecureStorage {
  final values = <String, String>{};
  final optionsSeen = <AndroidOptions?>[];
  Exception? error;

  @override
  Future<String?> read({
    required String key,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    optionsSeen.add(aOptions);
    if (error case final e?) throw e;
    return values[key];
  }

  @override
  Future<void> write({
    required String key,
    required String? value,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    optionsSeen.add(aOptions);
    if (error case final e?) throw e;
    values[key] = value!;
  }

  @override
  Future<void> delete({
    required String key,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    optionsSeen.add(aOptions);
    if (error case final e?) throw e;
    values.remove(key);
  }

  @override
  dynamic noSuchMethod(Invocation invocation) =>
      throw UnsupportedError('SecureTokenStore must not call this');
}

StoredSession _session() => StoredSession(
  accessToken: accessToken('1'),
  refreshToken: refreshToken('1'),
  accessExpiresAt: DateTime.utc(2026, 10, 2, 12, 15),
  accessLifetime: const Duration(minutes: 15),
);

void main() {
  group('StoredSessionCodec', () {
    test('round-trips a session', () {
      final s = StoredSessionCodec.decode(
        StoredSessionCodec.encode(_session()),
      );
      expect(s.accessToken, accessToken('1'));
      expect(s.refreshToken, refreshToken('1'));
      expect(s.accessExpiresAt, DateTime.utc(2026, 10, 2, 12, 15));
      expect(s.accessExpiresAt.isUtc, isTrue);
      expect(s.accessLifetime, const Duration(minutes: 15));
    });

    test('encodes exactly one versioned object', () {
      expect(jsonDecode(StoredSessionCodec.encode(_session())), {
        'v': 1,
        'at': accessToken('1'),
        'rt': refreshToken('1'),
        'exp': DateTime.utc(2026, 10, 2, 12, 15).millisecondsSinceEpoch,
        'ttl': 900,
      });
    });

    test('rejects anything else as corrupt', () {
      final ok = jsonDecode(
        StoredSessionCodec.encode(_session()),
      ) as Map<String, Object?>;
      final raws = <String>[
        '',
        'null',
        '[]',
        '{',
        'vt_at_x',
        jsonEncode({...ok, 'v': 2}),
        jsonEncode({...ok, 'v': '1'}),
        jsonEncode({...ok}..remove('rt')),
        jsonEncode({...ok, 'extra': 1}),
        jsonEncode({...ok, 'at': ok['rt']}),
        jsonEncode({...ok, 'rt': ok['at']}),
        jsonEncode({...ok, 'at': 'vt_at_short'}),
        jsonEncode({...ok, 'exp': 0}),
        jsonEncode({...ok, 'exp': -1}),
        jsonEncode({...ok, 'exp': 1.5}),
        jsonEncode({...ok, 'exp': 9000000000000000}),
        jsonEncode({...ok, 'ttl': 0}),
        jsonEncode({...ok, 'ttl': 86401}),
        jsonEncode({...ok, 'ttl': '900'}),
      ];
      for (final raw in raws) {
        expect(
          () => StoredSessionCodec.decode(raw),
          throwsA(isA<TokenStoreCorruptException>()),
          reason: raw,
        );
      }
    });
  });

  group('SecureTokenStore', () {
    late _FakeSecureStorage storage;
    late SecureTokenStore store;

    setUp(() {
      storage = _FakeSecureStorage();
      store = SecureTokenStore(storage);
    });

    test('an empty store reads null', () async {
      expect(await store.read(), isNull);
    });

    test('writes the whole session as one value under one key', () async {
      await store.write(_session());
      expect(storage.values.keys, [SecureTokenStore.key]);
      expect(
        storage.values[SecureTokenStore.key],
        StoredSessionCodec.encode(_session()),
      );
      final read = await store.read();
      expect(read!.refreshToken, refreshToken('1'));
    });

    test('clear deletes the key', () async {
      await store.write(_session());
      await store.clear();
      expect(storage.values, isEmpty);
      expect(await store.read(), isNull);
    });

    test('corrupt data reads as corrupt', () async {
      storage.values[SecureTokenStore.key] = '{"v":1}';
      await expectLater(
        store.read(),
        throwsA(isA<TokenStoreCorruptException>()),
      );
    });

    test('passes explicit Android options on every call', () async {
      await store.write(_session());
      await store.read();
      await store.clear();
      expect(storage.optionsSeen, hasLength(3));
      for (final o in storage.optionsSeen) {
        expect(o, same(SecureTokenStore.androidOptions));
      }
      expect(
        SecureTokenStore.androidOptions.toMap(),
        containsPair('resetOnError', 'true'),
      );
      expect(
        SecureTokenStore.androidOptions.toMap(),
        allOf([
          containsPair('migrateOnAlgorithmChange', 'true'),
          containsPair('migrateWithBackup', 'false'),
          containsPair('enforceBiometrics', 'false'),
          containsPair('requireBiometricsPerOperation', 'false'),
          containsPair(
            'keyCipherAlgorithm',
            'RSA_ECB_OAEPwithSHA_256andMGF1Padding',
          ),
          containsPair('storageCipherAlgorithm', 'AES_GCM_NoPadding'),
          containsPair('storageNamespace', ''),
        ]),
      );
    });

    test('wraps platform errors without their message', () async {
      storage.error = PlatformException(
        code: 'Exception',
        message: 'cannot decrypt ${refreshToken('LEAK')}',
      );
      for (final op in <Future<Object?> Function()>[
        store.read,
        () => store.write(_session()),
        store.clear,
      ]) {
        await expectLater(
          op(),
          throwsA(
            isA<TokenStoreException>().having(
              (e) => e.toString(),
              'toString',
              isNot(contains('LEAK')),
            ),
          ),
        );
      }
    });
  });
}

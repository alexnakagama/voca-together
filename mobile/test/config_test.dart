import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/config.dart';

void main() {
  group('parseApiBaseUrl', () {
    test('accepts the dev emulator origin in debug builds', () {
      final uri = parseApiBaseUrl('http://10.0.2.2:8080', requireHttps: false);
      expect(uri, Uri.parse('http://10.0.2.2:8080'));
    });

    test('accepts an https origin in release builds', () {
      final uri = parseApiBaseUrl(
        'https://api.example.com',
        requireHttps: true,
      );
      expect(uri.host, 'api.example.com');
    });

    test('rejects http in release builds', () {
      expect(
        () => parseApiBaseUrl('http://api.example.com', requireHttps: true),
        throwsA(isA<ConfigException>()),
      );
    });

    const invalid = {
      'empty': '',
      'leading whitespace': ' https://api.example.com',
      'trailing newline': 'https://api.example.com\n',
      'not absolute': 'api.example.com',
      'other scheme': 'ftp://api.example.com',
      'no host': 'https://',
      'credentials': 'https://user:pass@api.example.com',
      'trailing slash': 'https://api.example.com/',
      'path': 'https://api.example.com/v1',
      'query': 'https://api.example.com?x=1',
      'fragment': 'https://api.example.com#x',
    };
    for (final MapEntry(key: name, value: raw) in invalid.entries) {
      test('rejects $name', () {
        expect(
          () => parseApiBaseUrl(raw, requireHttps: false),
          throwsA(isA<ConfigException>()),
        );
      });
    }

    test('error messages never echo the value', () {
      const secretish = 'https://user:hunter2@api.example.com';
      try {
        parseApiBaseUrl(secretish, requireHttps: false);
        fail('expected ConfigException');
      } on ConfigException catch (e) {
        expect(e.toString(), isNot(contains('hunter2')));
        expect(e.toString(), contains('API_BASE_URL'));
      }
    });
  });

  group('parseGoogleServerClientId', () {
    const valid = '1234567890-abc123def.apps.googleusercontent.com';

    test('accepts a client ID in any build', () {
      expect(parseGoogleServerClientId(valid, required: true), valid);
      expect(parseGoogleServerClientId(valid, required: false), valid);
    });

    test('a debug build may leave it out: Google sign-in is off', () {
      expect(parseGoogleServerClientId('', required: false), isNull);
    });

    test('a release build refuses to start without it', () {
      expect(
        () => parseGoogleServerClientId('', required: true),
        throwsA(isA<ConfigException>()),
      );
    });

    final invalid = {
      'only the suffix': '.apps.googleusercontent.com',
      'another suffix': '1234-abc.apps.googleusercontent.com.evil.example',
      'no suffix': '1234-abc',
      'a client secret': 'GOCSPX-abcdefghijklmnopqrstuvwxyz12',
      'leading whitespace': ' $valid',
      'trailing newline': '$valid\n',
      'a space inside': '1234 abc.apps.googleusercontent.com',
      'a control character': '1234\tabc.apps.googleusercontent.com',
      'non-ASCII': '1234-ñ.apps.googleusercontent.com',
      'too long': '${'1' * 229}.apps.googleusercontent.com',
      'a quoted value': '"$valid"',
    };
    for (final MapEntry(key: name, value: raw) in invalid.entries) {
      for (final required in [true, false]) {
        test('rejects $name (required: $required)', () {
          expect(
            () => parseGoogleServerClientId(raw, required: required),
            throwsA(isA<ConfigException>()),
          );
        });
      }
    }

    test('accepts the longest allowed value', () {
      final longest = '${'1' * 228}.apps.googleusercontent.com';
      expect(longest.length, 255);
      expect(parseGoogleServerClientId(longest, required: true), longest);
    });

    test('error messages name the variable and never echo the value', () {
      for (final raw in ['', 'GOCSPX-hunter2secret']) {
        try {
          parseGoogleServerClientId(raw, required: true);
          fail('expected ConfigException');
        } on ConfigException catch (e) {
          expect(e.toString(), isNot(contains('hunter2')));
          expect(e.toString(), contains('GOOGLE_SERVER_CLIENT_ID'));
        }
      }
    });
  });
}

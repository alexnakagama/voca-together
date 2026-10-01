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
}

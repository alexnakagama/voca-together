import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/api/api_exception.dart';

void main() {
  group('ApiHttpException.fromBody', () {
    test('reads code and fields of a validation error', () {
      final e = ApiHttpException.fromBody(422, {
        'error': {
          'code': 'validation_failed',
          'fields': [
            {'field': 'email', 'code': 'invalid'},
            {'field': 'password', 'code': 'too_short'},
          ],
        },
      });
      expect(e.statusCode, 422);
      expect(e.code, 'validation_failed');
      expect(e.fields.map((f) => (f.field, f.code)), [
        ('email', 'invalid'),
        ('password', 'too_short'),
      ]);
      expect(e.retryAfter, isNull);
    });

    test('keeps the status when the body is unusable', () {
      final bodies = <Object?>[
        null,
        '',
        'not found',
        42,
        <Object?>[],
        <String, Object?>{},
        {'error': 'internal_error'},
        {'error': <String, Object?>{}},
        {
          'error': {'code': 7},
        },
        {
          'error': {'code': null},
        },
      ];
      for (final body in bodies) {
        final e = ApiHttpException.fromBody(500, body);
        expect(e.statusCode, 500, reason: '$body');
        expect(e.code, isNull, reason: '$body');
        expect(e.fields, isEmpty, reason: '$body');
      }
    });

    test('drops codes that are not plain identifiers', () {
      for (final code in [
        '',
        'Internal_Error',
        '1abc',
        'has space',
        'line\nbreak',
        'é',
        'x' * 65,
        'a' * 100000,
      ]) {
        final e = ApiHttpException.fromBody(400, {
          'error': {'code': code},
        });
        expect(e.code, isNull, reason: code.length > 20 ? 'long' : code);
      }
      final ok = ApiHttpException.fromBody(400, {
        'error': {'code': 'a${'b' * 63}'},
      });
      expect(ok.code, hasLength(64));
    });

    test('drops malformed field entries and keeps the valid ones', () {
      final e = ApiHttpException.fromBody(422, {
        'error': {
          'code': 'validation_failed',
          'fields': [
            {'field': 'email', 'code': 'invalid'},
            {'field': 'Email', 'code': 'invalid'},
            {'field': 'email'},
            {'field': 'email', 'code': 3},
            'email',
            null,
            {'field': 'id_token', 'code': 'required'},
          ],
        },
      });
      expect(e.fields.map((f) => f.field), ['email', 'id_token']);
    });

    test('ignores fields that are not a list', () {
      final e = ApiHttpException.fromBody(422, {
        'error': {
          'code': 'validation_failed',
          'fields': {'field': 'email', 'code': 'invalid'},
        },
      });
      expect(e.code, 'validation_failed');
      expect(e.fields, isEmpty);
    });

    test('fields are unmodifiable', () {
      final e = ApiHttpException.fromBody(422, {
        'error': {
          'code': 'validation_failed',
          'fields': [
            {'field': 'email', 'code': 'invalid'},
          ],
        },
      });
      expect(() => e.fields.clear(), throwsUnsupportedError);
    });
  });

  group('parseRetryAfter', () {
    const cases = <String?, Duration?>{
      null: null,
      '': null,
      '5': Duration(seconds: 5),
      ' 7 ': Duration(seconds: 7),
      '0': Duration(seconds: 1),
      '-1': null,
      '+3': null,
      '1.5': null,
      '0x10': null,
      'Wed, 21 Oct 2015 07:28:00 GMT': null,
      '86400': Duration(hours: 24),
      '99999999': Duration(hours: 24),
      '99999999999999999999999': Duration(hours: 24),
    };
    cases.forEach((raw, expected) {
      test('"$raw"', () => expect(parseRetryAfter(raw), expected));
    });
  });

  group('toString', () {
    test('uses fixed formats', () {
      expect(
        ApiHttpException(
          statusCode: 401,
          code: 'invalid_refresh_token',
        ).toString(),
        'ApiHttpException(401, invalid_refresh_token)',
      );
      expect(
        ApiHttpException(statusCode: 404).toString(),
        'ApiHttpException(404)',
      );
      expect(const ApiNetworkException().toString(), 'ApiNetworkException');
      expect(const ApiTimeoutException().toString(), 'ApiTimeoutException');
      expect(
        const ApiProtocolException(
          ProtocolFailure.redirect,
          statusCode: 302,
        ).toString(),
        'ApiProtocolException(redirect, 302)',
      );
      expect(
        const ApiProtocolException(ProtocolFailure.malformedBody).toString(),
        'ApiProtocolException(malformedBody)',
      );
    });
  });
}

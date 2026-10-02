import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/auth/auth_tokens.dart';

import '../support/fakes.dart';

void main() {
  group('token shapes', () {
    test('accept well-formed tokens of the right kind only', () {
      final at = accessToken('1');
      final rt = refreshToken('1');
      expect(isAccessToken(at), isTrue);
      expect(isRefreshToken(rt), isTrue);
      expect(isAccessToken(rt), isFalse);
      expect(isRefreshToken(at), isFalse);
    });

    test('reject everything else', () {
      final body = 'A' * 43;
      for (final bad in [
        '',
        'vt_at_',
        'vt_at_${'A' * 42}',
        'vt_at_${'A' * 44}',
        'vt_at_${'A' * 42}=',
        'vt_at_${'A' * 42}B', // non-zero trailing bits (not strict base64url)
        'vt_at_${'A' * 41}+A',
        'vt_at_${'A' * 41}/A',
        ' vt_at_$body',
        'vt_at_$body ',
        'VT_AT_$body',
        'Bearer vt_at_$body',
        'vt_xx_$body',
        body,
      ]) {
        expect(isAccessToken(bad), isFalse, reason: bad);
      }
      for (final last in 'AEIMQUYcgkosw048'.split('')) {
        expect(isAccessToken('vt_at_${'A' * 42}$last'), isTrue, reason: last);
      }
    });
  });

  group('AuthTokens.fromJson', () {
    test('parses a token response', () {
      final t = AuthTokens.fromJson(
        tokenBody(accessToken('1'), refreshToken('1'), expiresIn: 60),
      );
      expect(t.accessToken, accessToken('1'));
      expect(t.refreshToken, refreshToken('1'));
      expect(t.expiresIn, const Duration(seconds: 60));
    });

    test('ignores unknown fields', () {
      final t = AuthTokens.fromJson({
        ...tokenBody(accessToken('1'), refreshToken('1')),
        'scope': 'x',
      });
      expect(t.expiresIn, const Duration(seconds: 900));
    });

    test('rejects malformed responses', () {
      final ok = tokenBody(accessToken('1'), refreshToken('1'));
      final bodies = <Object?>[
        null,
        'x',
        <Object?>[],
        {...ok}..remove('access_token'),
        {...ok}..remove('refresh_token'),
        {...ok}..remove('token_type'),
        {...ok}..remove('expires_in'),
        {...ok, 'token_type': 'bearer'},
        {...ok, 'token_type': 'MAC'},
        {...ok, 'expires_in': 0},
        {...ok, 'expires_in': -5},
        {...ok, 'expires_in': 86401},
        {...ok, 'expires_in': 900.0},
        {...ok, 'expires_in': '900'},
        {...ok, 'access_token': refreshToken('1')},
        {...ok, 'refresh_token': accessToken('1')},
        {...ok, 'access_token': 'vt_at_short'},
        {...ok, 'access_token': 1},
      ];
      for (final body in bodies) {
        expect(
          () => AuthTokens.fromJson(body),
          throwsA(
            isA<ApiProtocolException>().having(
              (e) => e.failure,
              'failure',
              ProtocolFailure.malformedBody,
            ),
          ),
          reason: '$body',
        );
      }
    });
  });

  test('toString never shows a token', () {
    final t = AuthTokens.fromJson(
      tokenBody(accessToken('SECRET'), refreshToken('SECRET')),
    );
    final s = StoredSession(
      accessToken: accessToken('SECRET'),
      refreshToken: refreshToken('SECRET'),
      accessExpiresAt: DateTime.utc(2026),
      accessLifetime: const Duration(minutes: 15),
    );
    for (final text in [
      '$t',
      '$s',
      '${[t, s]}',
    ]) {
      expect(text, isNot(contains('SECRET')));
      expect(text, contains('<redacted>'));
    }
  });
}

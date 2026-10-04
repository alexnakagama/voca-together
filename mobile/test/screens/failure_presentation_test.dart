import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/auth/google_identity_exception.dart';
import 'package:vocatogether/auth/token_store.dart';
import 'package:vocatogether/screens/failure_presentation.dart';
import 'package:vocatogether/session.dart';

import 'harness.dart';

ApiHttpException _http(
  int status, [
  String? code,
  List<FieldError> fields = const [],
  Duration? retryAfter,
]) => ApiHttpException(
  statusCode: status,
  code: code,
  fields: fields,
  retryAfter: retryAfter,
);

FailurePresentation _present(Object error) => presentFailure(error, l10n);

void main() {
  group('every backend error code', () {
    // code → (status it comes with, kind, banner).
    final cases = <String, (int, FailureKind, String?)>{
      'invalid_request': (400, FailureKind.unexpected, l10n.errorUnexpected),
      'validation_failed': (
        422,
        FailureKind.invalidInput,
        l10n.errorCheckInput,
      ),
      'internal_error': (500, FailureKind.unexpected, l10n.errorUnexpected),
      'invalid_credentials': (
        401,
        FailureKind.invalidCredentials,
        l10n.errorInvalidCredentials,
      ),
      'email_not_verified': (
        403,
        FailureKind.emailNotVerified,
        l10n.errorEmailNotVerified,
      ),
      'invalid_refresh_token': (
        401,
        FailureKind.sessionInvalid,
        l10n.errorSessionInvalid,
      ),
      'invalid_access_token': (
        401,
        FailureKind.sessionInvalid,
        l10n.errorSessionInvalid,
      ),
      'invalid_google_token': (
        401,
        FailureKind.googleRejected,
        l10n.errorGoogleRejected,
      ),
      'google_email_unusable': (
        403,
        FailureKind.googleEmailUnusable,
        l10n.errorGoogleEmailUnusable,
      ),
      'account_exists': (
        409,
        FailureKind.accountExists,
        l10n.errorAccountExists,
      ),
      'rate_limited': (
        429,
        FailureKind.rateLimited,
        l10n.errorRateLimitedNoWait,
      ),
      'service_unavailable': (
        503,
        FailureKind.unavailable,
        l10n.errorUnavailableNoWait,
      ),
    };

    cases.forEach((code, expected) {
      final (status, kind, message) = expected;
      test(code, () {
        final p = _present(_http(status, code));
        expect(p.kind, kind);
        expect(p.message, message);
      });
    });
  });

  group('validation_failed fields', () {
    final cases = <(String, String), (String?, String?)>{
      ('email', 'invalid'): (l10n.errorEmailInvalid, null),
      ('password', 'required'): (null, l10n.passwordRequired),
      ('password', 'too_short'): (null, l10n.errorPasswordTooShort),
      ('password', 'too_long'): (null, l10n.errorPasswordTooLong),
      ('password', 'too_common'): (null, l10n.errorPasswordTooCommon),
      ('password', 'same_as_email'): (null, l10n.errorPasswordSameAsEmail),
    };
    cases.forEach((input, expected) {
      test('${input.$1}:${input.$2} goes on its field, with no banner', () {
        final p = _present(
          _http(422, 'validation_failed', [FieldError(input.$1, input.$2)]),
        );
        expect(p.kind, FailureKind.invalidInput);
        expect(p.message, isNull);
        expect(p.emailError, expected.$1);
        expect(p.passwordError, expected.$2);
      });
    });

    test('both fields at once', () {
      final p = _present(
        _http(422, 'validation_failed', const [
          FieldError('email', 'invalid'),
          FieldError('password', 'required'),
        ]),
      );
      expect(p.emailError, l10n.errorEmailInvalid);
      expect(p.passwordError, l10n.passwordRequired);
      expect(p.message, isNull);
    });

    test('the first error per field wins', () {
      final p = _present(
        _http(422, 'validation_failed', const [
          FieldError('password', 'too_short'),
          FieldError('password', 'too_common'),
        ]),
      );
      expect(p.passwordError, l10n.errorPasswordTooShort);
    });

    test('an unknown field or code adds the generic banner', () {
      for (final f in const [
        FieldError('token', 'invalid'),
        FieldError('email', 'too_long'),
        FieldError('password', 'needs_emoji'),
      ]) {
        final p = _present(
          _http(422, 'validation_failed', [
            f,
            const FieldError('email', 'invalid'),
          ]),
        );
        expect(p.message, l10n.errorCheckInput, reason: '$f');
        expect(p.emailError, l10n.errorEmailInvalid, reason: '$f');
      }
    });

    test('a code-less 422 is invalid input with the generic banner', () {
      final p = _present(_http(422));
      expect(p.kind, FailureKind.invalidInput);
      expect(p.message, l10n.errorCheckInput);
    });
  });

  group('status fallback for missing or unknown codes', () {
    final cases = <int, (FailureKind, String)>{
      400: (FailureKind.unexpected, l10n.errorUnexpected),
      401: (FailureKind.unexpected, l10n.errorUnexpected),
      403: (FailureKind.unexpected, l10n.errorUnexpected),
      404: (FailureKind.unexpected, l10n.errorUnexpected),
      429: (FailureKind.rateLimited, l10n.errorRateLimitedNoWait),
      500: (FailureKind.unexpected, l10n.errorUnexpected),
      502: (FailureKind.unexpected, l10n.errorUnexpected),
      503: (FailureKind.unavailable, l10n.errorUnavailableNoWait),
      504: (FailureKind.unexpected, l10n.errorUnexpected),
    };
    cases.forEach((status, expected) {
      for (final code in [null, 'brand_new_code']) {
        test('$status with code $code', () {
          final p = _present(_http(status, code));
          expect(p.kind, expected.$1);
          expect(p.message, expected.$2);
        });
      }
    });
  });

  group('Retry-After', () {
    test('429 and 503 say how long to wait', () {
      expect(
        _present(
          _http(429, 'rate_limited', const [], const Duration(seconds: 90)),
        ).message,
        'Too many attempts. Try again in 2 minutes.',
      );
      expect(
        _present(
          _http(
            503,
            'service_unavailable',
            const [],
            const Duration(seconds: 5),
          ),
        ).message,
        'VocaTogether is busy right now. Try again in 5 seconds.',
      );
    });

    test('waits are rounded up into one unit', () {
      final cases = <int, String>{
        1: '1 second',
        2: '2 seconds',
        59: '59 seconds',
        60: '1 minute',
        61: '2 minutes',
        3599: '60 minutes',
        3600: '1 hour',
        3601: '2 hours',
        86400: '24 hours',
      };
      cases.forEach((seconds, text) {
        expect(
          formatWait(Duration(seconds: seconds), l10n),
          text,
          reason: '$seconds s',
        );
      });
      expect(formatWait(const Duration(milliseconds: 1500), l10n), '2 seconds');
      expect(formatWait(Duration.zero, l10n), '1 second');
    });
  });

  group('other failures', () {
    test('transport', () {
      expect(_present(const ApiNetworkException()).kind, FailureKind.network);
      expect(_present(const ApiNetworkException()).message, l10n.errorNetwork);
      expect(_present(const ApiTimeoutException()).kind, FailureKind.timeout);
      expect(_present(const ApiTimeoutException()).message, l10n.errorTimeout);
    });

    test('every protocol failure is unexpected', () {
      for (final failure in ProtocolFailure.values) {
        final p = _present(ApiProtocolException(failure, statusCode: 200));
        expect(p.kind, FailureKind.unexpected);
        expect(p.message, l10n.errorUnexpected);
      }
    });

    test('an ended session', () {
      expect(
        _present(const SignedOutException()).kind,
        FailureKind.sessionEnded,
      );
    });

    test('local failures and anything else are unexpected', () {
      for (final error in <Object>[
        const TokenStoreException('write'),
        Exception('boom'),
        'a string',
      ]) {
        final p = _present(error);
        expect(p.kind, FailureKind.unexpected);
        expect(p.message, l10n.errorUnexpected);
      }
    });

    test('a hostile code is never shown', () {
      // ApiHttpException drops non-identifier codes; even an identifier the
      // app doesn't know is only a switch key.
      final hostile = ApiHttpException.fromBody(400, {
        'error': {'code': 'Your password is hunter2'},
      });
      final unknown = _http(400, 'show_this_text');
      for (final e in [hostile, unknown]) {
        final p = _present(e);
        expect(p.message, l10n.errorUnexpected);
      }
    });
  });

  group('Google sign-in', () {
    test('a closed account chooser is not an error: nothing is shown', () {
      final p = _present(
        const GoogleIdentityException(GoogleIdentityFailure.cancelled),
      );
      expect(p.kind, FailureKind.cancelled);
      expect(p.message, isNull);
    });

    for (final failure in GoogleIdentityFailure.values) {
      if (failure == GoogleIdentityFailure.cancelled) continue;
      test('${failure.name} says Google sign-in is unavailable', () {
        final p = _present(GoogleIdentityException(failure));
        expect(p.kind, FailureKind.googleUnavailable);
        expect(p.message, l10n.errorGoogleUnavailable);
      });
    }

    test('the account-exists message never points to a password', () {
      // The existing account may have none (020).
      expect(
        l10n.errorAccountExists.toLowerCase(),
        isNot(contains('password')),
      );
    });

    test('a programming error gets the generic message', () {
      for (final error in <Object>[StateError('x'), ArgumentError('x')]) {
        final p = _present(error);
        expect(p.kind, FailureKind.unexpected);
        expect(p.message, l10n.errorUnexpected);
      }
    });
  });
}

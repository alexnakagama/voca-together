import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/auth/google_identity_exception.dart';
import 'package:vocatogether/auth/token_store.dart';
import 'package:vocatogether/media/photo_source.dart';
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

  group('profile', () {
    // (field, code) → (name error, bio error).
    final cases = <(String, String), (String?, String?)>{
      ('display_name', 'required'): (l10n.displayNameRequired, null),
      ('display_name', 'too_long'): (l10n.errorDisplayNameTooLong, null),
      ('display_name', 'invalid'): (l10n.errorDisplayNameInvalid, null),
      ('bio', 'too_long'): (null, l10n.errorBioTooLong),
      ('bio', 'invalid'): (null, l10n.errorBioInvalid),
    };
    cases.forEach((input, expected) {
      test('${input.$1}:${input.$2} goes on its field, with no banner', () {
        final p = _present(
          _http(422, 'validation_failed', [FieldError(input.$1, input.$2)]),
        );
        expect(p.kind, FailureKind.invalidInput);
        expect(p.message, isNull);
        expect(p.displayNameError, expected.$1);
        expect(p.bioError, expected.$2);
        expect(p.emailError, isNull);
        expect(p.passwordError, isNull);
      });
    });

    test('both fields at once', () {
      final p = _present(
        _http(422, 'validation_failed', [
          const FieldError('display_name', 'too_long'),
          const FieldError('bio', 'invalid'),
        ]),
      );
      expect(p.message, isNull);
      expect(p.displayNameError, l10n.errorDisplayNameTooLong);
      expect(p.bioError, l10n.errorBioInvalid);
    });

    test('a code this app doesn\'t know adds the generic banner', () {
      final p = _present(
        _http(422, 'validation_failed', [
          const FieldError('display_name', 'taken'),
        ]),
      );
      expect(p.message, l10n.errorCheckInput);
      expect(p.displayNameError, isNull);
    });

    // The session layer turns it into "no profile"; if it ever reaches a
    // screen it is an ordinary unexpected failure, never server text.
    test('profile_not_found is unexpected', () {
      final p = _present(_http(404, 'profile_not_found'));
      expect(p.kind, FailureKind.unexpected);
      expect(p.message, l10n.errorUnexpected);
    });
  });

  group('languages', () {
    // code → text; the same under either list.
    final codes = <String, String>{
      'too_many': l10n.errorLanguagesTooMany,
      'unknown_language': l10n.errorLanguageUnknown,
      'invalid_level': l10n.errorLanguageLevelInvalid,
      'duplicate': l10n.errorLanguageDuplicate,
    };
    codes.forEach((code, text) {
      test('spoken:$code goes on the spoken list, with no banner', () {
        final p = _present(
          _http(422, 'validation_failed', [FieldError('spoken', code)]),
        );
        expect(p.kind, FailureKind.invalidInput);
        expect(p.message, isNull);
        expect(p.spokenError, text);
        expect(p.learningError, isNull);
      });

      test('learning:$code goes on the learning list, with no banner', () {
        final p = _present(
          _http(422, 'validation_failed', [FieldError('learning', code)]),
        );
        expect(p.kind, FailureKind.invalidInput);
        expect(p.message, isNull);
        expect(p.spokenError, isNull);
        expect(p.learningError, text);
        expect(p.displayNameError, isNull);
        expect(p.bioError, isNull);
      });
    });

    test('both lists at once', () {
      final p = _present(
        _http(422, 'validation_failed', const [
          FieldError('spoken', 'too_many'),
          FieldError('learning', 'duplicate'),
        ]),
      );
      expect(p.message, isNull);
      expect(p.spokenError, l10n.errorLanguagesTooMany);
      expect(p.learningError, l10n.errorLanguageDuplicate);
    });

    // The backend reports a list's errors in a fixed order (029).
    test('the first error per list wins', () {
      final p = _present(
        _http(422, 'validation_failed', const [
          FieldError('spoken', 'unknown_language'),
          FieldError('spoken', 'duplicate'),
        ]),
      );
      expect(p.spokenError, l10n.errorLanguageUnknown);
      expect(p.message, isNull);
    });

    // Nothing is required of either list (029, 030), so `required` is as
    // unknown as any other code.
    test('a code this app doesn\'t know adds the generic banner', () {
      for (final f in const [
        FieldError('spoken', 'required'),
        FieldError('learning', 'too_long'),
        FieldError('languages', 'too_many'),
      ]) {
        final p = _present(_http(422, 'validation_failed', [f]));
        expect(p.message, l10n.errorCheckInput, reason: '$f');
        expect(p.spokenError, isNull, reason: '$f');
        expect(p.learningError, isNull, reason: '$f');
      }
    });

    // Limits live on the server only: the text states none of them.
    test('no message states a number', () {
      for (final text in codes.values) {
        expect(text, isNot(contains(RegExp(r'\d'))));
      }
    });
  });

  group('profile picture', () {
    // code → the text by the picture control.
    final codes = <String, String>{
      'required': l10n.errorAvatarRequired,
      'too_large': l10n.errorAvatarTooLarge,
      'unsupported_type': l10n.errorAvatarUnsupportedType,
      'invalid_image': l10n.errorAvatarInvalidImage,
      'dimensions_too_large': l10n.errorAvatarDimensionsTooLarge,
    };
    codes.forEach((code, text) {
      test('avatar:$code goes by the picture, with no banner', () {
        final p = _present(
          _http(422, 'validation_failed', [FieldError('avatar', code)]),
        );
        expect(p.kind, FailureKind.invalidInput);
        expect(p.message, isNull);
        expect(p.avatarError, text);
        expect(p.displayNameError, isNull);
        expect(p.bioError, isNull);
        expect(p.spokenError, isNull);
        expect(p.learningError, isNull);
      });
    });

    test('each code has its own text', () {
      expect(codes.values.toSet(), hasLength(codes.length));
    });

    test('the first error wins', () {
      final p = _present(
        _http(422, 'validation_failed', [
          const FieldError('avatar', 'too_large'),
          const FieldError('avatar', 'invalid_image'),
        ]),
      );
      expect(p.message, isNull);
      expect(p.avatarError, l10n.errorAvatarTooLarge);
    });

    test('a code this app doesn\'t know adds the generic banner', () {
      final p = _present(
        _http(422, 'validation_failed', [
          const FieldError('avatar', 'too_blurry'),
        ]),
      );
      expect(p.kind, FailureKind.invalidInput);
      expect(p.message, l10n.errorCheckInput);
      expect(p.avatarError, isNull);
    });

    test('a profile or language error is not a picture error', () {
      final p = _present(
        _http(422, 'validation_failed', [
          const FieldError('bio', 'too_long'),
          const FieldError('spoken', 'too_many'),
        ]),
      );
      expect(p.avatarError, isNull);
    });

    // Limits live on the server only: the text states none of them.
    test('no message states a number', () {
      for (final text in codes.values) {
        expect(text, isNot(contains(RegExp(r'\d'))));
      }
    });

    // An upload cut short (031): nothing about the photo, so no picture
    // error, only something to try again.
    test('a 400 invalid_request says nothing about the photo', () {
      final p = _present(_http(400, 'invalid_request'));
      expect(p.kind, FailureKind.unexpected);
      expect(p.message, l10n.errorUnexpected);
      expect(p.avatarError, isNull);
    });

    // The session layer turns it into "no picture"; if it ever reaches a
    // screen it is an ordinary unexpected failure, never server text.
    test('avatar_not_found is unexpected', () {
      final p = _present(_http(404, 'avatar_not_found'));
      expect(p.kind, FailureKind.unexpected);
      expect(p.message, l10n.errorUnexpected);
      expect(p.avatarError, isNull);
    });
  });

  group('reports and blocks', () {
    // (field, code) → the text and where it goes.
    final codes = <(String, String), String>{
      ('reason', 'required'): l10n.errorReportReasonRequired,
      ('reason', 'invalid'): l10n.errorReportReasonInvalid,
      ('details', 'too_long'): l10n.errorReportDetailsTooLong,
      ('details', 'invalid'): l10n.errorReportDetailsInvalid,
      ('blocks', 'too_many'): l10n.errorBlocksTooMany,
    };
    codes.forEach((key, text) {
      final (field, code) = key;
      test('$field:$code goes to its own error, with no banner', () {
        final p = _present(
          _http(422, 'validation_failed', [FieldError(field, code)]),
        );
        expect(p.kind, FailureKind.invalidInput);
        expect(p.message, isNull);
        expect(p.reasonError, field == 'reason' ? text : isNull);
        expect(p.detailsError, field == 'details' ? text : isNull);
        expect(p.blocksError, field == 'blocks' ? text : isNull);
        expect(p.displayNameError, isNull);
        expect(p.bioError, isNull);
        expect(p.avatarError, isNull);
      });
    });

    test('each code of a field has its own text', () {
      expect(
        l10n.errorReportReasonRequired,
        isNot(l10n.errorReportReasonInvalid),
      );
      expect(
        l10n.errorReportDetailsTooLong,
        isNot(l10n.errorReportDetailsInvalid),
      );
    });

    test('both fields of a report are reported together', () {
      final p = _present(
        _http(422, 'validation_failed', [
          const FieldError('reason', 'invalid'),
          const FieldError('details', 'too_long'),
        ]),
      );
      expect(p.message, isNull);
      expect(p.reasonError, l10n.errorReportReasonInvalid);
      expect(p.detailsError, l10n.errorReportDetailsTooLong);
    });

    // The app never sends its own id, so there is no text for it: the
    // generic message, and no field error.
    test('member:self falls to the generic message', () {
      final p = _present(
        _http(422, 'validation_failed', [const FieldError('member', 'self')]),
      );
      expect(p.kind, FailureKind.invalidInput);
      expect(p.message, l10n.errorCheckInput);
      expect(p.reasonError, isNull);
      expect(p.detailsError, isNull);
      expect(p.blocksError, isNull);
    });

    test('a code this app doesn\'t know adds the generic banner', () {
      for (final field in ['reason', 'details', 'blocks']) {
        final p = _present(
          _http(422, 'validation_failed', [FieldError(field, 'too_odd')]),
        );
        expect(p.message, l10n.errorCheckInput, reason: field);
        expect(p.reasonError, isNull);
        expect(p.detailsError, isNull);
        expect(p.blocksError, isNull);
      }
    });

    // A profile's `bio: too_long` is not a report's `details: too_long`.
    test('another feature\'s error is not a report or block error', () {
      final p = _present(
        _http(422, 'validation_failed', [
          const FieldError('bio', 'too_long'),
          const FieldError('spoken', 'too_many'),
        ]),
      );
      expect(p.reasonError, isNull);
      expect(p.detailsError, isNull);
      expect(p.blocksError, isNull);
    });

    // Limits live on the server only: the text states none of them.
    test('no message states a number', () {
      for (final text in codes.values) {
        expect(text, isNot(contains(RegExp(r'\d'))));
      }
    });

    test('the limit message says that unblocking makes room', () {
      expect(l10n.errorBlocksTooMany, contains('Unblock'));
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

    test('a photo the device could not give', () {
      final p = _present(const PhotoSourceException());
      expect(p.kind, FailureKind.photoUnusable);
      expect(p.message, l10n.errorPhotoUnusable);
      // Nothing was sent, so nothing was refused by the server.
      expect(p.avatarError, isNull);
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

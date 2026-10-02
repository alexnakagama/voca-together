import '../api/api_exception.dart';
import '../l10n/app_localizations.dart';
import '../session.dart';

/// What a failed request means for the user. Screens branch on this, never
/// on the backend's error codes.
enum FailureKind {
  /// The server refused the input (422, or a code-less 422 body).
  invalidInput,

  /// Login: unknown email or wrong password, deliberately indistinguishable.
  invalidCredentials,

  /// Login: the password was right but the address isn't verified.
  emailNotVerified,

  /// 429: nothing was done; retrying after the wait is safe (018).
  rateLimited,

  /// 503: overloaded or out of time; the work may or may not have happened.
  unavailable,

  /// The server couldn't be reached.
  network,

  /// No answer in time; the request may or may not have arrived.
  timeout,

  /// A signed-in call was refused although the session still exists.
  sessionInvalid,

  /// The session ended (logout, failed refresh): the router is already
  /// leaving the signed-in screens, so nothing should be shown.
  sessionEnded,

  /// Anything else: 5xx, a broken response, an unknown code, a local failure.
  unexpected,
}

/// What a screen shows for a failure: localized text only, never anything
/// the server sent.
final class FailurePresentation {
  const FailurePresentation(
    this.kind, {
    this.message,
    this.emailError,
    this.passwordError,
  });

  final FailureKind kind;

  /// Form-level text, or null when the field errors say it all.
  final String? message;

  /// Errors for the email and password fields (422 `fields`).
  final String? emailError;
  final String? passwordError;
}

/// The app's one mapping from a failed call to what the user is told
/// (decision 024).
///
/// Server codes are used only as switch keys; unknown codes fall back to the
/// status class, and anything unrecognized gets the generic message.
FailurePresentation presentFailure(Object error, AppLocalizations l10n) {
  return switch (error) {
    SignedOutException() => FailurePresentation(
      FailureKind.sessionEnded,
      message: l10n.errorSessionInvalid,
    ),
    ApiNetworkException() => FailurePresentation(
      FailureKind.network,
      message: l10n.errorNetwork,
    ),
    ApiTimeoutException() => FailurePresentation(
      FailureKind.timeout,
      message: l10n.errorTimeout,
    ),
    ApiHttpException() => _fromHttp(error, l10n),
    // ApiProtocolException, a storage failure while signing in, and anything
    // else the screens can't explain.
    _ => _unexpected(l10n),
  };
}

FailurePresentation _fromHttp(ApiHttpException e, AppLocalizations l10n) {
  switch (e.code) {
    case 'validation_failed':
      return _validation(e, l10n);
    case 'invalid_credentials':
      return FailurePresentation(
        FailureKind.invalidCredentials,
        message: l10n.errorInvalidCredentials,
      );
    case 'email_not_verified':
      return FailurePresentation(
        FailureKind.emailNotVerified,
        message: l10n.errorEmailNotVerified,
      );
    case 'rate_limited':
      return _rateLimited(e, l10n);
    case 'service_unavailable':
      return _unavailable(e, l10n);
    case 'invalid_access_token' || 'invalid_refresh_token':
      return FailurePresentation(
        FailureKind.sessionInvalid,
        message: l10n.errorSessionInvalid,
      );
    case 'internal_error' || 'invalid_request':
      return _unexpected(l10n);
    // Google sign-in codes (020). No screen calls that endpoint yet; stage 6
    // gives them their own messages.
    case 'invalid_google_token' || 'google_email_unusable' || 'account_exists':
      return _unexpected(l10n);
  }
  // No code, or one this app doesn't know (a proxy's error page, a newer
  // backend): only the status is reliable.
  return switch (e.statusCode) {
    422 => _validation(e, l10n),
    429 => _rateLimited(e, l10n),
    503 => _unavailable(e, l10n),
    _ => _unexpected(l10n),
  };
}

FailurePresentation _validation(ApiHttpException e, AppLocalizations l10n) {
  String? email;
  String? password;
  var unshown = e.fields.isEmpty;
  for (final f in e.fields) {
    final text = switch ((f.field, f.code)) {
      ('email', 'invalid') => l10n.errorEmailInvalid,
      ('password', 'required') => l10n.passwordRequired,
      ('password', 'too_short') => l10n.errorPasswordTooShort,
      ('password', 'too_long') => l10n.errorPasswordTooLong,
      ('password', 'too_common') => l10n.errorPasswordTooCommon,
      ('password', 'same_as_email') => l10n.errorPasswordSameAsEmail,
      _ => null,
    };
    if (text == null) {
      unshown = true;
    } else if (f.field == 'email') {
      email ??= text;
    } else {
      password ??= text;
    }
  }
  return FailurePresentation(
    FailureKind.invalidInput,
    message: unshown ? l10n.errorCheckInput : null,
    emailError: email,
    passwordError: password,
  );
}

FailurePresentation _rateLimited(ApiHttpException e, AppLocalizations l10n) {
  final wait = e.retryAfter;
  return FailurePresentation(
    FailureKind.rateLimited,
    message: wait == null
        ? l10n.errorRateLimitedNoWait
        : l10n.errorRateLimited(formatWait(wait, l10n)),
  );
}

FailurePresentation _unavailable(ApiHttpException e, AppLocalizations l10n) {
  final wait = e.retryAfter;
  return FailurePresentation(
    FailureKind.unavailable,
    message: wait == null
        ? l10n.errorUnavailableNoWait
        : l10n.errorUnavailable(formatWait(wait, l10n)),
  );
}

FailurePresentation _unexpected(AppLocalizations l10n) =>
    FailurePresentation(FailureKind.unexpected, message: l10n.errorUnexpected);

/// A `Retry-After` wait in words, rounded up so the user never retries too
/// early: seconds below a minute, then whole minutes below an hour, then
/// whole hours.
String formatWait(Duration wait, AppLocalizations l10n) {
  final seconds = (wait.inMilliseconds / 1000).ceil().clamp(1, 1 << 31);
  if (seconds < 60) return l10n.waitSeconds(seconds);
  if (seconds < 3600) return l10n.waitMinutes((seconds / 60).ceil());
  return l10n.waitHours((seconds / 3600).ceil());
}

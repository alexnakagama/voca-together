/// The backend routes the app calls (backend `internal/server/server.go`).
///
/// Public account actions are called through `AccountApi`; every other route
/// carries or returns a token and is called only by `AuthApi`.
abstract final class ApiPaths {
  static const healthz = '/healthz';
  static const register = '/v1/auth/register';
  static const resendVerification = '/v1/auth/resend-verification';
  static const forgotPassword = '/v1/auth/forgot-password';
  static const login = '/v1/auth/login';
  static const google = '/v1/auth/google';
  static const refresh = '/v1/auth/refresh';
  static const logout = '/v1/auth/logout';
  static const me = '/v1/me';
  static const profile = '/v1/me/profile';

  /// The catalog every member chooses from.
  static const languages = '/v1/languages';

  /// The signed-in user's own languages.
  static const myLanguages = '/v1/me/languages';
}

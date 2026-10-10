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

  /// The signed-in user's own profile picture.
  static const myAvatar = '/v1/me/avatar';

  /// The public profile of the member whose public identifier is [id].
  ///
  /// Throws [ArgumentError] unless [id] is the canonical spelling, a
  /// lowercase UUID: an identifier is not text, so nothing is trimmed or
  /// lower-cased (decision 031). The value is never echoed.
  static String memberProfile(String id) {
    checkMemberId(id);
    return '/v1/profiles/$id';
  }

  /// The profile picture of the member whose public identifier is [id], as
  /// for [memberProfile].
  static String memberAvatar(String id) => '${memberProfile(id)}/avatar';

  /// The members the signed-in user has blocked.
  static const myBlocks = '/v1/me/blocks';

  /// The signed-in user's block of the member whose public identifier is
  /// [id] (decision 033). [id] as for [memberProfile].
  static String myBlock(String id) {
    checkMemberId(id);
    return '$myBlocks/$id';
  }

  /// The signed-in user's report of the member whose public identifier is
  /// [id] (decision 033). [id] as for [memberProfile].
  static String myReport(String id) {
    checkMemberId(id);
    return '/v1/me/reports/$id';
  }

  /// Throws [ArgumentError] unless [id] is a canonical public identifier,
  /// as [memberProfile] does.
  static void checkMemberId(String id) {
    if (!_memberId.hasMatch(id)) throw ArgumentError('id is not a member id');
  }

  static final _memberId = RegExp(
    r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
  );
}

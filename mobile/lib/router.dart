import 'package:go_router/go_router.dart';

import 'api/account_api.dart';
import 'media/photo_source.dart';
import 'screens/blocked_members_screen.dart';
import 'screens/forgot_password_screen.dart';
import 'screens/home_screen.dart';
import 'screens/languages_screen.dart';
import 'screens/login_screen.dart';
import 'screens/member_profile_screen.dart';
import 'screens/profile_edit_screen.dart';
import 'screens/profile_screen.dart';
import 'screens/register_screen.dart';
import 'screens/splash_screen.dart';
import 'session.dart';

/// Route paths. Never put a token, email or other personal data in a path or
/// query string.
abstract final class Routes {
  static const splash = '/splash';
  static const login = '/login';
  static const register = '/register';
  static const forgotPassword = '/forgot-password';
  static const home = '/home';
  static const profile = '/profile';

  /// The screen where the user edits their own profile. Like the profile it
  /// names nobody.
  static const profileEdit = '/profile/edit';

  /// The editor of the user's own languages. Like the profile it names
  /// nobody, and it carries no language (decision 030).
  static const languages = '/profile/languages';

  /// The members the user has blocked. Like the profile it names nobody.
  static const blocked = '/blocked';

  /// The read-only public profile of the member whose public identifier is
  /// [id] (decision 032). The identifier is the only thing a route says
  /// about anyone: never a name, a language or an account id.
  static String member(String id) => '/members/$id';

  /// The routes a signed-out user may visit.
  static const authRoutes = {login, register, forgotPassword};

  /// The exact routes a signed-in user may visit. The profile, its edit
  /// screen, the languages and the blocked members are the user's own and
  /// name nobody; the one route that names a member is [member], matched by
  /// [isMember].
  static const signedInRoutes = {
    home,
    profile,
    profileEdit,
    languages,
    blocked,
  };

  /// Whether [path] is [member] for a well-formed public identifier: the
  /// canonical lowercase UUID, with nothing before or after it.
  static bool isMember(String path) => _memberPath.hasMatch(path);

  static final _memberPath = RegExp(
    r'^/members/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
  );
}

/// Where a user with [status] may be when navigating to [location]: `null` to
/// stay, otherwise the path to go to instead.
///
/// This is the app's only navigation policy; screens never decide access.
/// Paths are matched exactly (the query is ignored), so anything unexpected,
/// including unmatched paths and a member path whose identifier is
/// malformed, goes to the status's default route.
String? authRedirect(SessionStatus status, Uri location) {
  final path = location.path;
  switch (status) {
    case SessionStatus.unknown:
      return path == Routes.splash ? null : Routes.splash;
    case SessionStatus.signedOut:
      return Routes.authRoutes.contains(path) ? null : Routes.login;
    case SessionStatus.signedIn:
      return Routes.signedInRoutes.contains(path) || Routes.isMember(path)
          ? null
          : Routes.home;
  }
}

/// Builds the app's router, which re-runs [authRedirect] whenever [session]
/// changes. Each screen gets only the dependencies it uses.
///
/// The caller owns the router and must [GoRouter.dispose] it, which also
/// stops it listening to [session].
GoRouter createRouter(
  SessionManager session,
  AccountApi accountApi,
  PhotoSource photoSource,
) {
  return GoRouter(
    initialLocation: Routes.splash,
    refreshListenable: session,
    redirect: (context, state) => authRedirect(session.status, state.uri),
    routes: [
      GoRoute(
        path: Routes.splash,
        builder: (context, state) => const SplashScreen(),
      ),
      GoRoute(
        path: Routes.login,
        builder: (context, state) =>
            LoginScreen(session: session, accountApi: accountApi),
      ),
      GoRoute(
        path: Routes.register,
        builder: (context, state) =>
            RegisterScreen(session: session, accountApi: accountApi),
      ),
      GoRoute(
        path: Routes.forgotPassword,
        builder: (context, state) =>
            ForgotPasswordScreen(accountApi: accountApi),
      ),
      GoRoute(
        path: Routes.home,
        builder: (context, state) => HomeScreen(session: session),
      ),
      GoRoute(
        path: Routes.profile,
        builder: (context, state) => ProfileScreen(session: session),
      ),
      GoRoute(
        path: Routes.profileEdit,
        builder: (context, state) =>
            ProfileEditScreen(session: session, photoSource: photoSource),
      ),
      GoRoute(
        path: Routes.languages,
        builder: (context, state) => LanguagesScreen(session: session),
      ),
      GoRoute(
        path: Routes.blocked,
        builder: (context, state) => BlockedMembersScreen(session: session),
      ),
      GoRoute(
        path: '/members/:id',
        builder: (context, state) => MemberProfileScreen(
          session: session,
          id: state.pathParameters['id']!,
        ),
      ),
    ],
  );
}

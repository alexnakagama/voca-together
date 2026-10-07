import 'package:go_router/go_router.dart';

import 'api/account_api.dart';
import 'media/photo_source.dart';
import 'screens/forgot_password_screen.dart';
import 'screens/home_screen.dart';
import 'screens/languages_screen.dart';
import 'screens/login_screen.dart';
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

  /// The routes a signed-out user may visit.
  static const authRoutes = {login, register, forgotPassword};

  /// The routes a signed-in user may visit. The profile, its edit screen and
  /// the languages are the user's own: no route names another member.
  static const signedInRoutes = {home, profile, profileEdit, languages};
}

/// Where a user with [status] may be when navigating to [location]: `null` to
/// stay, otherwise the path to go to instead.
///
/// This is the app's only navigation policy; screens never decide access.
/// Paths are matched exactly (the query is ignored), so anything unexpected,
/// including unmatched paths, goes to the status's default route.
String? authRedirect(SessionStatus status, Uri location) {
  final path = location.path;
  switch (status) {
    case SessionStatus.unknown:
      return path == Routes.splash ? null : Routes.splash;
    case SessionStatus.signedOut:
      return Routes.authRoutes.contains(path) ? null : Routes.login;
    case SessionStatus.signedIn:
      return Routes.signedInRoutes.contains(path) ? null : Routes.home;
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
        builder: (context, state) => ProfileEditScreen(session: session),
      ),
      GoRoute(
        path: Routes.languages,
        builder: (context, state) => LanguagesScreen(session: session),
      ),
    ],
  );
}

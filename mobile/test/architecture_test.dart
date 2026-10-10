// Enforces the token boundaries of decisions 023 and 025 over the real source: Dart
// can't make a class private to a set of files, so the import rules are
// checked here instead. Runs from the package root, like every flutter test.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// Every library under lib/, by path relative to lib/, with comments removed.
Map<String, String> _sources() {
  final sources = <String, String>{};
  for (final entity in Directory('lib').listSync(recursive: true)) {
    if (entity is File && entity.path.endsWith('.dart')) {
      final path = entity.path.substring('lib/'.length).replaceAll(r'\', '/');
      sources[path] = _stripComments(entity.readAsStringSync());
    }
  }
  return sources;
}

String _stripComments(String source) => source
    .replaceAll(RegExp(r'/\*.*?\*/', dotAll: true), '')
    .split('\n')
    .map((line) => line.replaceFirst(RegExp(r'//.*$'), ''))
    .join('\n');

final _import = RegExp(
  r'''^\s*(?:import|export)\s+'([^']+)'.*;''',
  multiLine: true,
);

/// The libraries [path] imports or exports, normalized: `package:vocatogether/x`
/// and relative URIs become `x` (relative to lib/); other packages stay as is.
Set<String> _importsOf(String path, String source) {
  final dir = path.contains('/')
      ? path.substring(0, path.lastIndexOf('/'))
      : '';
  return {
    for (final m in _import.allMatches(source)) _normalize(dir, m.group(1)!),
  };
}

String _normalize(String dir, String uri) {
  const self = 'package:vocatogether/';
  if (uri.startsWith(self)) return uri.substring(self.length);
  if (uri.contains(':')) return uri;
  final parts = [if (dir.isNotEmpty) ...dir.split('/')];
  for (final segment in uri.split('/')) {
    if (segment == '..') {
      parts.removeLast();
    } else if (segment != '.') {
      parts.add(segment);
    }
  }
  return parts.join('/');
}

bool _isUi(String path) =>
    path.startsWith('screens/') || path.startsWith('ui/');

void main() {
  final sources = _sources();

  test('the scan sees the code it guards', () {
    expect(
      sources.keys,
      containsAll(['session.dart', 'main.dart', 'app.dart']),
    );
    expect(sources.keys.where(_isUi), isNotEmpty);
    expect(
      _importsOf('session.dart', sources['session.dart']!),
      containsAll(['api/auth_api.dart', 'auth/token_store.dart']),
    );
  });

  test(
    'token-bearing libraries are imported only inside the session layer',
    () {
      // library (or package prefix) → the only lib/ files that may import it.
      const allowed = <String, Set<String>>{
        'api/auth_api.dart': {'session.dart', 'main.dart'},
        'api/api_client.dart': {
          'api/account_api.dart',
          'api/auth_api.dart',
          'main.dart',
        },
        'auth/auth_tokens.dart': {
          'api/api_client.dart',
          'api/auth_api.dart',
          'auth/token_store.dart',
          'session.dart',
        },
        'auth/token_store.dart': {'session.dart', 'main.dart'},
        'auth/auth_clock.dart': {'session.dart'},
        // The source of Google ID tokens (025): held by the session only.
        'auth/google_identity.dart': {
          'auth/google_identity_plugin.dart',
          'session.dart',
        },
        'auth/google_identity_plugin.dart': {'main.dart'},
        'package:google_sign_in/': {'auth/google_identity_plugin.dart'},
        'package:flutter_secure_storage/': {
          'auth/token_store.dart',
          'main.dart',
        },
        'package:http/': {'api/api_client.dart', 'main.dart'},
        // The photo chooser (032): one adapter over the plugin, built in main.
        'media/photo_source_plugin.dart': {'main.dart'},
        'package:image_picker': {'media/photo_source_plugin.dart'},
      };
      final violations = <String>[];
      sources.forEach((path, source) {
        for (final imported in _importsOf(path, source)) {
          for (final MapEntry(key: library, value: importers)
              in allowed.entries) {
            final matches = library.startsWith('package:')
                ? imported.startsWith(library)
                : imported == library;
            if (matches && !importers.contains(path)) {
              violations.add('$path imports $imported');
            }
          }
        }
      });
      expect(violations, isEmpty);
    },
  );

  test(
    'screens and widgets reach the API only through token-free libraries',
    () {
      // What a screen may import from the networking and session layers.
      const uiAllowed = {
        'api/account_api.dart',
        'api/api_exception.dart',
        // Whom the member blocked, and why a member reports (033): no token.
        'api/blocked_member.dart',
        'api/languages.dart',
        'api/me.dart',
        // What one member may read about another (031): holds no token.
        'api/member_profile.dart',
        'api/profile.dart',
        'api/report_reason.dart',
        // Only the failure Google sign-in can end with; it holds no data.
        'auth/google_identity_exception.dart',
        'session.dart',
      };
      final violations = <String>[];
      sources.forEach((path, source) {
        if (!_isUi(path)) return;
        for (final imported in _importsOf(path, source)) {
          final layer =
              imported.startsWith('api/') ||
              imported.startsWith('auth/') ||
              imported == 'session.dart' ||
              imported == 'main.dart' ||
              imported.startsWith('package:http/') ||
              imported.startsWith('package:flutter_secure_storage/') ||
              imported.startsWith('package:google_sign_in') ||
              imported.startsWith('package:image_picker') ||
              imported == 'media/photo_source_plugin.dart';
          if (layer && !uiAllowed.contains(imported)) {
            violations.add('$path imports $imported');
          }
        }
      });
      expect(violations, isEmpty);
    },
  );

  test('screens and widgets never name a token type or token value', () {
    final forbidden = RegExp(
      r'\b(AuthApi|AuthTokens|StoredSession|StoredSessionCodec|TokenStore|'
      r'SecureTokenStore|ApiClient|accessToken|refreshToken|access_token|'
      r'refresh_token|Authorization)\b|vt_at_|vt_rt_|'
      // Google (025): no ID token, no token source, no plugin type, no
      // client ID. (GoogleSignInButton, GoogleSignInSection and
      // GoogleIdentityException are other words.)
      r'[iI]dToken|id_token|\b(GoogleIdentity|PluginGoogleIdentity|'
      r'GoogleSignIn|GoogleSignInAccount|GoogleSignInAuthentication)\b|'
      r'[sS]erverClientId',
    );
    final violations = <String>[];
    sources.forEach((path, source) {
      if (!_isUi(path)) return;
      for (final m in forbidden.allMatches(source)) {
        violations.add('$path: ${m.group(0)}');
      }
    });
    expect(violations, isEmpty);
  });

  test("SessionManager's public API neither takes nor returns a token", () {
    final source = sources['session.dart']!;
    final start = source.indexOf('class SessionManager');
    final body = source.substring(start, source.indexOf('\n}', start));
    // Public members are declared at two-space indentation; the constructor
    // (which takes the AuthApi from main) and private members are excluded.
    final declarations = body
        .split('\n')
        .where(
          (line) =>
              RegExp(r'^  [A-Za-z]').hasMatch(line) &&
              !RegExp(r'^  (SessionManager\(|static|final|late|bool _|int _)')
                  .hasMatch(line) &&
              !RegExp(r'^  [\w<>?, ]+ _').hasMatch(line) &&
              !line.trimLeft().startsWith('_'),
        )
        .toList();
    expect(declarations, isNotEmpty);
    final tokenish = RegExp(
      r'accessToken|refreshToken|AuthTokens|StoredSession|TokenStore|'
      r'String Function|authorized|[iI]dToken|GoogleIdentity',
    );
    for (final line in declarations) {
      expect(line, isNot(matches(tokenish)), reason: line);
    }
    expect(
      declarations.map(
        (l) => RegExp(r' (\w+)[({]? ?(?:=>|\(|\{)').firstMatch(l)?.group(1),
      ),
      containsAll([
        'restore',
        'signIn',
        'signInWithGoogle',
        'me',
        'profile',
        'saveProfile',
        'languageCatalog',
        'languages',
        'saveLanguages',
        'avatar',
        'saveAvatar',
        'removeAvatar',
        'memberProfile',
        'memberAvatar',
        'blockedMembers',
        'blockMember',
        'unblockMember',
        'reportMember',
        'logout',
      ]),
    );
    expect(
      body,
      isNot(contains(RegExp(r'^  Future<T> authorized', multiLine: true))),
    );
    // Google sign-in takes nothing: the ID token is obtained inside (025).
    expect(body, contains('  Future<void> signInWithGoogle() {'));
  });

  test('a Google ID token is never kept: no field, no top-level variable', () {
    // In the session layer it may only be a parameter or a local of the one
    // call that sends it.
    final declaration = RegExp(
      r'^(?:  )?(?:static\s+)?(?:late\s+)?(?:final\s+|var\s+|const\s+)?'
      r'[\w<>?, ]*\b_?\w*[iI]dToken\w*\s*(?:=|;)',
      multiLine: true,
    );
    for (final path in [
      'session.dart',
      'api/auth_api.dart',
      'auth/google_identity.dart',
      'auth/google_identity_plugin.dart',
    ]) {
      final matches = declaration
          .allMatches(sources[path]!)
          .map((m) => m.group(0)!.trim())
          // The size limits are constants about tokens, not tokens.
          .where((m) => !m.contains('maxIdTokenBytes'));
      expect(matches, isEmpty, reason: path);
    }
    // And nothing else in lib/ names one at all.
    final named = sources.keys.where(
      (path) => RegExp(r'[iI]dToken|id_token').hasMatch(sources[path]!),
    );
    expect(
      named.toSet().difference({
        'session.dart',
        'api/auth_api.dart',
        'auth/google_identity.dart',
        'auth/google_identity_plugin.dart',
      }),
      isEmpty,
    );
  });

  test('the plugin adapter asks Google for an ID token and nothing else', () {
    final adapter = sources['auth/google_identity_plugin.dart']!;
    // No silent sign-in, no event stream, no scopes or access tokens, no
    // profile data, no nonce or hosted domain, no revocation.
    final forbidden = RegExp(
      r'attemptLightweightAuthentication|authenticationEvents|'
      r'authorizationClient|authorize|scopeHint|accessToken|serverAuthCode|'
      r'\.email\b|displayName|photoUrl|\.id\b|nonce|hostedDomain|disconnect|'
      r'clientId:',
    );
    expect(forbidden.allMatches(adapter).map((m) => m.group(0)), isEmpty);
    expect(RegExp(r'\.authenticate\(\)').allMatches(adapter), hasLength(1));
    expect(RegExp(r'\.initialize\(').allMatches(adapter), hasLength(1));
  });

  test('main gives the Google identity to the session and to nothing else', () {
    final main = sources['main.dart']!;
    expect('PluginGoogleIdentity('.allMatches(main), hasLength(1));
    final session = RegExp(
      r'SessionManager\((.*?)\n  \);',
      dotAll: true,
    ).firstMatch(main)!.group(1)!;
    expect(session, contains('PluginGoogleIdentity('));
    // The client ID reaches no widget: screens ask the session instead.
    for (final path in sources.keys.where(_isUi)) {
      expect(
        sources[path],
        isNot(contains('googleServerClientId')),
        reason: path,
      );
    }
    expect(sources['app.dart'], isNot(contains('googleServerClientId')));
    expect(sources['router.dart'], isNot(contains('googleServerClientId')));
  });

  test('main passes only config, session, AccountApi and the photo source to '
      'the widget tree', () {
    final main = sources['main.dart']!;
    expect(
      RegExp(r'VocaTogetherApp\(([^)]*)\)')
          .firstMatch(main)!
          .group(1)!
          .replaceAll(RegExp(r'\s'), ''),
      'config:config,session:session,accountApi:accountApi,'
      'photoSource:photoSource,',
    );
  });

  test(
    'the photo adapter opens the gallery for one image and nothing else',
    () {
      final adapter = sources['media/photo_source_plugin.dart']!;
      // No camera, no video or mixed media, no several photos, and no recovery
      // of a pick lost when Android killed the activity (032).
      final forbidden = RegExp(
        r'ImageSource\.camera|CameraDevice|pickMultiImage|pickVideo|pickMedia|'
        r'pickMultipleMedia|pickMultiVideo|retrieveLostData|\.path\b|\.name\b',
      );
      expect(forbidden.allMatches(adapter).map((m) => m.group(0)), isEmpty);
      expect(RegExp(r'\.pickImage\(').allMatches(adapter), hasLength(1));
      expect(adapter, contains('source: ImageSource.gallery'));
      // main builds the one source; no screen or widget names the plugin.
      expect(
        'PluginPhotoSource('.allMatches(sources['main.dart']!),
        hasLength(1),
      );
    },
  );

  test('screens never read the session status or make access decisions', () {
    // Navigation policy lives only in router.dart (021): screens call
    // SessionManager's actions and let the redirect react.
    final forbidden = RegExp(r'\bSessionStatus\b|\.status\b|\bauthRedirect\b');
    final violations = <String>[];
    sources.forEach((path, source) {
      if (!path.startsWith('screens/')) return;
      for (final m in forbidden.allMatches(source)) {
        violations.add('$path: ${m.group(0)}');
      }
    });
    expect(violations, isEmpty);
  });

  test('release builds fail closed without Google configuration', () {
    // kReleaseMode can't be flipped in a host test, so the wiring is checked
    // in the source; config_test covers what `required` does.
    final config = sources['config.dart']!.replaceAll(RegExp(r'\s+'), ' ');
    expect(
      config,
      contains(
        'googleServerClientId: parseGoogleServerClientId( '
        "const String.fromEnvironment('GOOGLE_SERVER_CLIENT_ID'), "
        'required: kReleaseMode, )',
      ),
    );
  });

  test('nothing that handles a token prints or logs', () {
    final forbidden = RegExp(
      r'\b(print|debugPrint|debugPrintStack|log)\s*\(|dart:developer|'
      r'\bstdout\b|\bstderr\b',
    );
    final violations = <String>[];
    sources.forEach((path, source) {
      if (!path.startsWith('api/') &&
          !path.startsWith('auth/') &&
          path != 'session.dart') {
        return;
      }
      for (final m in forbidden.allMatches(source)) {
        violations.add('$path: ${m.group(0)}');
      }
    });
    expect(violations, isEmpty);
  });

  test('screens and widgets never print or log', () {
    final forbidden = RegExp(
      r'\b(print|debugPrint|debugPrintStack|log)\s*\(|dart:developer|'
      r'\bstdout\b|\bstderr\b',
    );
    final violations = <String>[];
    sources.forEach((path, source) {
      if (!_isUi(path)) return;
      for (final m in forbidden.allMatches(source)) {
        violations.add('$path: ${m.group(0)}');
      }
    });
    expect(violations, isEmpty);
  });
}

// Enforces the token boundary of decision 023 over the real source: Dart
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
        'package:flutter_secure_storage/': {
          'auth/token_store.dart',
          'main.dart',
        },
        'package:http/': {'api/api_client.dart', 'main.dart'},
      };
      final violations = <String>[];
      sources.forEach((path, source) {
        for (final imported in _importsOf(path, source)) {
          for (final MapEntry(key: library, value: importers)
              in allowed.entries) {
            final matches = library.endsWith('/')
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
        'api/me.dart',
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
              imported.startsWith('package:flutter_secure_storage/');
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
      r'refresh_token|Authorization)\b|vt_at_|vt_rt_',
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
      r'String Function|authorized',
    );
    for (final line in declarations) {
      expect(line, isNot(matches(tokenish)), reason: line);
    }
    expect(
      declarations.map(
        (l) => RegExp(r' (\w+)[({]? ?(?:=>|\(|\{)').firstMatch(l)?.group(1),
      ),
      containsAll(['restore', 'signIn', 'signInWithGoogle', 'me', 'logout']),
    );
    expect(
      body,
      isNot(contains(RegExp(r'^  Future<T> authorized', multiLine: true))),
    );
  });

  test(
    'main passes only config, session and AccountApi to the widget tree',
    () {
      final main = sources['main.dart']!;
      expect(
        RegExp(r'VocaTogetherApp\(([^)]*)\)')
            .firstMatch(main)!
            .group(1)!
            .replaceAll(RegExp(r'\s'), ''),
        'config:config,session:session,accountApi:accountApi',
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

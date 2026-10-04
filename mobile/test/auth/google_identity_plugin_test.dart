import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_sign_in_platform_interface/google_sign_in_platform_interface.dart';
import 'package:vocatogether/api/auth_api.dart';
import 'package:vocatogether/auth/google_identity.dart';
import 'package:vocatogether/auth/google_identity_exception.dart';
import 'package:vocatogether/auth/google_identity_plugin.dart';

import '../support/fake_google_platform.dart';

const _clientId = '1234-abc.apps.googleusercontent.com';

Matcher _fails(GoogleIdentityFailure failure) => throwsA(
  isA<GoogleIdentityException>().having((e) => e.failure, 'failure', failure),
);

void main() {
  late FakeGooglePlatform platform;
  late PluginGoogleIdentity identity;

  setUp(() {
    platform = FakeGooglePlatform();
    GoogleSignInPlatform.instance = platform;
    identity = PluginGoogleIdentity(serverClientId: _clientId);
  });

  tearDown(() => expect(platform.forbidden, isEmpty));

  test('the size limit is the one AuthApi enforces', () {
    expect(GoogleIdentity.maxIdTokenBytes, AuthApi.maxIdTokenBytes);
  });

  group('idToken', () {
    test('returns the ID token of an interactive sign-in', () async {
      platform.answer('hdr.payload.sig');

      expect(await identity.idToken(), 'hdr.payload.sig');
      expect(platform.authenticates, hasLength(1));
    });

    test('initializes once with only the server client ID, lazily', () async {
      expect(platform.inits, isEmpty, reason: 'nothing at construction');
      platform
        ..answer('a.b.c')
        ..answer('d.e.f');

      await identity.idToken();
      await identity.idToken();
      await identity.clear();

      expect(platform.inits, hasLength(1));
      final init = platform.inits.single;
      expect(init.serverClientId, _clientId);
      expect(init.clientId, isNull);
      expect(init.nonce, isNull);
      expect(init.hostedDomain, isNull);
    });

    test('requests no scopes', () async {
      platform.answer('a.b.c');
      await identity.idToken();
      expect(platform.authenticates.single.scopeHint, isEmpty);
    });

    test('every call asks Google again: nothing is cached', () async {
      platform
        ..answer('first.b.c')
        ..answer('second.b.c');

      expect(await identity.idToken(), 'first.b.c');
      expect(await identity.idToken(), 'second.b.c');
      expect(platform.authenticates, hasLength(2));
    });

    const codes = {
      GoogleSignInExceptionCode.canceled: GoogleIdentityFailure.cancelled,
      GoogleSignInExceptionCode.interrupted: GoogleIdentityFailure.interrupted,
      GoogleSignInExceptionCode.uiUnavailable:
          GoogleIdentityFailure.unavailable,
      GoogleSignInExceptionCode.providerConfigurationError:
          GoogleIdentityFailure.unavailable,
      GoogleSignInExceptionCode.clientConfigurationError:
          GoogleIdentityFailure.misconfigured,
      GoogleSignInExceptionCode.userMismatch: GoogleIdentityFailure.unknown,
      GoogleSignInExceptionCode.unknownError: GoogleIdentityFailure.unknown,
    };
    test('the table covers every plugin code', () {
      expect(codes.keys.toSet(), GoogleSignInExceptionCode.values.toSet());
    });
    for (final MapEntry(key: code, value: failure) in codes.entries) {
      test('maps ${code.name} to ${failure.name}', () async {
        platform.fail(
          GoogleSignInException(
            code: code,
            description: 'LEAK description $fakeGoogleEmail',
            details: 'LEAK details',
          ),
        );
        await expectLater(identity.idToken(), _fails(failure));
      });
    }

    test('maps anything else the plugin throws to unknown', () async {
      platform
        ..fail(
          PlatformException(code: 'LEAK', message: 'LEAK $fakeGoogleEmail'),
        )
        ..fail(MissingPluginException('LEAK'))
        ..fail(StateError('LEAK'))
        ..fail(UnimplementedError('LEAK'));
      for (var i = 0; i < 4; i++) {
        await expectLater(
          identity.idToken(),
          _fails(GoogleIdentityFailure.unknown),
        );
      }
    });

    test('its exceptions carry nothing from the plugin', () async {
      platform.fail(
        GoogleSignInException(
          code: GoogleSignInExceptionCode.clientConfigurationError,
          description: 'LEAK $_clientId $fakeGoogleEmail',
          details: 'LEAK',
        ),
      );
      try {
        await identity.idToken();
        fail('expected GoogleIdentityException');
      } on GoogleIdentityException catch (e) {
        expect('$e', 'GoogleIdentityException(misconfigured)');
      }
    });

    final malformed = <String, String?>{
      'null': null,
      'empty': '',
      'two segments': 'a.b',
      'four segments': 'a.b.c.d',
      'an empty segment': 'a..c',
      'a trailing dot': 'a.b.c.',
      'standard base64 characters': 'a+/.b.c',
      'padding': 'a.b.c=',
      'whitespace': 'a.b.c\n',
      'a space inside': 'a.b .c',
      'non-ASCII': 'a.b.ñ',
      'one byte over the limit':
          'a.b.${'c' * (GoogleIdentity.maxIdTokenBytes - 3)}',
    };
    for (final MapEntry(key: name, value: token) in malformed.entries) {
      test('refuses a token that is $name', () async {
        platform.answer(token);
        await expectLater(
          identity.idToken(),
          _fails(GoogleIdentityFailure.malformed),
        );
      });
    }

    test('accepts a token exactly at the size limit', () async {
      final token = 'a.b.${'c' * (GoogleIdentity.maxIdTokenBytes - 4)}';
      expect(token.length, GoogleIdentity.maxIdTokenBytes);
      platform.answer(token);
      expect(await identity.idToken(), token);
    });

    test('is unavailable where the platform has no authenticate', () async {
      platform.supports = false;
      await expectLater(
        identity.idToken(),
        _fails(GoogleIdentityFailure.unavailable),
      );
      expect(platform.authenticates, isEmpty);
    });

    test(
      'a failed initialization stays failed and is never repeated',
      () async {
        platform.initError = PlatformException(code: 'LEAK');
        await expectLater(
          identity.idToken(),
          _fails(GoogleIdentityFailure.unavailable),
        );
        platform.initError = null;
        await expectLater(
          identity.idToken(),
          _fails(GoogleIdentityFailure.unavailable),
        );
        expect(platform.inits, hasLength(1));
        expect(platform.authenticates, isEmpty);
      },
    );

    test('refuses a second call while one is running', () async {
      final gate = Completer<AuthenticationResults>();
      platform
        ..wait(gate)
        ..answer('later.b.c');

      final first = identity.idToken();
      await expectLater(identity.idToken(), throwsStateError);
      expect(platform.authenticates.length, lessThanOrEqualTo(1));

      gate.complete(
        const AuthenticationResults(
          user: GoogleSignInUserData(email: fakeGoogleEmail, id: 'LEAKsub'),
          authenticationTokens: AuthenticationTokenData(idToken: 'a.b.c'),
        ),
      );
      expect(await first, 'a.b.c');
      // The slot is free again, after success and after failure.
      expect(await identity.idToken(), 'later.b.c');
      platform
        ..fail(StateError('x'))
        ..answer('again.b.c');
      await expectLater(
        identity.idToken(),
        _fails(GoogleIdentityFailure.unknown),
      );
      expect(await identity.idToken(), 'again.b.c');
    });
  });

  group('clear', () {
    test("clears Google's credential state", () async {
      await identity.clear();
      expect(platform.signOuts, 1);
      expect(platform.inits, hasLength(1));
    });

    test('never throws', () async {
      platform.signOutError = PlatformException(code: 'LEAK');
      await identity.clear();
      platform.signOutError = StateError('LEAK');
      await identity.clear();
      expect(platform.signOuts, 2);
    });

    test('does nothing when initialization failed', () async {
      platform.initError = StateError('LEAK');
      await identity.clear();
      expect(platform.signOuts, 0);
    });
  });
}

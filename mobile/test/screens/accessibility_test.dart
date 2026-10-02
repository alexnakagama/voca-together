import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/form_notice_banner.dart';

import '../support/fakes.dart';
import 'harness.dart';

/// One screen in one state: how to script the backend, whether it starts
/// signed in, how to get there, and the control that must stay reachable.
final class _Case {
  const _Case(
    this.name, {
    this.script,
    this.signedIn = false,
    this.drive,
    required this.action,
  });

  final String name;
  final void Function(FakeServer server)? script;
  final bool signedIn;
  final Future<void> Function(WidgetTester tester)? drive;
  final String action;
}

Future<void> _logInAttempt(WidgetTester tester) async {
  await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
  await tester.enterText(field(l10n.passwordLabel), 'correct horse');
  await tapAndSettle(
    tester,
    find.widgetWithText(FilledButton, l10n.logInButton),
  );
}

Future<void> _openRegister(WidgetTester tester) =>
    tapAndSettle(tester, find.text(l10n.createAccountLink));

Future<void> _openForgot(WidgetTester tester) =>
    tapAndSettle(tester, find.text(l10n.forgotPasswordLink));

final _cases = <_Case>[
  _Case('login', action: l10n.logInButton),
  _Case(
    'login, empty fields',
    drive: (t) =>
        tapAndSettle(t, find.widgetWithText(FilledButton, l10n.logInButton)),
    action: l10n.logInButton,
  ),
  _Case(
    'login, 401',
    script: (s) => s.once(
      'POST',
      ApiPaths.login,
      (_) => errorResponse(401, 'invalid_credentials'),
    ),
    drive: _logInAttempt,
    action: l10n.logInButton,
  ),
  _Case(
    'login, not verified, resent',
    script: (s) => s
      ..once(
        'POST',
        ApiPaths.login,
        (_) => errorResponse(403, 'email_not_verified'),
      )
      ..once('POST', ApiPaths.resendVerification, (_) => accepted()),
    drive: (t) async {
      await _logInAttempt(t);
      await tapAndSettle(t, find.text(l10n.resendVerificationButton));
    },
    action: l10n.logInButton,
  ),
  _Case(
    'register, errors',
    drive: (t) async {
      await _openRegister(t);
      await t.enterText(field(l10n.passwordLabel), 'a');
      await t.enterText(field(l10n.confirmPasswordLabel), 'b');
      await tapAndSettle(
        t,
        find.widgetWithText(FilledButton, l10n.registerButton),
      );
    },
    action: l10n.registerButton,
  ),
  _Case(
    'register, check your email',
    script: (s) => s.once('POST', ApiPaths.register, (_) => accepted()),
    drive: (t) async {
      await _openRegister(t);
      await t.enterText(field(l10n.emailLabel), 'ana@example.com');
      await t.enterText(field(l10n.passwordLabel), 'correct horse');
      await t.enterText(field(l10n.confirmPasswordLabel), 'correct horse');
      await tapAndSettle(
        t,
        find.widgetWithText(FilledButton, l10n.registerButton),
      );
    },
    action: l10n.backToLogIn,
  ),
  _Case(
    'forgot password, 429',
    script: (s) => s.once(
      'POST',
      ApiPaths.forgotPassword,
      (_) => errorResponse(429, 'rate_limited', headers: {'retry-after': '60'}),
    ),
    drive: (t) async {
      await _openForgot(t);
      await t.enterText(field(l10n.emailLabel), 'ana@example.com');
      await tapAndSettle(
        t,
        find.widgetWithText(FilledButton, l10n.forgotPasswordButton),
      );
    },
    action: l10n.forgotPasswordButton,
  ),
  _Case(
    'forgot password, sent',
    script: (s) => s.once('POST', ApiPaths.forgotPassword, (_) => accepted()),
    drive: (t) async {
      await _openForgot(t);
      await t.enterText(field(l10n.emailLabel), 'ana@example.com');
      await tapAndSettle(
        t,
        find.widgetWithText(FilledButton, l10n.forgotPasswordButton),
      );
    },
    action: l10n.backToLogIn,
  ),
  _Case(
    'home',
    signedIn: true,
    script: (s) =>
        s.once('GET', ApiPaths.me, (_) => jsonResponse(200, meBody())),
    action: l10n.logOutButton,
  ),
  _Case(
    'home, failed',
    signedIn: true,
    script: (s) => s.once('GET', ApiPaths.me, networkFailure),
    action: l10n.logOutButton,
  ),
];

Future<void> _reach(
  WidgetTester tester,
  _Case c, {
  Size? size,
  double textScale = 1,
  double keyboard = 0,
  Brightness brightness = Brightness.light,
}) async {
  final server = FakeServer();
  c.script?.call(server);
  await pumpApp(
    tester,
    server: server,
    signedIn: c.signedIn,
    size: size,
    textScale: textScale,
    keyboard: keyboard,
    brightness: brightness,
  );
  await c.drive?.call(tester);
  expect(tester.takeException(), isNull, reason: c.name);
}

/// The main control can be scrolled to and receives the tap.
Future<void> _expectReachable(WidgetTester tester, String label) async {
  final finder = find.text(label).last;
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  final center = tester.getCenter(finder);
  final view = tester.view.physicalSize / tester.view.devicePixelRatio;
  expect(center.dy, inInclusiveRange(0, view.height), reason: label);
  final hit = tester.hitTestOnBinding(center);
  expect(
    hit.path.any((e) => e.target == tester.renderObject(finder)),
    isTrue,
    reason: '$label is covered',
  );
}

void main() {
  for (final c in _cases) {
    group(c.name, () {
      for (final brightness in Brightness.values) {
        testWidgets('meets the tap-target, label and contrast guidelines '
            '(${brightness.name})', (tester) async {
          final handle = tester.ensureSemantics();
          await _reach(tester, c, brightness: brightness);
          await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
          await expectLater(tester, meetsGuideline(labeledTapTargetGuideline));
          await expectLater(tester, meetsGuideline(textContrastGuideline));
          handle.dispose();
        });
      }

      testWidgets('large text on a small screen', (tester) async {
        await _reach(tester, c, size: const Size(320, 480), textScale: 2);
        await _expectReachable(tester, c.action);
      });

      testWidgets('with the keyboard open', (tester) async {
        await _reach(tester, c, size: const Size(360, 640), keyboard: 300);
        await _expectReachable(tester, c.action);
      });
    });
  }

  group('semantics', () {
    testWidgets('screen titles are headers', (tester) async {
      final handle = tester.ensureSemantics();
      await pumpApp(tester);
      expect(
        tester.getSemantics(find.text(l10n.logInTitle).first),
        isSemantics(label: l10n.logInTitle, isHeader: true),
      );
      handle.dispose();
    });

    testWidgets('field errors are part of the field\'s semantics', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      await pumpApp(tester);
      await tapAndSettle(
        tester,
        find.widgetWithText(FilledButton, l10n.logInButton),
      );
      final node = tester.getSemantics(field(l10n.emailLabel));
      final data = node.getSemanticsData();
      expect(
        '${data.label}\n${data.value}\n${data.hint}',
        contains(l10n.emailRequired),
      );
      handle.dispose();
    });

    testWidgets('banners are live regions with a text label for the icon', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final server = FakeServer()
        ..once(
          'POST',
          ApiPaths.login,
          (_) => errorResponse(403, 'email_not_verified'),
        )
        ..once('POST', ApiPaths.resendVerification, networkFailure);
      await pumpApp(tester, server: server);
      await _logInAttempt(tester);
      expect(
        tester.getSemantics(find.byType(FormNoticeBanner)),
        isSemantics(
          isLiveRegion: true,
          label:
              '${l10n.noticeLabel}\n'
              '${l10n.emailNotVerifiedNotice('ana@example.com')}',
        ),
      );
      await tapAndSettle(tester, find.text(l10n.resendVerificationButton));
      expect(
        tester.getSemantics(find.byType(FormErrorBanner)),
        isSemantics(
          isLiveRegion: true,
          label: '${l10n.errorLabel}\n${l10n.errorNetwork}',
        ),
      );
      handle.dispose();
    });

    testWidgets('the password toggle is labelled in both states', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      await pumpApp(tester);
      expect(find.byTooltip(l10n.showPassword), findsOneWidget);
      await tester.tap(find.byTooltip(l10n.showPassword));
      await tester.pump();
      expect(find.byTooltip(l10n.hidePassword), findsOneWidget);
      handle.dispose();
    });

    testWidgets('a busy button keeps its label for screen readers', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final server = FakeServer()..once('POST', ApiPaths.login, neverAnswers);
      await pumpApp(tester, server: server);
      await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
      await tester.enterText(field(l10n.passwordLabel), 'correct horse');
      await tester.tap(find.widgetWithText(FilledButton, l10n.logInButton));
      await tester.pump();
      expect(
        tester.getSemantics(find.byType(FilledButton)),
        isSemantics(
          label: l10n.logInButton,
          isButton: true,
          hasEnabledState: true,
          isEnabled: false,
        ),
      );
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      handle.dispose();
    });
  });
}

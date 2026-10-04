import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:vocatogether/app.dart';
import 'package:vocatogether/config.dart';
import 'package:vocatogether/l10n/app_localizations.dart';
import 'package:vocatogether/session.dart';

import '../support/fakes.dart';

/// The app's English strings, for finding and comparing text.
final l10n = lookupAppLocalizations(const Locale('en'));

/// The real app over fakes: the real router, `SessionManager` and
/// `AccountApi`, talking to [server].
final class TestApp {
  TestApp(this.server, this.session, this.store, this.google);

  final FakeServer server;
  final SessionManager session;
  final InMemoryTokenStore store;

  /// The scripted Google side, or null when the app was pumped without
  /// Google configuration.
  final FakeGoogleIdentity? google;

  /// The router's current location, as a URL would show it.
  String location(WidgetTester tester) =>
      GoRouter.of(tester.element(find.byType(Navigator).first)).state.uri
          .toString();
}

/// Pumps the app, restored signed out (on log in) or [signedIn] (on home,
/// which loads `/v1/me`: script it first).
///
/// With [google], the app has Google sign-in over that fake; without, it is
/// a build with no Google configuration (no button).
///
/// [size] sets the logical screen size, [textScale] the system font scale,
/// [keyboard] a bottom inset like an open keyboard. With [settle] false, only
/// two frames are pumped (for requests that never finish, where a spinner
/// would keep `pumpAndSettle` waiting).
Future<TestApp> pumpApp(
  WidgetTester tester, {
  FakeServer? server,
  FakeGoogleIdentity? google,
  bool signedIn = false,
  Size? size,
  double textScale = 1,
  double keyboard = 0,
  Brightness brightness = Brightness.light,
  bool settle = true,
}) async {
  final backend = server ?? FakeServer();
  if (size != null || keyboard > 0) {
    tester.view.devicePixelRatio = 1;
    if (size != null) tester.view.physicalSize = size;
    if (keyboard > 0) {
      tester.view.viewInsets = FakeViewPadding(bottom: keyboard);
    }
    addTearDown(tester.view.reset);
  }
  tester.platformDispatcher
    ..textScaleFactorTestValue = textScale
    ..platformBrightnessTestValue = brightness;
  addTearDown(tester.platformDispatcher.clearAllTestValues);

  final clock = FakeAuthClock();
  final store = InMemoryTokenStore(
    raw: signedIn ? storedRaw(clock, 'home') : null,
  );
  final session = SessionManager(
    store: store,
    authApi: authApiFor(backend.client),
    clock: clock,
    google: google,
  );
  addTearDown(session.dispose);
  await session.restore();

  await tester.pumpWidget(
    VocaTogetherApp(
      config: AppConfig(apiBaseUrl: Uri.parse('http://10.0.2.2:8080')),
      session: session,
      accountApi: accountApiFor(backend.client),
    ),
  );
  if (settle) {
    await tester.pumpAndSettle();
  } else {
    await tester.pump();
    await tester.pump();
  }
  return TestApp(backend, session, store, google);
}

/// The text field labelled [label].
Finder field(String label) => find.widgetWithText(TextField, label);

/// The [TextField] shown inside [finder].
TextField textFieldOf(WidgetTester tester, Finder finder) =>
    tester.widget<TextField>(finder);

/// Whether the field labelled [label] has keyboard focus.
bool hasFocus(WidgetTester tester, String label) =>
    textFieldOf(tester, field(label)).focusNode?.hasFocus ?? false;

/// The error text the field labelled [label] shows, if any.
String? errorOf(WidgetTester tester, String label) =>
    textFieldOf(tester, field(label)).decoration?.errorText;

/// Scrolls [finder] into view and taps it, then lets the result render.
Future<void> tapAndSettle(WidgetTester tester, Finder finder) async {
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

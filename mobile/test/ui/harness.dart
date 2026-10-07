import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/l10n/app_localizations.dart';
import 'package:vocatogether/ui/theme.dart';

/// Pumps [child] in an app with the real themes and localizations.
///
/// [page] is used as the whole screen; otherwise [child] is placed in a padded
/// page. [size] sets the logical screen size (device pixel ratio 1), and
/// [viewInsets] simulates the keyboard.
Future<void> pumpUi(
  WidgetTester tester,
  Widget child, {
  Brightness brightness = Brightness.light,
  double textScale = 1,
  Size? size,
  EdgeInsets viewInsets = EdgeInsets.zero,
  bool page = false,
}) async {
  if (size != null) {
    tester.view
      ..devicePixelRatio = 1
      ..physicalSize = size;
    addTearDown(tester.view.reset);
  }
  await tester.pumpWidget(
    MaterialApp(
      theme: AppTheme.light,
      darkTheme: AppTheme.dark,
      themeMode: brightness == Brightness.dark
          ? ThemeMode.dark
          : ThemeMode.light,
      localizationsDelegates: AppLocalizations.localizationsDelegates,
      supportedLocales: AppLocalizations.supportedLocales,
      home: Builder(
        builder: (context) => MediaQuery(
          data: MediaQuery.of(context).copyWith(
            textScaler: TextScaler.linear(textScale),
            viewInsets: viewInsets,
          ),
          child: page
              ? child
              : Scaffold(
                  body: SafeArea(
                    child: Padding(
                      padding: const EdgeInsets.all(Spacing.md),
                      child: Center(child: child),
                    ),
                  ),
                ),
        ),
      ),
    ),
  );
}

/// WCAG 2 contrast ratio between two opaque colors.
double contrastRatio(Color a, Color b) {
  final la = a.computeLuminance();
  final lb = b.computeLuminance();
  final (hi, lo) = la > lb ? (la, lb) : (lb, la);
  return (hi + 0.05) / (lo + 0.05);
}

/// Waits until [bytes], shown by a widget already pumped, has been decoded
/// or refused, and renders the result. Decoding is real asynchronous work,
/// which a widget test only does inside `runAsync`.
Future<void> pumpDecoded(WidgetTester tester, Uint8List bytes) async {
  await tester.runAsync(() async {
    final done = Completer<void>();
    // The same key as the widget's image, so this joins its decode.
    MemoryImage(bytes)
        .resolve(ImageConfiguration.empty)
        .addListener(
          ImageStreamListener(
            (_, _) => done.complete(),
            onError: (_, _) => done.complete(),
          ),
        );
    await done.future;
  });
  await tester.pump();
}

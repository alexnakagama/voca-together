import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/previews/app_text_field_previews.dart';
import 'package:vocatogether/ui/previews/auth_scaffold_previews.dart';
import 'package:vocatogether/ui/previews/form_error_banner_previews.dart';
import 'package:vocatogether/ui/previews/form_notice_banner_previews.dart';
import 'package:vocatogether/ui/previews/google_sign_in_button_previews.dart';
import 'package:vocatogether/ui/previews/preview_support.dart';
import 'package:vocatogether/ui/previews/primary_button_previews.dart';
import 'package:vocatogether/ui/previews/secondary_button_previews.dart';
import 'package:vocatogether/ui/widgets/app_text_field.dart';

/// Pumps [preview] laid out the way Flutter 3.47's widget previewer lays out a
/// preview card: inside a vertical and a horizontal scroll view (so width and
/// height are unbounded), sized only by the preview's `size`, where an
/// infinite dimension means "unconstrained" (`ZoomablePreviewArea` and
/// `_WidgetPreviewWrapper` in the generated `.widget_preview` scaffold).
Future<void> _pumpLikePreviewer(
  WidgetTester tester,
  Widget preview,
  Size size,
) async {
  final l10n = previewLocalizations();
  await tester.pumpWidget(
    MaterialApp(
      localizationsDelegates: l10n.localizationsDelegates,
      supportedLocales: l10n.supportedLocales,
      home: SingleChildScrollView(
        child: SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          child: SizedBox(
            width: size.width.isFinite ? size.width : null,
            height: size.height.isFinite ? size.height : null,
            child: previewWrapper(preview),
          ),
        ),
      ),
    ),
  );
}

/// Every preview builds and lays out in both brightnesses under the
/// previewer's constraints.
void main() {
  // Each preview with the `size` its annotation resolves to; keep in sync
  // with the `@VocaPreview` annotations.
  const fallback = VocaPreview.defaultSize;
  final previews = <String, (Widget Function(), Size)>{
    'emailField': (emailField, fallback),
    'emailFieldWithError': (emailFieldWithError, fallback),
    'passwordField': (passwordField, fallback),
    'passwordFieldDark': (passwordFieldDark, fallback),
    'newPasswordFieldDisabled': (newPasswordFieldDisabled, fallback),
    'nameField': (nameField, fallback),
    'nameFieldWithError': (nameFieldWithError, fallback),
    'multilineField': (multilineField, fallback),
    'multilineFieldDarkFilled': (multilineFieldDarkFilled, fallback),
    'primaryButton': (primaryButton, fallback),
    'primaryButtonDisabled': (primaryButtonDisabled, fallback),
    'primaryButtonBusy': (primaryButtonBusy, fallback),
    'primaryButtonLargeText': (primaryButtonLargeText, fallback),
    'googleButtonLight': (googleButtonLight, fallback),
    'googleButtonDark': (googleButtonDark, fallback),
    'googleButtonDisabled': (googleButtonDisabled, fallback),
    'googleButtonLargeText': (googleButtonLargeText, const Size(320, 160)),
    'errorBanner': (errorBanner, fallback),
    'errorBannerLong': (errorBannerLong, fallback),
    'noticeBanner': (noticeBanner, fallback),
    'noticeBannerLong': (noticeBannerLong, fallback),
    'secondaryButton': (secondaryButton, fallback),
    'secondaryButtonBusy': (secondaryButtonBusy, fallback),
    'secondaryButtonLargeText': (secondaryButtonLargeText, fallback),
    'authScaffoldLight': (authScaffoldLight, const Size(390, 760)),
    'authScaffoldDarkError': (authScaffoldDarkError, const Size(390, 760)),
    'authScaffoldSmallLargeText': (
      authScaffoldSmallLargeText,
      const Size(320, 480),
    ),
  };

  test('the default preview size bounds the width', () {
    expect(VocaPreview.defaultSize.width.isFinite, isTrue);
    expect(
      const VocaPreview(group: 'g', name: 'n').size,
      VocaPreview.defaultSize,
    );
  });

  testWidgets('the harness rejects a preview with unbounded width', (
    tester,
  ) async {
    // Guards the harness itself: without a width, a text field fails exactly
    // as it did in the real previewer.
    await _pumpLikePreviewer(
      tester,
      const AppTextField.email(),
      const Size(double.infinity, double.infinity),
    );
    expect(tester.takeException(), isNotNull);
  });

  for (final MapEntry(key: name, value: (build, size)) in previews.entries) {
    for (final brightness in Brightness.values) {
      testWidgets('$name ($brightness)', (tester) async {
        tester.platformDispatcher.platformBrightnessTestValue = brightness;
        addTearDown(tester.platformDispatcher.clearPlatformBrightnessTestValue);

        await _pumpLikePreviewer(tester, build(), size);
        expect(tester.takeException(), isNull);
      });
    }
  }
}

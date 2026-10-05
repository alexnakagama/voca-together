# 022: Design system, reusable auth widgets and localization (client stage 3)

> **Status:** in force.
>
> **Changed later:** 024 added `FormNoticeBanner`, `SecondaryButton` and the error-code mapping; 025 wired
> the Google button; 028 added `AppTextField.text` and `AppTextField.multiline`.
>
> **Current rules:** `.claude/rules/mobile.md`, `.claude/rules/google-sign-in.md` (the Google button).

- **Identity:** Material 3. `ColorScheme.fromSeed` with a deep-teal seed (`#00696B`); the four tertiary roles are the
  primary roles of a coral-seeded scheme (`#E8735A`) of the same brightness, so each on-color keeps the contrast
  Material computed for its pair (tests require ≥ 4.5:1 for every text pair used). Platform font (Roboto), system
  light/dark. Buttons are at least 48 dp tall; filled and outlined buttons are stadium-shaped; fields are outlined with
  a 12 dp radius. `Spacing` (4/8/16/24/32) and `Radii` in `lib/ui/theme.dart` are the only tokens: no theme
  extensions or per-component token classes until a real need appears.
- **Reusable widgets** (`lib/ui/widgets/`: `AppTextField`, `PrimaryButton`, `GoogleSignInButton`, `FormErrorBanner`,
  `AuthScaffold`) take data and callbacks by constructor, never import `Session`, the router or `AppConfig`, and hold
  only ephemeral UI state (password visibility, the tap latch). User-visible strings come from `AppLocalizations` or
  from the caller. Screens keep owning forms, `AutofillGroup`, validation and submission.
- **Fields:** `AppTextField.email` (email keyboard and autofill, no autocorrect) and `AppTextField.password`
  (obscured with a show/hide toggle; autocorrect, suggestions, smart punctuation and IME personalized learning off;
  autofill `password`, or `newPassword` for registration and reset). Neither trims nor validates. Server field errors
  (422 `fields`) are shown through `errorText`, which overrides any `Form` validator error.
- **Double submission:** the screen owns `busy` and passes it to `PrimaryButton`, which then shows a spinner, ignores
  taps and keeps its size and enabled colors. The button also drops a second tap in the same frame (before the parent
  can rebuild); the latch releases on the next frame, so it can't wedge. The keyboard's done action bypasses the
  button, so the submit handler remains the final guard and must check its own busy flag.
- **Google button** (presentation only; sign-in itself is a later stage): Google's light (`#FFFFFF` fill, `#747775`
  stroke, `#1F1F1F` text) or dark (`#131314`, `#8E918F`, `#E3E3E3`) button theme following the app's brightness, never
  the app's scheme; pill shape (matching `PrimaryButton`); 12 dp / 10 dp / 12 dp padding around a 20 dp logo; text
  "Continue with Google" (an approved label, localizable). The logo is Google's gradient G, cropped unaltered
  (pixel copy of the 20×20 dp region at offset 10,10) from the icon-only Square Light/Dark PNGs at @1x/@3x/@4x of
  `https://developers.google.com/static/identity/images/signin-assets.zip` (downloaded 2026-10-02), so each crop
  carries its theme's fill and must only be shown on that fill. It is painted with `Ink.image` so ripples cover it.
  Deliberate deviations: Roboto Medium 14/20 instead of Google Sans Medium (OFL, but only a 5 MB variable font;
  revisit with a subset if needed), a 48 dp minimum height instead of 40 dp to pair with `PrimaryButton` and meet tap
  target size, and a disabled appearance of our own (the whole button at 38% opacity, colors unchanged), since Google
  defines no disabled state and forbids recoloring the logo.
- **Errors** use `FormErrorBanner`: error-container colors plus an icon with a semantic label and the message text, so
  meaning never rests on color alone, inside a live region so TalkBack announces it. Mapping API error codes to
  messages is a later stage.
- **`AuthScaffold`:** a title (semantic header) and children in a scroll view that is centered when it fits, scrolls
  under the keyboard, small screens and large text, dismisses the keyboard on drag, and caps the content at 480 dp.
- **Localization:** `flutter_localizations` and `intl` (imported by the generated code; its version is pinned by
  `flutter_localizations`) join the Flutter libraries of 008. gen-l10n runs through `flutter: generate: true` with
  `l10n.yaml` (`nullable-getter: false`, `required-resource-attributes: true`, so every string needs a description).
  English only; the generated `lib/l10n/app_localizations*.dart` is committed so string-API changes show in review;
  never edit it by hand. `MaterialApp.router` gets the delegates, locales, themes and its title from l10n. Existing
  placeholder screens keep their hardcoded text until they are rebuilt.
- **Widget previews** (`lib/ui/previews/`) use `@VocaPreview`, a `Preview` subclass that applies the app theme for the
  preview's brightness through `wrapper` and the app localizations through `localizations`. The unstable
  `PreviewThemeData` interface is avoided. Previews are pure UI (no `dart:io`, plugins, HTTP, session or config);
  their text is sample content. `test/ui/previews_test.dart` builds every preview in both brightnesses.
- **Deferred:** a Google Sans subset, more locales, golden tests, app icon and native splash colors, screens adopting
  these widgets.

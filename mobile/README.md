# VocaTogether mobile

Flutter client (Android only). Commands are in the repository's `CLAUDE.md`, conventions in
`.claude/rules/mobile.md`, the layer map in `docs/architecture.md` and design decisions in `docs/decisions/`
(indexed by `docs/decisions.md`).

```sh
flutter run --dart-define-from-file=config/dev.json   # emulator → local backend at http://10.0.2.2:8080
```

## Google sign-in

Rules: `.claude/rules/google-sign-in.md`. Design and reasoning: decisions 020 (backend contract), 025 (client) and
026 (a Google ID token is accepted while it is valid, not once).

`GOOGLE_SERVER_CLIENT_ID` in `config/<env>.json` is the **Web application** OAuth client ID of the Google Cloud
project, the same value the backend gets as `GOOGLE_CLIENT_ID`. It is public. Release builds refuse to start without
it; a debug build without it runs with no Google button.

```json
{
  "API_BASE_URL": "http://10.0.2.2:8080",
  "GOOGLE_SERVER_CLIENT_ID": "<number>-<id>.apps.googleusercontent.com"
}
```

Google Cloud setup (one project that holds only VocaTogether's clients):

- A Web application OAuth client with no JavaScript origins and no redirect URIs. Never download its secret.
- An Android OAuth client for package `com.vocatogether.app` and the SHA-1 of **each** certificate that signs a build:
  - your debug keystore:
    `keytool -list -v -keystore ~/.android/debug.keystore -alias androiddebugkey -storepass android`
    (it also signs `--release` builds until release signing exists);
  - later, the upload key and the Play App Signing key (Play Console → App integrity).
- A consent screen. While it is in "Testing", only the listed test users can sign in.

There is no `google-services.json` and no Android client ID anywhere in the app. A wrong SHA-1, package name or client
ID usually shows up as a button that does nothing (Google reports it as a cancelled sign-in).

Smoke test, on an emulator with a Google Play image and a Google account added. Restart the backend first so that
migration 00004 has run (`\dt google_id_token_uses` in the dev database then finds nothing):

1. `make db-up`, `GOOGLE_CLIENT_ID=<web id> make run`, then `flutter run --dart-define-from-file=config/dev.json`.
2. Continue with Google → choose the account → home shows its email. Backend log: `google sign-in succeeded`.
3. Log out, then at once Continue with Google again → home again, and a second `google sign-in succeeded` with
   `new_account=false`. Google usually returns the same ID token here; the backend accepts it (decision 026).
4. Force-stop the app (`adb shell am force-stop com.vocatogether.app`), open it: still signed in. Log out, force-stop,
   open, Continue with Google → home.
5. Repeat step 3 three or four times in a row: every one succeeds, and no `google sign-in failed` appears.
6. Log out, Continue with Google, choose "Add another account" or a second account: it gets its own VocaTogether
   account (or the 409 message if its address already has one).
7. Close the account chooser with back: no error, the form works.
8. Register an address with a password, then Continue with Google with the Google account for that address: the
   "already a VocaTogether account" message on log in and on register, twice in a row; password login still works.
9. With the Google-created account: forgot password shows the usual confirmation (the email has no link), and
   password login shows the usual "incorrect email or password".
10. Stop the backend → connection error. Start it without `GOOGLE_CLIENT_ID` → "Google sign-in didn't work".
11. Airplane mode, double taps, rotating or backgrounding with the chooser open: no crash, the form stays usable.
12. Without `GOOGLE_SERVER_CLIENT_ID`: a debug build has no Google button, and a release build stops at startup.
13. `adb logcat` during all of it shows no token, email or client ID from the app.

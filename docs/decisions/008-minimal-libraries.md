# 008: Minimal libraries

> **Status:** in force as a principle; the two lists below are out of date. The current lists are
> `backend/go.mod` and `mobile/pubspec.yaml`.
>
> **Changed later:** the backend never adopted `x/time/rate` (018 wrote its own limiter) and added `x/text`
> (Unicode normalization, 009 and 027) and `x/image` (scaling profile pictures, 031). The client added
> `go_router` (021), `flutter_localizations` and `intl` (022), `google_sign_in` (025), and for tests only
> `fake_async` (023) and `google_sign_in_platform_interface` (025).

- Backend: stdlib `net/http` routing (Go 1.22+ patterns), pgx, goose, x/crypto, x/time/rate.
- Flutter: `http`, `flutter_secure_storage`, and `ChangeNotifier` for state.

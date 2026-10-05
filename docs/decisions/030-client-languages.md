# 030: Languages in the profile (client stage 8, draft)

> **Status:** draft. The decisions are approved; none of the code exists yet (see the first bullet).
>
> **Backend side:** 029.
>
> **Current rules:** `.claude/rules/languages.md`.

- **Status: draft.** This entry records the client-side decisions approved before any client code was written.
  **Nothing of it exists in `mobile/` yet:** the app calls none of the three language routes of 029. The entry is
  completed (final names, screen states, tests, deferred work) when the client ships, and the word "draft" is
  removed then. The backend it builds on is complete and described by 029, which this entry does not change.
- **Scope:** the signed-in user reads the catalog and reads and replaces their own languages, over the contract of
  029. Nothing about other members.
- **No minimum in the client.** `spoken` may be empty, `learning` may be empty, and both may be empty. The app
  adds no rule of its own, such as "at least one of each", before a save.
  - *Why:* the backend requires nothing (029 dropped the plan's `required` rule and left the question to the
    client). A minimum held only by the app would be a rule the API doesn't keep, so the stored data could never
    be relied on to satisfy it, and it would leave a member no way to take back the last language of a list.
    Saving two empty lists is how a member clears their languages (029).
  - This replaces the stage plan's client check "each list has at least one". Client validation stays what 024
    and 028 made it: the server owns every rule (`too_many`, `unknown_language`, `invalid_level`, `duplicate`),
    and the app sends the selection as chosen.
  - If matching later needs a language of each kind, that is a gate to decide then (028 left the profile gate
    open in the same way), not a rule of this editor.
- **The save model is the complete selection, and its JSON always holds both lists.** A requirement for the first
  client step (models, `AuthApi`, `SessionManager`):
  - the model the app saves represents the member's whole selection, both lists, never one list or a change to
    one;
  - its `toJson` **always emits both `spoken` and `learning`, each as an array**; an empty list is `[]`, never
    `null` and never left out;
  - the body of `PUT /v1/me/languages` is never partial: there is no "save only the list that was edited" path,
    and no serializer setting that drops empty or null members may sit between the model and the request.
  - *Why:* 029 refuses a body with a missing or `null` list with 400 `invalid_request` so that a client's mistake
    can't delete a member's languages. From the app that 400 is a generic failure the member can do nothing
    about, so the client must be unable to produce it. With no minimum (above), an empty list is an ordinary
    value the app sends often, which is exactly the case a careless serializer drops.
  - *To be tested with that step:* the exact PUT body for a selection with both lists filled, with each list
    empty in turn and with both empty, each showing both keys as arrays.
- **A separate editor, and a read-only summary on Profile.** Languages are edited on their own screen at
  `/profile/languages`; the Profile screen shows a read-only "Languages" summary and opens the editor. The
  profile form and its save (028) are not changed, and the two saves stay separate requests, as their resources
  are (029).
  - The route names nobody: like `/profile` it is always the caller's own, and it carries no language code.
  - It joins `Routes.signedInRoutes`; `authRedirect` stays a function of the session status and the path alone.
- **Languages are public by intent, and the app says so.** A member's languages and levels are what discovery
  will show other members (029). The app must tell the member this before they save, as the profile form does for
  the name and the text (028). *Where* it says so (on the editor, on the summary, or by extending the profile's
  existing notice) is decided during the implementation, following the conventions the screens already have.
  Nothing is shown to anyone yet.
- **Carried over from 029 and 023, for the implementation to respect:**
  - `GET /v1/me/languages` always answers 200, so "no languages yet" is two empty lists, not a 404 turned into a
    value as `profile_not_found` is (028).
  - The save is idempotent (029), which is what makes `_authorized`'s single resend after a 401 safe (023), and
    the member's own retry after a timeout or a 503.
  - The token boundary is unchanged: screens reach the routes only through token-free `SessionManager` methods
    and models that hold no token; a member's languages are personal data, so the models redact `toString`.
  - The failure mapping covers the four codes above on `spoken` and `learning`. There is no `required` code.
- **Not decided here:** the placement of the visibility notice (above); the layout of the summary and the
  editor; names of the new types and methods. They are settled in the client steps and written here then.

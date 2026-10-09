# Spec Delta

## Purpose

Lets a signed-in member see another member's public profile (name, text, picture and languages) by an opaque
public identifier, while everything else about an account stays private and nobody can change a profile that
is not their own.

## ADDED Requirements

### Requirement: Public profile identifier
Every saved profile SHALL have a public identifier that is random, opaque, unique, and never changes while the
profile exists. It SHALL NOT be the account's internal identifier and SHALL NOT be derived from it or from the
email address. The owner's own profile responses SHALL include it as `id`.

#### Scenario: Assigned on the first save
- **WHEN** a member saves a profile for the first time
- **THEN** the response contains an `id`, and reading the profile again returns the same `id`

#### Scenario: Stable across edits
- **WHEN** a member changes their name or text and saves
- **THEN** the profile's `id` is the one it had before

#### Scenario: Profiles saved before this change
- **WHEN** the change is deployed over existing profiles
- **THEN** every existing profile has an `id`, and no two profiles share one

#### Scenario: Not the account identifier
- **WHEN** a member reads their account and their profile
- **THEN** the profile's `id` differs from the account's `id`

### Requirement: Reading a member's public profile
`GET /v1/profiles/{id}` SHALL answer a signed-in member with 200 and the public profile that `id` names: `id`,
`display_name`, `bio`, `has_avatar`, and `languages` holding `spoken` and `learning`, each an array of
`{language, level}` in the owner's order. A member with no languages SHALL get two empty arrays.

#### Scenario: Another member's profile
- **WHEN** member A requests the profile id of member B, who has a name, a text and languages
- **THEN** the response is 200 with B's name, text and languages, as B last saved them

#### Scenario: One's own profile
- **WHEN** a member requests their own profile id
- **THEN** the response is 200 with the same public fields any other member would get

#### Scenario: No languages chosen
- **WHEN** the profile's owner has chosen no language
- **THEN** `languages.spoken` and `languages.learning` are both empty arrays

#### Scenario: Whether there is a picture
- **WHEN** the owner has a profile picture, and again after they remove it
- **THEN** `has_avatar` is `true`, and then `false`

### Requirement: Only public fields are disclosed
A member profile response SHALL contain only the fields listed in "Reading a member's public profile". It SHALL
NOT contain the email address, the account identifier, whether or when the email was verified, how the member
signs in, any timestamp, or anything about sessions.

#### Scenario: Exact fields
- **WHEN** a member reads another member's profile
- **THEN** the response's keys are exactly `id`, `display_name`, `bio`, `has_avatar` and `languages`

#### Scenario: Private data is absent
- **WHEN** a member reads another member's profile
- **THEN** the response body contains neither that member's email address nor their account identifier

### Requirement: Unknown and unavailable profiles are indistinguishable
`GET /v1/profiles/{id}` SHALL answer 404 `profile_not_found` when `id` names no profile, and the same response
when `id` is not a well-formed identifier. A member who has saved no profile has no public identifier, so
nothing a client can send reaches them.

#### Scenario: Unknown identifier
- **WHEN** a member requests a well-formed identifier that no profile has
- **THEN** the response is 404 `profile_not_found`

#### Scenario: Malformed identifier
- **WHEN** a member requests `/v1/profiles/not-an-id`, or a valid identifier written in upper case
- **THEN** the response is 404 `profile_not_found`, identical to the unknown case

#### Scenario: The account identifier is not accepted
- **WHEN** a member requests `/v1/profiles/` followed by another member's account identifier
- **THEN** the response is 404 `profile_not_found`

#### Scenario: The owner deleted their account
- **WHEN** a member requests the identifier of a profile whose account no longer exists
- **THEN** the response is 404 `profile_not_found`

### Requirement: Member profiles are for signed-in members only
The member profile routes SHALL require a valid access token and SHALL answer 401 without one, revealing
nothing about the identifier. Every response SHALL carry `Cache-Control: no-store`.

#### Scenario: No token
- **WHEN** a request for an existing profile carries no access token, or an invalid or expired one
- **THEN** the response is 401, the same as for an identifier that names no profile

#### Scenario: Not cached
- **WHEN** a member reads a profile, successfully or not
- **THEN** the response has `Cache-Control: no-store`

### Requirement: A member route cannot change anything
The routes that name a member SHALL be read-only. No request to a route containing a member's identifier SHALL
create, change or delete any data, whoever sends it. A member's profile, languages and picture change only
through the routes of their own session, which name nobody.

#### Scenario: Writing through a member route
- **WHEN** a member sends PUT, POST, PATCH or DELETE to `/v1/profiles/{id}` or `/v1/profiles/{id}/avatar`, with
  another member's identifier or their own
- **THEN** the response is 405 and nothing stored has changed

#### Scenario: An identifier in an owner route is ignored
- **WHEN** member A saves their profile with member B's identifier in the query string
- **THEN** A's profile is saved and B's is unchanged

#### Scenario: An identifier in an owner body is refused
- **WHEN** member A sends a profile save whose body carries an `id`
- **THEN** the response is 400 `invalid_request` and nothing is saved

### Requirement: Member reads are limited per reader
Reads of member profiles and member pictures SHALL be limited per signed-in member. A member who exceeds the
limit SHALL get 429 `rate_limited` with `Retry-After`, and the limit SHALL NOT affect that member's own
profile, languages or picture requests, or any other member.

#### Scenario: Over the limit
- **WHEN** a member reads member profiles faster than the limit allows
- **THEN** further reads answer 429 `rate_limited` with `Retry-After` until the limit recovers

#### Scenario: Own requests are unaffected
- **WHEN** a member has exhausted the member-read limit
- **THEN** they can still read and save their own profile

#### Scenario: Unauthenticated requests spend nothing
- **WHEN** requests without a valid token are sent to a member route
- **THEN** they answer 401 and no member's limit is spent

### Requirement: Member profile data is never logged
The backend SHALL NOT write a member's name, text, languages or public identifier to any log, for the owner's
requests or a reader's.

#### Scenario: Reading leaves no personal data in the logs
- **WHEN** a member reads another member's profile, successfully or with a 404
- **THEN** the logs contain neither the identifier requested nor any text of the profile

### Requirement: The member profile screen
The app SHALL show a member's public profile at `/members/:id` as a read-only page: the picture or, without
one, a placeholder built from the name; the name; the text; and the "I speak" and "I'm learning" languages
with their levels, in the member's order. It SHALL offer no way to edit and SHALL NOT show a Friends area.

#### Scenario: A profile with everything
- **WHEN** the screen opens for a member who has a picture, a text and languages
- **THEN** it shows the picture, the name, the text and both language lists

#### Scenario: No picture
- **WHEN** the member has no picture, or the picture cannot be loaded
- **THEN** the placeholder is shown and the rest of the profile is shown normally

#### Scenario: No languages
- **WHEN** the member has chosen no language
- **THEN** one text says no language is chosen, and neither list heading is shown

#### Scenario: Nothing to edit
- **WHEN** the screen shows any member, including the member using the app
- **THEN** there is no "Edit Profile" control, no editable field and no Friends area

#### Scenario: A language the catalog does not name
- **WHEN** the member has a language code the catalog does not list
- **THEN** the entry is shown with its code in place of the name

### Requirement: Member profile loading and failure states
The screen SHALL show a labelled progress indicator while loading. A profile that does not exist SHALL be
shown as unavailable, with no retry. Any other failure SHALL show the app's own localized message with a "Try
again" control. Nothing of the profile SHALL be shown unless the profile itself loaded.

#### Scenario: Unavailable
- **WHEN** the server answers 404 `profile_not_found`
- **THEN** the screen says the profile isn't available and offers no "Try again"

#### Scenario: A failure that can be retried
- **WHEN** the load fails with a network failure, a timeout, 429, 503 or an unexpected response
- **THEN** the matching localized message and "Try again" are shown, and activating it loads again

#### Scenario: The session ended
- **WHEN** the load fails because the session ended
- **THEN** no error is shown and the app goes to the log in screen

#### Scenario: A late answer
- **WHEN** an answer arrives after the member left the screen or after a newer load started
- **THEN** it is ignored

### Requirement: The member route
`/members/:id` SHALL be a signed-in route whose only parameter is a well-formed public identifier. Access SHALL
be decided only by the session status and the path. The route SHALL carry no token, email, name or language.

#### Scenario: Signed out
- **WHEN** a signed-out user navigates to `/members/` followed by a well-formed identifier
- **THEN** the app shows the log in screen

#### Scenario: A malformed identifier is not a route
- **WHEN** a signed-in user navigates to `/members/abc`, `/members/` or `/members/<id>/extra`
- **THEN** the app shows the home screen and sends no profile request

### Requirement: Privacy and accessibility of the member profile screen
The app SHALL print nothing about a member's profile, SHALL show no text taken from a server response other
than the profile's own fields and catalog names, and SHALL keep every state usable with labelled controls, 48
dp tap targets and sufficient contrast in both themes, at twice the normal text size on a 320 dp wide screen.

#### Scenario: Nothing printed
- **WHEN** a member profile is loaded, fails to load, and is loaded again
- **THEN** the app prints nothing

#### Scenario: Large text on a small screen
- **WHEN** a profile with a long name, a long text and five languages in each list is shown at text scale 2.0
  on a 320 by 480 dp screen
- **THEN** nothing overflows and the whole profile can be scrolled to

#### Scenario: The picture is described
- **WHEN** a screen reader reads the profile
- **THEN** the picture or its placeholder is announced as the member's profile picture, with their name

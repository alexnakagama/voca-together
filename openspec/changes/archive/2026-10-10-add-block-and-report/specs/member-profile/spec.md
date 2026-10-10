# Spec Delta

## MODIFIED Requirements

### Requirement: Unknown and unavailable profiles are indistinguishable
`GET /v1/profiles/{id}` SHALL answer 404 `profile_not_found` when `id` names no profile, and the same response
when `id` is not a well-formed identifier. A member who has saved no profile has no public identifier, so
nothing a client can send reaches them. It SHALL answer the same response, identical in status, body and
headers, when a block exists between the reader and the profile's owner, in either direction.

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

#### Scenario: The owner blocked the reader
- **WHEN** a member requests the identifier of a member who has blocked them
- **THEN** the response is 404 `profile_not_found`, identical to the unknown case

#### Scenario: The reader blocked the owner
- **WHEN** a member requests the identifier of a member they have blocked
- **THEN** the response is 404 `profile_not_found`, identical to the unknown case

#### Scenario: A block does not hide one's own profile
- **WHEN** a member who has blocked others, or is blocked by others, requests their own identifier
- **THEN** the response is 200

### Requirement: A member route cannot change anything
The routes under `/v1/profiles` SHALL be read-only. No request, to any route, SHALL create, change or delete
another member's profile, languages or picture, whoever sends it: they change only through the routes of their
owner's session. A route of the caller's own session SHALL name another member only as the target of the
caller's own block or report, and such a request SHALL change nothing that belongs to that member.

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

#### Scenario: Blocking or reporting changes nothing of the target
- **WHEN** member A blocks member B, or reports B
- **THEN** B's profile, languages and picture are stored exactly as before, and member C reads them unchanged

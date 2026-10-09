# Spec Delta

## MODIFIED Requirements

### Requirement: Reading a member's picture
`GET /v1/profiles/{id}/avatar` SHALL answer a signed-in member with 200 and the picture of the member that
`id` names, as `image/jpeg`. It SHALL answer 404 `avatar_not_found` when that member has a profile and no
picture, and 404 `profile_not_found` when `id` names no profile. When a block exists between the reader and
that member, in either direction, it SHALL answer 404 `profile_not_found`, whether or not there is a picture.

#### Scenario: A member with a picture
- **WHEN** member A requests the picture of member B, who has a profile and a picture
- **THEN** the response is 200 with B's stored picture

#### Scenario: A member without one
- **WHEN** the member named has a profile and no picture
- **THEN** the response is 404 `avatar_not_found`

#### Scenario: A picture without a profile is not public
- **WHEN** a member has uploaded a picture and has saved no profile
- **THEN** no request by another member returns that picture

#### Scenario: Unknown or malformed identifier
- **WHEN** the identifier names no profile or is not well formed
- **THEN** the response is 404 `profile_not_found`

#### Scenario: A block between the two members
- **WHEN** a block exists between the reader and the member named, made by either of them, and that member has
  a picture, or has none
- **THEN** the response is 404 `profile_not_found` in both cases, identical to the unknown case, and no picture
  is returned

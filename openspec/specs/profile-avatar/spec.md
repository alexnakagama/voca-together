# profile-avatar Specification

## Purpose

Lets a member set, replace and remove their own profile picture, guarantees that what is stored and shown is a
normalized image carrying no metadata, and lets signed-in members see the pictures of members who have a
profile.

## Requirements

### Requirement: Setting and replacing one's own picture
`PUT /v1/me/avatar` SHALL take the image as the request body and make it the signed-in member's picture,
replacing any earlier one. It SHALL answer 200 with the picture as stored, as `image/jpeg`. A member SHALL have
at most one picture, and the earlier one SHALL no longer be retrievable once replaced.

#### Scenario: First picture
- **WHEN** a member with no picture uploads a valid JPEG
- **THEN** the response is 200 with a JPEG body, and reading their picture returns the same bytes

#### Scenario: Replacing
- **WHEN** a member who has a picture uploads a different valid image
- **THEN** reading their picture returns the new one, and the earlier one is returned by no request

#### Scenario: Before a profile exists
- **WHEN** a member who has saved no profile uploads a valid image
- **THEN** the upload succeeds and they can read their own picture

### Requirement: Accepted input
The upload SHALL accept only JPEG and PNG images, recognized by their content, of at most 5 MiB and at most
4096 pixels on either side. The declared content type and any file name SHALL be ignored. A refused upload
SHALL answer 422 `validation_failed` naming the field `avatar` and one code, and SHALL leave the member's
current picture unchanged.

#### Scenario: Empty body
- **WHEN** the upload has no body
- **THEN** the response is 422 with `avatar: required`

#### Scenario: Too many bytes
- **WHEN** the body is larger than 5 MiB
- **THEN** the response is 422 with `avatar: too_large`, and the server read no more than the limit allows

#### Scenario: Not a JPEG or PNG
- **WHEN** the body is a GIF, a WebP, a PDF or plain text, whatever content type it declares
- **THEN** the response is 422 with `avatar: unsupported_type`

#### Scenario: A declared type does not make it one
- **WHEN** a PNG is uploaded declared as `image/jpeg`, or a JPEG with no declared type
- **THEN** the upload is accepted as what its content is

#### Scenario: Damaged image
- **WHEN** the body starts as a JPEG or PNG but cannot be decoded
- **THEN** the response is 422 with `avatar: invalid_image`

#### Scenario: Too many pixels
- **WHEN** the image is wider or taller than 4096 pixels, including one whose file is small
- **THEN** the response is 422 with `avatar: dimensions_too_large`, decided before the image is decoded

#### Scenario: A refused upload keeps the current picture
- **WHEN** a member who has a picture sends an upload that is refused
- **THEN** reading their picture still returns the earlier one

### Requirement: Stored pictures are normalized
Every stored picture SHALL be produced by the server from the decoded pixels: the orientation recorded in the
upload applied, the centre square kept, scaled to 512 by 512 pixels, and encoded as JPEG. Transparent areas
SHALL become white. The bytes a member uploaded SHALL never be stored or served.

#### Scenario: Size and format
- **WHEN** a 3000 by 2000 PNG is uploaded
- **THEN** the stored picture is a 512 by 512 JPEG showing the centre square of the upload

#### Scenario: A small image
- **WHEN** a 100 by 100 image is uploaded
- **THEN** it is accepted and stored as 512 by 512

#### Scenario: Orientation
- **WHEN** a JPEG whose metadata says it must be rotated to be upright is uploaded
- **THEN** the stored picture is upright

#### Scenario: Transparency
- **WHEN** a PNG with transparent areas is uploaded
- **THEN** those areas are white in the stored picture

### Requirement: Stored pictures carry no metadata
A stored picture SHALL contain no metadata from the upload: no location, no device or software name, no
timestamps, no embedded thumbnail, no comments and no color profile.

#### Scenario: Location is removed
- **WHEN** a JPEG holding GPS coordinates is uploaded and the member's picture is read back
- **THEN** the returned bytes contain no GPS coordinates and no EXIF segment

#### Scenario: Hidden data is removed
- **WHEN** an image with data appended after the end of the image, or with text chunks, is uploaded
- **THEN** none of that data is in the returned bytes

### Requirement: Uploading is idempotent
Uploading the bytes that produced the member's current picture SHALL change nothing and SHALL answer exactly
as the first upload did, so a client can safely repeat an upload whose answer it did not receive.

#### Scenario: The same upload twice
- **WHEN** a member uploads an image and then uploads the same bytes again
- **THEN** both responses are 200 with identical bodies, and nothing is rewritten the second time

### Requirement: Removing one's own picture
`DELETE /v1/me/avatar` SHALL remove the signed-in member's picture and answer 204, whether or not there was
one. A removed picture SHALL be returned by no later request.

#### Scenario: Removing
- **WHEN** a member with a picture removes it
- **THEN** the response is 204, and reading their picture answers 404 `avatar_not_found`

#### Scenario: Removing nothing
- **WHEN** a member with no picture sends the removal
- **THEN** the response is 204

#### Scenario: Other members stop seeing it
- **WHEN** a member removes their picture
- **THEN** a request for that member's picture by another member answers 404 `avatar_not_found`

### Requirement: Reading one's own picture
`GET /v1/me/avatar` SHALL answer 200 with the signed-in member's picture as `image/jpeg`, or 404
`avatar_not_found` when they have none.

#### Scenario: With a picture
- **WHEN** a member with a picture reads it
- **THEN** the response is 200, `image/jpeg`, with the stored bytes

#### Scenario: Without one
- **WHEN** a member with no picture reads it
- **THEN** the response is 404 `avatar_not_found`

### Requirement: Reading a member's picture
`GET /v1/profiles/{id}/avatar` SHALL answer a signed-in member with 200 and the picture of the member that
`id` names, as `image/jpeg`. It SHALL answer 404 `avatar_not_found` when that member has a profile and no
picture, and 404 `profile_not_found` when `id` names no profile.

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

### Requirement: A picture is changed only by its owner
The owner of a picture SHALL be the member whose access token the request carries, and nothing in a request
SHALL be able to name another member as the target of an upload or a removal. All avatar routes SHALL require
a valid access token and carry `Cache-Control: no-store`.

#### Scenario: No token
- **WHEN** an upload, a removal or a read carries no valid access token
- **THEN** the response is 401 and nothing is stored, removed or returned

#### Scenario: An identifier cannot redirect a write
- **WHEN** member A uploads or removes a picture with member B's identifier in the query string
- **THEN** only A's picture is affected and B's is unchanged

#### Scenario: Two sessions of one member
- **WHEN** a member uploads from one of their sessions and reads from another
- **THEN** the read returns the picture just uploaded

### Requirement: Avatar writes are limited
Uploads and removals SHALL share a per-member limit that answers 429 `rate_limited` with `Retry-After` once
exceeded, having done nothing. When the server is too busy processing images, an upload SHALL answer 503
`service_unavailable` with `Retry-After`, and repeating it SHALL be safe.

#### Scenario: Over the limit
- **WHEN** a member uploads more often than the limit allows
- **THEN** the next upload answers 429 `rate_limited` with `Retry-After` and their picture is unchanged

#### Scenario: Refused uploads count
- **WHEN** a member repeatedly sends uploads that are refused as invalid
- **THEN** they are limited as valid uploads are

#### Scenario: Other writes are unaffected
- **WHEN** a member has exhausted the avatar limit
- **THEN** they can still save their profile and their languages

### Requirement: Pictures are personal data
The backend SHALL NOT write image bytes, image sizes, dimensions or formats to any log. A member's picture
SHALL be deleted with their account.

#### Scenario: A save is logged without the image
- **WHEN** a member uploads or removes a picture
- **THEN** the log records that the member's picture changed and nothing about the image

#### Scenario: Account deletion
- **WHEN** a member's account is deleted
- **THEN** their picture no longer exists and no request returns it

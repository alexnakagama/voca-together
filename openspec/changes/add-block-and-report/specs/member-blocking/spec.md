# Spec Delta

## Purpose

Lets a signed-in member block and unblock another member, guarantees that two members with a block between them
cannot reach each other's profile or picture, and lets a member see and undo the blocks they made, without
telling anyone that they were blocked.

## ADDED Requirements

### Requirement: Blocking a member
`PUT /v1/me/blocks/{id}` SHALL make the signed-in member block the member whose profile has the public
identifier `id`, and answer 204 with no body. The blocker SHALL be the member whose access token the request
carries; nothing in the request SHALL be able to name another member as the blocker. The request body SHALL be
ignored.

#### Scenario: Blocking another member
- **WHEN** member A sends the block for member B's profile identifier
- **THEN** the response is 204, and B is in A's list of blocked members

#### Scenario: The blocker is the session
- **WHEN** member A sends a block for B with member C's identifier in the query string or in a body
- **THEN** A has blocked B, and C has blocked nobody and is blocked by nobody

#### Scenario: A reader needs no profile to block
- **WHEN** member A, who has saved no profile, sends the block for B's identifier
- **THEN** the response is 204 and B is blocked by A

### Requirement: Unblocking a member
`DELETE /v1/me/blocks/{id}` SHALL remove the signed-in member's block of the member `id` names and answer 204
with no body, whether or not there was one. It SHALL NOT remove a block that the other member made. The
identifier alone SHALL name the block to remove: the unblock SHALL NOT depend on that member being in the
caller's list of blocked members. A blocked member who has no profile has no identifier, so their block cannot
be removed until they have a profile again.

#### Scenario: Unblocking
- **WHEN** member A, who blocked B, sends the unblock for B's identifier
- **THEN** the response is 204, B is no longer in A's list, and A and B can read each other's profile again

#### Scenario: Unblocking someone who was not blocked
- **WHEN** member A sends the unblock for a member they never blocked
- **THEN** the response is 204 and nothing changes

#### Scenario: Only one's own block is removed
- **WHEN** A and B have each blocked the other, and A unblocks B
- **THEN** B's block of A still exists, and neither can read the other's profile

#### Scenario: Unblocking a member who is not in the list
- **WHEN** A has blocked B, B is absent from A's list of blocked members because B has also blocked A, and A
  sends the unblock for B's identifier
- **THEN** the response is 204, A's block of B no longer exists, and B's block of A still does

### Requirement: Blocking is idempotent
Repeating a block or an unblock SHALL change nothing and SHALL answer exactly as the first request did, so a
client can safely repeat one whose answer it did not receive. Concurrent requests for the same pair SHALL leave
at most one block from one member to the other.

#### Scenario: The same block twice
- **WHEN** member A blocks B and then sends the same block again
- **THEN** both responses are 204, A's list holds B once, and B's place in the list has not moved

#### Scenario: Concurrent identical blocks
- **WHEN** member A sends the block for B many times at once, from one or several of their sessions
- **THEN** every response is 204 and exactly one block from A to B exists

#### Scenario: Both block each other at the same moment
- **WHEN** A blocks B while B blocks A
- **THEN** both requests answer 204 and both blocks exist

#### Scenario: A block and an unblock at the same moment
- **WHEN** member A sends a block and an unblock for B at once
- **THEN** both answer 204, and B is either blocked or not, never in a state between

### Requirement: A member cannot block themselves
A block or an unblock whose identifier is the caller's own profile SHALL answer 422 `validation_failed` with
the field `member` and the code `self`, and SHALL store nothing.

#### Scenario: Blocking oneself
- **WHEN** a member sends the block for their own profile identifier
- **THEN** the response is 422 with `member: self`, and their list of blocked members is unchanged

#### Scenario: Nothing can store it
- **WHEN** anything tries to store a block whose two members are the same account
- **THEN** the database refuses it

### Requirement: A block write does not reveal whether a profile exists
A block or an unblock for an identifier that names no profile, well formed or not, SHALL answer 204 and store
nothing, exactly as a request for an existing member answers. A member SHALL be able to block a member who has
blocked them, with the same answer.

#### Scenario: Unknown identifier
- **WHEN** a member sends the block for a well-formed identifier that no profile has
- **THEN** the response is 204, and their list of blocked members is unchanged

#### Scenario: Malformed identifier
- **WHEN** a member sends the block for `not-an-id`, for an identifier in upper case, or for another member's
  account identifier
- **THEN** the response is 204 and nothing is stored

#### Scenario: Blocking someone who blocked you
- **WHEN** B has blocked A, and A sends the block for B's identifier
- **THEN** the response is 204, identical to the answer for an unknown identifier, and A has blocked B

### Requirement: A block hides both members from each other
While a block exists between two members, in either direction, a request by one of them for the other's public
profile or picture SHALL answer 404 `profile_not_found`, identical in status, body and headers to the answer
for an identifier that names no profile. The check SHALL be made by the backend on every such request.

#### Scenario: The blocked member cannot read the blocker
- **WHEN** A has blocked B, and B requests A's profile and A's picture
- **THEN** both responses are 404 `profile_not_found`, identical to those for an unknown identifier

#### Scenario: The blocker cannot read the blocked member
- **WHEN** A has blocked B, and A requests B's profile and B's picture
- **THEN** both responses are 404 `profile_not_found`

#### Scenario: Other members are unaffected
- **WHEN** A has blocked B, and member C requests A's and B's profiles
- **THEN** both responses are 200

#### Scenario: One's own profile is unaffected
- **WHEN** a member who has blocked others, and is blocked by others, requests their own profile identifier
- **THEN** the response is 200

#### Scenario: Not only in the app
- **WHEN** B, blocked by A, requests A's profile with a valid token and no app, directly from the API
- **THEN** the response is 404 `profile_not_found`

#### Scenario: Unblocking restores the read
- **WHEN** A unblocks B and no other block exists between them
- **THEN** each can read the other's profile and picture again

### Requirement: Listing the members one has blocked
`GET /v1/me/blocks` SHALL answer 200 with `blocks`, an array of `{id, display_name}` for the members the
signed-in member has blocked, most recently blocked first. It SHALL list only the caller's own blocks and SHALL
never show who has blocked the caller. A blocked member who has no profile SHALL NOT be listed. A blocked member
who has blocked the caller SHALL NOT be listed either, whoever blocked first; the caller's block of them SHALL
still exist and SHALL still be removable.

#### Scenario: A member's blocks
- **WHEN** member A has blocked B and then C
- **THEN** the response is 200 with C and then B, each with their profile identifier and current name

#### Scenario: No blocks
- **WHEN** a member who has blocked nobody reads the list
- **THEN** the response is 200 with `blocks` as an empty array

#### Scenario: Blocks made by others are not shown
- **WHEN** B has blocked A, and A reads the list
- **THEN** B is not in it

#### Scenario: Exact fields
- **WHEN** a member reads the list
- **THEN** each entry's keys are exactly `id` and `display_name`, and the body holds no account identifier, email
  address or timestamp

#### Scenario: A blocked member without a profile
- **WHEN** a blocked member's profile no longer exists while their account does
- **THEN** they are not in the list, and they are still blocked

#### Scenario: A blocked member who has blocked the caller
- **WHEN** A and B have each blocked the other, in either order, and B reads the list
- **THEN** A is not in it, the response is identical to the one B would get if A's profile did not exist, and
  B's block of A still exists and can be removed

#### Scenario: The other member's block is removed
- **WHEN** A and B have each blocked the other, and A unblocks B
- **THEN** A is in B's list again

### Requirement: The list of blocked members is one small response
A full list of blocked members SHALL be smaller than 64 KiB, whatever the members' names, so that a client reads
it whole without paging.

#### Scenario: A full list of long names
- **WHEN** a member has blocked 200 members whose names are each the longest allowed, made of any characters a
  name may hold
- **THEN** the response is 200 and smaller than 64 KiB

### Requirement: A limit on how many members one may block
A member SHALL be able to block at most 200 members at a time. A block beyond that SHALL answer 422
`validation_failed` with the field `blocks` and the code `too_many`, and SHALL store nothing. Repeating an
existing block SHALL NOT be refused for this reason.

#### Scenario: At the limit
- **WHEN** a member who has blocked 200 members sends a block for one more
- **THEN** the response is 422 with `blocks: too_many`

#### Scenario: Room again
- **WHEN** that member unblocks one member and sends the block again
- **THEN** the response is 204

#### Scenario: Repeating at the limit
- **WHEN** a member who has blocked 200 members sends the block for one of them again
- **THEN** the response is 204

#### Scenario: Concurrent blocks at the limit
- **WHEN** a member who has blocked 199 members sends blocks for two more at once
- **THEN** one answers 204 and the other 422, and the member has blocked 200

### Requirement: Block routes are for signed-in members only
The block routes SHALL require a valid access token and SHALL answer 401 without one, having done nothing.
Every response SHALL carry `Cache-Control: no-store`.

#### Scenario: No token
- **WHEN** a block, an unblock or a read of the list carries no access token, or an invalid or expired one
- **THEN** the response is 401, nothing is stored or removed, and nothing is returned

#### Scenario: Other methods
- **WHEN** a member sends POST or PATCH to `/v1/me/blocks/{id}`, or PUT, POST or DELETE to `/v1/me/blocks`
- **THEN** the response is 405 and nothing changes

### Requirement: Block writes are limited
Blocks and unblocks SHALL share a per-member limit that answers 429 `rate_limited` with `Retry-After` once
exceeded, having done nothing. The limit SHALL NOT affect the member's other requests or any other member.

#### Scenario: Over the limit
- **WHEN** a member blocks and unblocks faster than the limit allows
- **THEN** the next request answers 429 `rate_limited` with `Retry-After` and nothing changes

#### Scenario: Other requests are unaffected
- **WHEN** a member has exhausted the block limit
- **THEN** they can still read their list of blocked members, save their profile and send a report

#### Scenario: Unauthenticated requests spend nothing
- **WHEN** requests without a valid token are sent to a block route
- **THEN** they answer 401 and no member's limit is spent

### Requirement: Blocks and account deletion
A block SHALL be deleted when the account of either of its two members is deleted.

#### Scenario: The blocker's account is deleted
- **WHEN** A has blocked B and A's account is deleted
- **THEN** no block involving A exists

#### Scenario: The blocked member's account is deleted
- **WHEN** A has blocked B and B's account is deleted
- **THEN** B is no longer in A's list, and the block no longer counts toward A's limit

#### Scenario: An account deleted during the request
- **WHEN** the caller's account is deleted after the request was authenticated and before the block is stored
- **THEN** the response is 401 and nothing is stored

### Requirement: Blocks are never disclosed or logged
The backend SHALL NOT tell a member that they were blocked, by whom, or when, through any response. It SHALL
NOT write to any log who was blocked or unblocked, or a public identifier; a log line of a block or an unblock
SHALL hold only the account identifier of the member who made it.

#### Scenario: The blocked member is told nothing
- **WHEN** A blocks B
- **THEN** no response to any request by B differs from what B would get if A's profile did not exist

#### Scenario: No notification
- **WHEN** A blocks B, and again when A unblocks B
- **THEN** nothing is sent to B: no message, no email and no notification

#### Scenario: Logs name only the blocker
- **WHEN** a member blocks and unblocks another
- **THEN** the logs record that the member's blocks changed, and hold neither the other member's account
  identifier nor any public identifier or name

### Requirement: The block action in the app
The member profile screen SHALL offer "Block" when it shows the profile of a member other than the one using
the app, and SHALL NOT offer it on the member's own profile, while loading, on a failure or for an unavailable
profile. Activating it SHALL ask for confirmation, saying that neither member will see the other's profile and
that the other member is not told; nothing SHALL be sent before the member confirms.

#### Scenario: Another member's profile
- **WHEN** the screen shows another member's profile
- **THEN** a menu offers "Block" and "Report" as two separate items

#### Scenario: One's own profile
- **WHEN** the screen shows the profile of the member using the app
- **THEN** neither "Block" nor "Report" is offered

#### Scenario: Backing out
- **WHEN** the member activates "Block" and dismisses the confirmation
- **THEN** no request is sent and the profile is still shown

#### Scenario: Confirming
- **WHEN** the member confirms
- **THEN** one block request is sent for that profile's identifier

### Requirement: After a block in the app
After a successful block the screen SHALL stop showing the member's profile and SHALL say that the member is
blocked and SHALL show an "Unblock" control that undoes the block; the text SHALL NOT promise that the member
can be unblocked later from the list. A failed block SHALL leave the profile shown with the app's own
localized message. While the request is in flight the screen SHALL accept no
other action and SHALL ignore a second activation.

#### Scenario: Blocked
- **WHEN** the block request answers 204
- **THEN** the name, text, picture and languages are no longer shown, a text says the member is blocked and
  that "Unblock" undoes it, without saying they can be unblocked from "Blocked members", and an "Unblock"
  control is shown

#### Scenario: A failure that can be retried
- **WHEN** the block fails with a network failure, a timeout, 429, 503 or an unexpected response
- **THEN** the profile is still shown with the matching localized message, and the member can try again

#### Scenario: Too many blocked members
- **WHEN** the block answers 422 with `blocks: too_many`
- **THEN** a message says the limit is reached and that unblocking someone makes room

#### Scenario: The session ended
- **WHEN** the block fails because the session ended, with the confirmation open or closed
- **THEN** no error is shown and the app goes to the log in screen

#### Scenario: A double activation
- **WHEN** the member confirms twice before the first answer
- **THEN** one request is sent

### Requirement: The blocked members screen
The app SHALL show the members the user has blocked at `/blocked`, a signed-in route that names nobody, opened
from the home screen: each member's name with an "Unblock" control, most recently blocked first. It SHALL show
a labelled progress indicator while loading, a text when nobody is blocked, and the app's own localized message
with "Try again" when the load fails.

#### Scenario: With blocked members
- **WHEN** the screen opens for a member who has blocked two members
- **THEN** both names are shown, each with "Unblock"

#### Scenario: Nobody blocked
- **WHEN** the member has blocked nobody
- **THEN** one text says so and no "Unblock" is shown

#### Scenario: A failed load
- **WHEN** the load fails with a network failure, a timeout, 503 or an unexpected response
- **THEN** the matching localized message and "Try again" are shown, and activating it loads again

#### Scenario: Loaded each time
- **WHEN** the member leaves the screen and opens it again
- **THEN** the list is requested again

#### Scenario: Signed out
- **WHEN** a signed-out user navigates to `/blocked`
- **THEN** the app shows the log in screen

### Requirement: Unblocking in the app
"Unblock" SHALL ask for confirmation and then send one unblock for that member. On success the member SHALL
leave the list without a reload. On failure the member SHALL stay in the list with the app's own localized
message. While an unblock is in flight no other "Unblock" SHALL be accepted.

#### Scenario: Unblocking
- **WHEN** the member confirms "Unblock" for a member and the request answers 204
- **THEN** that member is no longer in the list and the others are unchanged

#### Scenario: The last one
- **WHEN** the member unblocks the only member in the list
- **THEN** the "nobody blocked" text is shown

#### Scenario: Backing out
- **WHEN** the member dismisses the confirmation
- **THEN** no request is sent and the list is unchanged

#### Scenario: A failed unblock
- **WHEN** the unblock fails with a network failure, a timeout, 429, 503 or an unexpected response
- **THEN** the member is still listed and the matching localized message is shown

### Requirement: Unblocking by identifier from the member screen
The "Unblock" control of the member screen's blocked state SHALL ask for confirmation and then send one unblock
for the route's identifier, without reading the list of blocked members. It is an immediate undo of the block
just made, not a way to unblock, later, a member who is absent from the list. On success the screen SHALL load
the profile again; on failure the blocked state SHALL stay. In flight, other actions SHALL be ignored.

#### Scenario: Unblocking the member just blocked
- **WHEN** the member confirms "Unblock" in the blocked state and the request answers 204
- **THEN** one unblock was sent for the route's identifier, no request for the list was sent, and the profile
  is loaded and shown again

#### Scenario: The profile cannot be read after the unblock
- **WHEN** the unblock answers 204 and the reload answers 404, as when the other member blocked the caller
  while the blocked state was shown
- **THEN** the screen shows the "unavailable" state of any identifier that cannot be read, with no "Unblock"

#### Scenario: Backing out
- **WHEN** the member dismisses the confirmation
- **THEN** no request is sent and the blocked state is unchanged

#### Scenario: A failed unblock
- **WHEN** the unblock fails with a network failure, a timeout, 429, 503 or an unexpected response
- **THEN** the blocked state and its "Unblock" are still shown with the matching localized message

#### Scenario: A double activation
- **WHEN** the member confirms twice before the first answer
- **THEN** one request is sent

#### Scenario: The session ended
- **WHEN** the unblock fails because the session ended, with the confirmation open or closed
- **THEN** no error is shown and the app goes to the log in screen

### Requirement: Privacy and accessibility of the blocking screens
The app SHALL print nothing about a block or a blocked member, SHALL put nothing but a profile's public
identifier in a route, and SHALL show no text from a server response other than a member's name. Every state,
the confirmations included, SHALL be usable with labelled controls, 48 dp tap targets and sufficient contrast
in both themes, at twice the normal text size on a 320 dp wide screen.

#### Scenario: Nothing printed
- **WHEN** a member is blocked, the list is loaded and a member is unblocked
- **THEN** the app prints nothing

#### Scenario: Large text on a small screen
- **WHEN** the block confirmation, the blocked state and a list of members with long names are shown at text
  scale 2.0 on a 320 by 480 dp screen
- **THEN** nothing overflows and every control can be reached

#### Scenario: Each control says whom it concerns
- **WHEN** a screen reader reads the list
- **THEN** each "Unblock" control is announced with the name of the member it unblocks

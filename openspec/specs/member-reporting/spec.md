# member-reporting Specification

## Purpose

Lets a signed-in member report another member for inappropriate behavior with a reason and optional details,
stores the report for the people who run the service, and guarantees that no member, the reported one above
all, can learn that a report exists or who made it.

## Requirements

### Requirement: Reporting a member
`PUT /v1/me/reports/{id}` SHALL store the signed-in member's report of the member whose profile has the public
identifier `id`, from a JSON body holding `reason` and, optionally, `details`, and answer 204 with no body. The
reporter SHALL be the member whose access token the request carries; nothing in the request SHALL be able to
name another member as the reporter.

#### Scenario: A report with a reason
- **WHEN** member A sends a report of member B with `{"reason":"spam"}`
- **THEN** the response is 204 and a report of B by A with that reason and no details is stored

#### Scenario: A report with details
- **WHEN** member A sends a report of B with a reason and a text in `details`
- **THEN** the response is 204 and the stored report holds that reason and that text

#### Scenario: The reporter is the session
- **WHEN** member A sends a report of B with member C's identifier in the query string
- **THEN** the stored report is A's, and C has reported nobody

#### Scenario: A body that names a reporter is refused
- **WHEN** the body carries any key other than `reason` and `details`
- **THEN** the response is 400 `invalid_request` and nothing is stored

### Requirement: Report reasons
`reason` SHALL be exactly one of `harassment`, `inappropriate_content`, `spam`, `impersonation` and `other`,
written in lower case with nothing added. A missing, empty or `null` reason SHALL answer 422
`validation_failed` with `reason: required`, and any other value with `reason: invalid`. A refused report
SHALL store nothing and SHALL leave an earlier report unchanged.

#### Scenario: Each reason
- **WHEN** a member sends a report with each of the five reasons
- **THEN** each response is 204

#### Scenario: No reason
- **WHEN** the body has no `reason`, or it is `""` or `null`
- **THEN** the response is 422 with `reason: required`

#### Scenario: An unknown reason
- **WHEN** `reason` is `"Spam"`, `" spam"`, `"rude"` or a number
- **THEN** the response is 422 with `reason: invalid`, or 400 `invalid_request` when the value is not a string

#### Scenario: A refused report keeps the earlier one
- **WHEN** a member who has reported B sends a report of B that is refused
- **THEN** the stored report is the earlier one

### Requirement: Report details
`details` SHALL be optional for every reason; absent, `null` and empty SHALL mean none. Stored details SHALL be
the text with line breaks unified, tabs as spaces and surrounding whitespace removed, of at most 1000
characters. A longer text SHALL answer 422 with `details: too_long`, and one holding any other control
character, or bytes that are not valid text, with `details: invalid`. Every failing field SHALL be reported
together.

#### Scenario: No details
- **WHEN** a report is sent with `details` absent, `null`, `""` or only spaces
- **THEN** the response is 204 and the stored report has no details

#### Scenario: At and over the limit
- **WHEN** `details` is 1000 characters long, and again when it is 1001
- **THEN** the first response is 204 and the second is 422 with `details: too_long`

#### Scenario: A long paste gets a field error
- **WHEN** `details` is several thousand characters long
- **THEN** the response is 422 with `details: too_long`, not 400

#### Scenario: Control characters
- **WHEN** `details` holds a NUL or another control character that is not a line break or a tab
- **THEN** the response is 422 with `details: invalid`

#### Scenario: Line breaks and tabs
- **WHEN** `details` holds several lines separated by CRLF, with a tab in one of them
- **THEN** the response is 204, and the stored text has LF line breaks and a space in place of the tab

#### Scenario: Both fields wrong
- **WHEN** the reason is unknown and the details are too long
- **THEN** the response is 422 naming both `reason` and `details`

### Requirement: One report per reporter and reported member
There SHALL be at most one stored report by one member about another. Sending a report with the content
already stored SHALL change nothing and answer as the first did, so a client can safely repeat a report whose
answer it did not receive. Sending a different reason or different details SHALL replace the stored report's
content. Reports by different members about the same member SHALL each be stored.

#### Scenario: The same report twice
- **WHEN** member A reports B and then sends the same reason and details again
- **THEN** both responses are 204, one report exists, and nothing was rewritten the second time

#### Scenario: A later report replaces the earlier one
- **WHEN** member A reports B for `spam` and later for `harassment` with details
- **THEN** one report of B by A exists, with `harassment` and those details

#### Scenario: Several reporters
- **WHEN** members A and C each report B
- **THEN** two reports of B exist, one by each

#### Scenario: Concurrent reports by one member
- **WHEN** member A sends several different reports of B at once
- **THEN** every response is 204 and one report exists, holding the reason and the details of one of the
  requests together

### Requirement: A member cannot report themselves
A report whose identifier is the caller's own profile SHALL answer 422 `validation_failed` with the field
`member` and the code `self`, and SHALL store nothing.

#### Scenario: Reporting oneself
- **WHEN** a member sends a valid report for their own profile identifier
- **THEN** the response is 422 with `member: self`, and no report is stored

#### Scenario: Nothing can store it
- **WHEN** anything tries to store a report whose reporter and reported member are the same account
- **THEN** the database refuses it

### Requirement: A report does not reveal whether a profile exists
A valid report for an identifier that names no profile, well formed or not, SHALL answer 204 and store nothing,
exactly as a report of an existing member answers. What is wrong with the body SHALL be answered the same
whatever the identifier names. A block between the two members, in either direction, SHALL NOT prevent a
report.

#### Scenario: Unknown or malformed identifier
- **WHEN** a member sends a valid report for an identifier that no profile has, or for `not-an-id`
- **THEN** the response is 204 and no report is stored

#### Scenario: An invalid body for an unknown identifier
- **WHEN** a member sends a report with an unknown reason for an identifier that names no profile
- **THEN** the response is 422 with `reason: invalid`, as for an existing member

#### Scenario: Reporting a member who blocked the reporter
- **WHEN** B has blocked A, and A sends a valid report for B's identifier
- **THEN** the response is 204 and the report is stored

#### Scenario: Reporting a member one has blocked
- **WHEN** A has blocked B and sends a valid report for B's identifier
- **THEN** the response is 204 and the report is stored

### Requirement: What a report holds
A stored report SHALL hold only who reported, who was reported, the reason, the details, and when it was made
and last changed. It SHALL NOT hold a copy of the reported member's name, text, languages or picture, so what
a reviewer sees of the profile is the profile as it is when they look.

#### Scenario: Nothing of the profile is stored
- **WHEN** member A reports member B, who has a name, a text and a picture
- **THEN** the stored report holds none of B's name, text or picture

#### Scenario: The profile changed after the report
- **WHEN** B changes their name and text after A's report
- **THEN** the report is unchanged and holds neither the earlier nor the new name and text

### Requirement: Reports are never returned
No route SHALL return a report, a count of reports, or whether a report exists, to any member: not to the
reported member, not to the reporter, and not to anyone else. A report SHALL change nothing that the reported
member or any other member can request.

#### Scenario: The reported member sees nothing
- **WHEN** A reports B
- **THEN** every response to B's requests is what it would be if no report existed

#### Scenario: Other members see nothing
- **WHEN** A reports B, and member C reads B's profile
- **THEN** the response is the public profile, with no field about reports

#### Scenario: No way to read a report
- **WHEN** a member sends GET to `/v1/me/reports`, or GET, POST, PATCH or DELETE to `/v1/me/reports/{id}`
- **THEN** the response is 404 or 405, and no report is returned or changed

### Requirement: Report routes are for signed-in members only
The report route SHALL require a valid access token and SHALL answer 401 without one, having stored nothing.
Every response SHALL carry `Cache-Control: no-store`.

#### Scenario: No token
- **WHEN** a report carries no access token, or an invalid or expired one
- **THEN** the response is 401 and nothing is stored, for an existing and an unknown identifier alike

### Requirement: Reports are limited
Reports SHALL have a per-member limit that answers 429 `rate_limited` with `Retry-After` once exceeded, having
stored nothing. Refused and repeated reports SHALL count. The limit SHALL NOT affect the member's other
requests or any other member.

#### Scenario: Over the limit
- **WHEN** a member sends reports faster than the limit allows
- **THEN** the next one answers 429 `rate_limited` with `Retry-After` and is not stored

#### Scenario: Refused reports count
- **WHEN** a member repeatedly sends reports that are refused as invalid
- **THEN** they are limited as valid reports are

#### Scenario: Other requests are unaffected
- **WHEN** a member has exhausted the report limit
- **THEN** they can still block a member and save their profile

### Requirement: Reports and account deletion
When the reported member's account is deleted, every report about them SHALL be deleted. When a reporter's
account is deleted, their reports SHALL stay, about the same member and with the same content, and SHALL no
longer identify any reporter.

#### Scenario: The reported member's account is deleted
- **WHEN** A and C have reported B, and B's account is deleted
- **THEN** no report about B exists

#### Scenario: The reporter's account is deleted
- **WHEN** A has reported B, and A's account is deleted
- **THEN** the report about B still exists with its reason and details, and names no reporter

#### Scenario: An account deleted during the request
- **WHEN** the caller's account is deleted after the request was authenticated and before the report is stored
- **THEN** the response is 401 and nothing is stored

### Requirement: Reports are never logged
The backend SHALL NOT write a report's reason or details, the reported member's account identifier, or any
public identifier to any log. A log line of a stored report SHALL hold only the reporter's account identifier,
and SHALL be written only when a report was stored or changed.

#### Scenario: A report is logged without its content
- **WHEN** a member reports another with details
- **THEN** the logs record that the member sent a report, and hold neither the reason, the details, nor
  anything that names the reported member

#### Scenario: A repeat logs nothing
- **WHEN** a member sends the report that is already stored, or one for an identifier that names no profile
- **THEN** no log line is written

### Requirement: The report flow in the app
"Report" on the member profile screen SHALL open a report screen at `/members/:id/report`, a signed-in route
whose only parameter is a well-formed public identifier. The screen SHALL show the five reasons with none
chosen, an optional details field, and a notice that the report is private and the member is not told who
reported them. It SHALL request nothing when it opens.

#### Scenario: Opening the report screen
- **WHEN** the member activates "Report" on another member's profile
- **THEN** the report screen opens with no reason chosen, an empty details field and the notice, and no request
  is sent

#### Scenario: Not offered on one's own profile
- **WHEN** the member profile screen shows the profile of the member using the app
- **THEN** "Report" is not offered

#### Scenario: A malformed identifier is not a route
- **WHEN** a signed-in user navigates to `/members/abc/report` or `/members/<id>/report/x`
- **THEN** the app shows the home screen and sends no request

#### Scenario: Signed out
- **WHEN** a signed-out user navigates to `/members/` followed by a well-formed identifier and `/report`
- **THEN** the app shows the log in screen

### Requirement: Sending a report from the app
Sending SHALL require a chosen reason, the app's only check, and SHALL send one request holding the reason and
the details exactly as typed. On success the screen SHALL replace the form with a confirmation that says the
report was sent and that the member can also be blocked from their profile, with a control that returns to the
profile. The app SHALL NOT block the member by itself.

#### Scenario: No reason chosen
- **WHEN** the member activates "Send report" with no reason chosen
- **THEN** a text asks them to choose a reason and no request is sent

#### Scenario: Sending
- **WHEN** the member chooses a reason, types details and sends
- **THEN** one request is sent with that reason and the text as typed

#### Scenario: Sent
- **WHEN** the request answers 204
- **THEN** the form is replaced by the confirmation, and its control returns to the member's profile

#### Scenario: Reporting does not block
- **WHEN** a report is sent successfully
- **THEN** no block request is sent, the server has stored no block, and the member's profile is still shown on
  return

### Requirement: Report failures and a report in flight
A failed report SHALL keep the chosen reason and the typed details and show the app's own localized message;
a refused field SHALL be explained beside it. While a report is in flight every control SHALL be disabled,
leaving SHALL be held back, and a second activation SHALL be ignored.

#### Scenario: Details too long
- **WHEN** the server answers 422 with `details: too_long`
- **THEN** a message under the details field says the text is too long, and the text is kept

#### Scenario: A failure that can be retried
- **WHEN** the report fails with a network failure, a timeout, 429, 503 or an unexpected response
- **THEN** the matching localized message is shown, the form keeps its content, and sending again is possible

#### Scenario: The session ended
- **WHEN** the report fails because the session ended
- **THEN** no error is shown and the app goes to the log in screen

#### Scenario: A double activation
- **WHEN** the member activates "Send report" twice before the first answer
- **THEN** one request is sent

### Requirement: Privacy and accessibility of the report screen
The app SHALL print nothing about a report, SHALL put nothing but the public identifier in the route, and SHALL
show no text taken from a server response. Every state SHALL be usable with labelled controls, 48 dp tap
targets and sufficient contrast in both themes, at twice the normal text size on a 320 dp wide screen with the
keyboard open.

#### Scenario: Nothing printed
- **WHEN** a report is typed, fails and is sent
- **THEN** the app prints nothing, and the reason and the details appear only in the body of the request

#### Scenario: Large text on a small screen
- **WHEN** the form, a failure and the confirmation are shown at text scale 2.0 on a 320 by 480 dp screen with
  the keyboard open
- **THEN** nothing overflows and every reason and control can be reached

#### Scenario: The reasons are one choice
- **WHEN** a screen reader reads the form
- **THEN** each reason is announced as one option of a single choice, with whether it is chosen

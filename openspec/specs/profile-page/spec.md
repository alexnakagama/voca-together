# profile-page Specification

## Purpose

Gives a signed-in member a read-only page that shows their profile as their social identity in the app: their
picture, name, text and languages, with the ways to edit it and to see it as other members do.

## Requirements

### Requirement: The profile page is read-only
The page at `/profile` SHALL show the signed-in member's picture, name, text, a Friends area and languages,
and an "Edit Profile" control. It SHALL contain no text field and no control that changes the profile, the
picture or the languages directly.

#### Scenario: A complete profile
- **WHEN** a member with a picture, a name, a text and languages opens `/profile`
- **THEN** the page shows all of them and an "Edit Profile" control, and no text field

#### Scenario: No text
- **WHEN** the member's profile has a name and no text
- **THEN** the page shows the name and leaves no empty space or label for the text

#### Scenario: What is shown is what is stored
- **WHEN** the page has loaded
- **THEN** the name and the text are the ones the server returned, not anything typed and not saved

### Requirement: The picture on the profile page
The page SHALL show the member's picture when they have one, and otherwise a placeholder built from the first
character of their name. A picture that fails to load SHALL be replaced by the placeholder without an error
message, and SHALL NOT keep the rest of the page from showing.

#### Scenario: With a picture
- **WHEN** the member has a picture
- **THEN** it is shown, round, beside or above the name

#### Scenario: Without a picture
- **WHEN** the member has no picture
- **THEN** the placeholder shows the first character of the name

#### Scenario: The picture request fails
- **WHEN** the profile loads and the picture request fails
- **THEN** the placeholder is shown, the name, text and languages are shown, and no error is shown for the
  picture

### Requirement: The Friends placeholder
The page SHALL show a Friends area made of a heading and one line saying the feature is coming later. The area
SHALL show no number and no member, SHALL NOT be a control, and SHALL cause no request.

#### Scenario: What it shows
- **WHEN** the profile page has loaded
- **THEN** it shows the heading "Friends" and a "Coming later" line, with no count

#### Scenario: Not interactive
- **WHEN** the member taps the Friends area
- **THEN** nothing happens and the location stays `/profile`

### Requirement: Languages on the profile page
The page SHALL show the member's "I speak" and "I'm learning" languages with their levels, in the member's
order, read-only. The languages SHALL load and fail on their own, with their own error and retry, without
affecting the rest of the page. The page SHALL offer no control that opens the languages editor.

#### Scenario: Both lists
- **WHEN** the member has languages in both lists
- **THEN** each list is shown under its heading with every language's name and level

#### Scenario: None chosen
- **WHEN** the member has chosen no language
- **THEN** one text says so and neither heading is shown

#### Scenario: The languages fail to load
- **WHEN** the profile loads and a languages request fails
- **THEN** the languages area shows its error and "Try again", and the name, text, picture and "Edit Profile"
  are shown normally

#### Scenario: No shortcut to the editor
- **WHEN** the profile page is shown in any state
- **THEN** it has no "Edit languages" control

### Requirement: A member without a profile
When the member has saved no profile, the page SHALL say so and offer a control that opens the edit screen to
create one. It SHALL show no name, no Friends area, no languages and no "See public profile" action.

#### Scenario: Nothing saved yet
- **WHEN** a member who has saved no profile opens `/profile`
- **THEN** the page invites them to create their profile and shows a control that opens `/profile/edit`

#### Scenario: After creating one
- **WHEN** that member saves a profile on the edit screen and returns
- **THEN** the page shows the new profile

### Requirement: Opening the edit screen and showing the result
"Edit Profile" SHALL open `/profile/edit` on top of the page. Each time the member comes back from it, by
saving, cancelling or going back, the page SHALL load the profile, the picture and the languages again and
show what the server has stored.

#### Scenario: After a save
- **WHEN** the member changes their name on the edit screen, saves and is returned to the page
- **THEN** the page shows the new name without being reopened

#### Scenario: After changing the picture
- **WHEN** the member sets or removes a picture on the edit screen and goes back
- **THEN** the page shows the new picture, or the placeholder

#### Scenario: After editing languages
- **WHEN** the member saves a new selection in the languages editor, returns to the edit screen and then to
  the page
- **THEN** the page shows the new selection

#### Scenario: After leaving without saving
- **WHEN** the member leaves the edit screen without saving
- **THEN** the page loads again and shows the stored profile

#### Scenario: The reload fails
- **WHEN** the load after returning fails
- **THEN** the page shows the failure and "Try again", and not the profile it showed before

### Requirement: Seeing one's public profile
When the member has a profile, the page SHALL offer a "See public profile" action that opens the member
profile screen for the member's own public identifier.

#### Scenario: Opening it
- **WHEN** the member activates "See public profile"
- **THEN** the app shows `/members/` followed by their public identifier, with their name, text, picture and
  languages and no way to edit

#### Scenario: Going back
- **WHEN** the member goes back from their public profile
- **THEN** the profile page is shown again

### Requirement: Loading and failure of the profile page
The page SHALL show a labelled progress indicator until the profile request answers. If it fails, the page
SHALL show the app's own localized message with "Try again" and nothing of the profile.

#### Scenario: The profile fails to load
- **WHEN** the profile request fails with a network failure, a timeout, 429, 503 or an unexpected response
- **THEN** the matching localized message and "Try again" are shown, and no name, Friends area or languages

#### Scenario: Retry
- **WHEN** the member activates "Try again"
- **THEN** the progress indicator is shown and the profile is requested again

#### Scenario: The session ended
- **WHEN** a request fails because the session ended
- **THEN** no error is shown and the app goes to the log in screen

#### Scenario: A late answer
- **WHEN** an answer arrives after the member left the page or after a newer load started
- **THEN** it is ignored

### Requirement: The profile route is the caller's own
`/profile` SHALL be a signed-in route that names no member and carries no token, email, name or identifier.
Access SHALL be decided only by the session status and the path.

#### Scenario: Signed out
- **WHEN** a signed-out user navigates to `/profile`
- **THEN** the app shows the log in screen

#### Scenario: Nothing personal in the location
- **WHEN** the member opens their profile page
- **THEN** the app's location is exactly `/profile`

### Requirement: Privacy and accessibility of the profile page
The app SHALL print nothing about the profile, and SHALL show no text from a server response other than the
profile's own fields and catalog names. Every state SHALL be usable with labelled controls, 48 dp tap targets
and sufficient contrast in both themes, at twice the normal text size on a 320 dp wide screen.

#### Scenario: Nothing printed
- **WHEN** the page loads, fails and reloads
- **THEN** the app prints nothing

#### Scenario: Large text on a small screen
- **WHEN** the page is shown at text scale 2.0 on a 320 by 480 dp screen with a long name and a long text
- **THEN** nothing overflows and "Edit Profile" can be scrolled to and activated

#### Scenario: Read as a profile
- **WHEN** a screen reader reads the page
- **THEN** the picture or placeholder is announced as the profile picture, and the name and each section
  heading are announced as headings

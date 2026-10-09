# profile-editing Specification

## Purpose

Lets a signed-in member create and change their own profile on a dedicated edit screen: their name and text,
their picture, and the way in to the languages editor, without losing unsaved work by accident.

## Requirements

### Requirement: The edit route is the caller's own
The edit screen SHALL live at the signed-in route `/profile/edit`. The route SHALL name no member and carry no
token, email, name or identifier. Access SHALL be decided only by the session status and the path.

#### Scenario: Signed out
- **WHEN** a signed-out user navigates to `/profile/edit`
- **THEN** the app shows the log in screen

#### Scenario: Anything below the route is not a route
- **WHEN** a signed-in user navigates to `/profile/edit/` or `/profile/edit/name`
- **THEN** the app shows the home screen

#### Scenario: Nothing personal in the location
- **WHEN** the member edits, fails to save and saves
- **THEN** the app's location stays exactly `/profile/edit` until the screen is left

### Requirement: Loading the form
On opening, the screen SHALL request the member's profile and show a labelled progress indicator until it
answers. A member with a profile SHALL get the form filled with the stored name and text; a member without one
SHALL get the empty form to create it. A failed load SHALL show the localized message with "Try again" and no
form.

#### Scenario: Editing
- **WHEN** a member with a profile opens the screen
- **THEN** the name and text fields hold what is stored, under an "edit" heading

#### Scenario: Creating
- **WHEN** a member with no profile opens the screen
- **THEN** both fields are empty, under a "create" heading

#### Scenario: A response that is not "no profile"
- **WHEN** the profile request answers 404 without the code `profile_not_found`
- **THEN** the screen shows an error with "Try again" and never an empty form

#### Scenario: The session ended
- **WHEN** the load fails because the session ended
- **THEN** no error is shown and the app goes to the log in screen

### Requirement: Telling the member what others will see
The form SHALL show, before anything is typed, a notice that other members will be able to see the name, the
text and the picture, and that the email address stays private.

#### Scenario: The notice precedes the save
- **WHEN** the form is shown, to create or to edit
- **THEN** the notice is visible above the fields

### Requirement: Saving the name and the text
Save SHALL send the name and the text exactly as typed, in one request that replaces both. The only check
made by the app SHALL be that the name is not empty. On success the screen SHALL close and return to the
profile page.

#### Scenario: Success
- **WHEN** the member changes the text, saves, and the server answers 200
- **THEN** the screen closes without asking about unsaved changes and the profile page is shown

#### Scenario: Empty name
- **WHEN** the member activates Save with a name that is empty or only spaces
- **THEN** no request is sent, the name field shows that it is required and takes the focus

#### Scenario: Sent as typed
- **WHEN** the member saves a name with surrounding spaces and a very long text
- **THEN** the request holds both exactly as typed, with no trimming and no length check by the app

#### Scenario: Clearing the text
- **WHEN** the member empties the text field and saves
- **THEN** the request is sent and the saved profile has no text

#### Scenario: Saving without a change
- **WHEN** the member activates Save having changed nothing
- **THEN** the request is sent and, on success, the screen returns to the profile page

### Requirement: Profile save failures
A failed save SHALL keep the typed name and text and leave Save available. Errors the server reports on a
field SHALL be shown on that field, and the first such field SHALL take the focus; every other failure SHALL
be one message above the form. Every message SHALL be the app's own localized text. Editing a field SHALL
clear its error.

#### Scenario: A field error
- **WHEN** the server answers 422 with `too_long` on `bio`
- **THEN** the text field shows the "too long" message and keeps what was typed, and the screen stays open

#### Scenario: Errors on both fields
- **WHEN** the server answers 422 with an error on `display_name` and one on `bio`
- **THEN** each field shows its own error and the name field takes the focus

#### Scenario: Rate limited, unavailable, offline, timed out or unexpected
- **WHEN** the save fails with 429, 503, a network failure, a timeout or any other failure
- **THEN** the matching localized message is shown above the form and the typed text is unchanged

#### Scenario: The session ended during the save
- **WHEN** the save fails because the session ended
- **THEN** no error is shown and the app goes to the log in screen

### Requirement: A profile save in flight
While a save is in flight the Save control SHALL show that it is working, the fields and every other control
of the screen SHALL be disabled, a second save SHALL NOT be sent, and attempts to leave SHALL be ignored.

#### Scenario: Double activation
- **WHEN** the member activates Save twice before the server answers
- **THEN** exactly one save request is sent

#### Scenario: Back during a save
- **WHEN** the member presses back while the save is in flight
- **THEN** the screen stays open until the save answers

### Requirement: Cancelling and unsaved changes
The screen SHALL offer a Cancel control, and Cancel, the app bar's back control and the system back action
SHALL behave alike. When the name and the text equal what was loaded they SHALL close the screen at once.
Otherwise they SHALL ask the member to confirm discarding the changes and close only on confirmation. Leaving
SHALL send no save. A change of picture is never an unsaved change.

#### Scenario: Leaving with nothing changed
- **WHEN** the member has changed neither field and activates Cancel or goes back
- **THEN** the screen closes with no question and no save request

#### Scenario: Leaving with changes asks first
- **WHEN** the member has changed the name or the text and activates Cancel or goes back
- **THEN** a dialog asks whether to discard the changes, with "Keep editing" and "Discard"

#### Scenario: Typing it back
- **WHEN** the member changes the name and then types the loaded name again
- **THEN** leaving closes the screen with no question

#### Scenario: Keep editing
- **WHEN** the member chooses "Keep editing", or dismisses the dialog
- **THEN** the screen stays open with the text as typed

#### Scenario: Discard
- **WHEN** the member chooses "Discard"
- **THEN** the screen closes, no save request is sent, and the profile page shows the stored profile

#### Scenario: After only changing the picture
- **WHEN** the member has set or removed a picture and changed neither field
- **THEN** leaving closes the screen with no question

#### Scenario: The session ends with unsaved changes
- **WHEN** the session ends while the form has unsaved changes, with or without the dialog open
- **THEN** the app goes to the log in screen without asking, and nothing of the screen stays visible

### Requirement: The way in to the languages editor
The screen SHALL offer a "Languages" control that opens the languages editor on top of it, so that going back
returns to the edit screen with the name and text as the member left them. The control SHALL be available
whenever the form is. The edit screen SHALL request and show no language itself.

#### Scenario: Opening the editor
- **WHEN** the form is shown and the member activates "Languages"
- **THEN** the languages editor opens at `/profile/languages`

#### Scenario: Unsaved text survives the visit
- **WHEN** the member types in the form without saving, opens the languages editor and comes back, having saved
  languages or not
- **THEN** the form still holds the typed text and no profile save was sent

#### Scenario: Before a profile exists
- **WHEN** a member with no profile opens the edit screen
- **THEN** the "Languages" control is available

#### Scenario: Not while loading or failed
- **WHEN** the edit screen is loading or shows a load error
- **THEN** no "Languages" control is shown

#### Scenario: No languages requested here
- **WHEN** the edit screen opens, saves and closes
- **THEN** it sends no catalog request and no languages request

### Requirement: The picture control
The screen SHALL show the member's current picture, or the placeholder, with a control to choose a picture
from the device's photos and, when there is a picture, a control to remove it. The picture SHALL load on its
own: while it loads or if it fails to load, the form SHALL remain usable.

#### Scenario: With a picture
- **WHEN** the member has a picture
- **THEN** it is shown with "Change photo" and "Remove photo"

#### Scenario: Without a picture
- **WHEN** the member has no picture
- **THEN** the placeholder is shown with "Add photo", and no "Remove photo"

#### Scenario: The picture fails to load
- **WHEN** the picture request fails
- **THEN** the placeholder is shown with the control to choose a picture, and the form can be edited and saved

#### Scenario: Before a profile exists
- **WHEN** a member with no profile opens the edit screen
- **THEN** the picture control is available

### Requirement: Choosing a picture applies it at once
Choosing a photo SHALL upload it immediately as the member's picture, independently of Save, and on success
SHALL show the picture as the server stored it. Closing the photo chooser without choosing SHALL change
nothing. The app SHALL apply no rule of its own about the photo's format, size or dimensions.

#### Scenario: Choosing a photo
- **WHEN** the member chooses a photo and the upload succeeds
- **THEN** the control shows the stored picture and "Remove photo" becomes available, without Save

#### Scenario: Backing out of the chooser
- **WHEN** the member opens the photo chooser and closes it without choosing
- **THEN** no request is sent and the picture is unchanged

#### Scenario: The text is untouched
- **WHEN** the member has typed in the form and then chooses a photo
- **THEN** the typed text is kept and no profile save is sent

#### Scenario: No permission is asked
- **WHEN** the member opens the photo chooser for the first time
- **THEN** the app asks for no storage, media or camera permission

### Requirement: Removing the picture
"Remove photo" SHALL ask the member to confirm, and on confirmation SHALL remove the picture immediately,
independently of Save, and show the placeholder.

#### Scenario: Confirmed
- **WHEN** the member activates "Remove photo" and confirms
- **THEN** the removal is sent, the placeholder is shown and "Remove photo" is no longer offered

#### Scenario: Not confirmed
- **WHEN** the member activates "Remove photo" and dismisses the question
- **THEN** no request is sent and the picture stays

### Requirement: Picture failures
A failed upload or removal SHALL leave the picture shown as it was before, and SHALL show the app's own
localized message beside the picture control, never text from the server. The next picture action SHALL clear
the message. A picture failure SHALL NOT change or block the form.

#### Scenario: The photo is refused
- **WHEN** the server answers the upload with 422 and `unsupported_type`, `invalid_image`, `too_large` or
  `dimensions_too_large` on `avatar`
- **THEN** the matching message is shown by the picture control and the earlier picture or placeholder stays

#### Scenario: Rate limited, unavailable, offline or timed out
- **WHEN** the upload or the removal fails with 429, 503, a network failure or a timeout
- **THEN** the matching localized message is shown by the picture control, and the form can still be saved

#### Scenario: The photo cannot be read from the device
- **WHEN** the chosen photo cannot be read
- **THEN** a message says the photo could not be used and no request is sent

#### Scenario: The session ended
- **WHEN** an upload or a removal fails because the session ended
- **THEN** no error is shown and the app goes to the log in screen

### Requirement: A picture action in flight
While an upload or a removal is in flight the picture control SHALL show that it is working, every control of
the screen SHALL be disabled, no second picture request SHALL be sent, and attempts to leave SHALL be ignored.

#### Scenario: Controls while uploading
- **WHEN** an upload has been sent and has not answered
- **THEN** Save, Cancel, "Languages" and the picture controls are disabled

#### Scenario: Back during an upload
- **WHEN** the member presses back while the upload is in flight
- **THEN** the screen stays open until the upload answers

### Requirement: Privacy of the edit screen
The app SHALL NOT write the name, the text or any image data to a log or console output, SHALL NOT place them
in a route, and SHALL send them only in the bodies of the member's own save and upload requests. The screen
SHALL show no token, no email address and no text taken from a server response.

#### Scenario: Nothing printed
- **WHEN** the member loads, edits, uploads, fails to save and saves
- **THEN** the app prints nothing

#### Scenario: Server text is never shown
- **WHEN** a failed save's or a failed upload's response body contains arbitrary text
- **THEN** none of that text appears on screen

### Requirement: Accessibility of the edit screen
Every control of the screen and of its dialogs SHALL have a text label that names what it acts on, a tap
target of at least 48 by 48 dp, and sufficient contrast in the light and the dark theme. Every state SHALL
remain usable, with every control reachable and nothing overflowing, at twice the normal text size on a 320
dp wide screen, also with the keyboard open.

#### Scenario: Large text on a small screen
- **WHEN** the form is shown at text scale 2.0 on a 320 by 480 dp screen
- **THEN** nothing overflows and the picture controls, "Languages", Save and Cancel can be scrolled to and
  activated

#### Scenario: Errors are announced
- **WHEN** a save or a picture action fails
- **THEN** the message is announced to assistive technology as the other screens' error banners are

#### Scenario: The picture is described
- **WHEN** a screen reader reads the picture control
- **THEN** it announces whether there is a profile picture, and each picture control by what it does

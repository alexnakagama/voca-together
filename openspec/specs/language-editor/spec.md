# language-editor Specification

## Purpose

Lets a signed-in member of the mobile app change the languages they speak and the languages they are learning,
with a level for each and in the order they choose, and see the result on their profile.

## Requirements

### Requirement: Reaching the editor from the profile edit screen
The profile edit screen at `/profile/edit` SHALL offer a "Languages" control whenever it shows its form,
including for a member who has chosen no language or saved no profile. The control SHALL open the language
editor on top of the edit screen, so that going back returns to it with the profile form as the member left
it. The profile page at `/profile` SHALL offer no control that opens the editor.

#### Scenario: Opening the editor with languages chosen
- **WHEN** the edit screen shows its form, the member has languages, and the member activates "Languages"
- **THEN** the language editor opens at `/profile/languages` with the member's languages

#### Scenario: Opening the editor with no language chosen
- **WHEN** the member has chosen no language and activates "Languages" on the edit screen
- **THEN** the language editor opens with both lists empty

#### Scenario: No way in while the edit screen is loading or failed
- **WHEN** the edit screen is loading, or shows a load error with its retry
- **THEN** no "Languages" control is shown

#### Scenario: Unsaved profile text survives the visit
- **WHEN** the member types in the profile form without saving, opens the editor and comes back
- **THEN** the profile form still holds the typed text and no profile save was sent

#### Scenario: No way in from the profile page
- **WHEN** the profile page shows the member's languages, or that none is chosen
- **THEN** it shows no control that opens the editor

### Requirement: The editor route is the caller's own
The editor SHALL live at the signed-in route `/profile/languages`. The route SHALL name no member and carry no
language code, level, token or email, in its path or query. Access SHALL be decided only by the session status
and the path, as for every other route.

#### Scenario: Signed out
- **WHEN** a signed-out user navigates to `/profile/languages`
- **THEN** the app shows the log in screen

#### Scenario: Anything below the route is not a route
- **WHEN** a signed-in user navigates to `/profile/languages/es` or `/profile/languages/`
- **THEN** the app shows the home screen

#### Scenario: Nothing personal in the location
- **WHEN** the member adds, changes, reorders, removes and saves languages
- **THEN** the app's location stays exactly `/profile/languages` until the editor is left

### Requirement: Loading the editor
On opening, the editor SHALL request the language catalog and the member's current selection, once each, and
show a labelled progress indicator until both have answered. The load SHALL succeed or fail as a whole.

#### Scenario: Loaded
- **WHEN** both requests succeed
- **THEN** the editor shows the visibility notice, the "I speak" list and the "I'm learning" list with the
  member's languages in their stored order, each with its name, its own name and its level

#### Scenario: Either request fails
- **WHEN** the catalog request or the selection request fails
- **THEN** the editor shows the localized error and a "Try again" control, and shows no list, no "add" control
  and no Save control

#### Scenario: Retry
- **WHEN** the member activates "Try again" after a failed load
- **THEN** the progress indicator is shown and both requests are sent again

#### Scenario: The session ended during the load
- **WHEN** the load fails because the session ended
- **THEN** the editor shows no error and the app goes to the log in screen

#### Scenario: A late answer
- **WHEN** an answer arrives after the member left the editor or after a newer load started
- **THEN** it is ignored

### Requirement: A language the catalog does not name stays in the selection
If the member's selection holds a language code the catalog does not list, the editor SHALL show that entry
with the code in place of the name, SHALL let the member change its level, move it and remove it, and SHALL
include it unchanged in a save unless the member removed it.

#### Scenario: An unnamed code is shown and saved
- **WHEN** the selection holds a code absent from the catalog and the member saves after another change
- **THEN** the entry was shown by its code and the saved selection still contains it at its level and position

### Requirement: Adding a language
Each list SHALL have an "Add a language" control that opens a picker over the whole catalog, showing each
language's name and own name. The picker SHALL have a search field that filters by name, own name and code,
ignoring letter case, and SHALL leave out every language already in either list. Choosing a language SHALL ask
for its level, and choosing the level SHALL add the language at the end of that list.

#### Scenario: Adding to an empty list
- **WHEN** the member activates "Add a language" under "I'm learning", chooses Japanese and then level A2
- **THEN** "I'm learning" shows Japanese at A2 as its only entry

#### Scenario: Adding goes last
- **WHEN** "I speak" holds Spanish and the member adds English at C1
- **THEN** "I speak" shows Spanish, then English

#### Scenario: Search
- **WHEN** the member types "ESP" in the picker's search field
- **THEN** the picker lists Spanish (own name "Español") and no language that matches in none of name, own name
  and code

#### Scenario: No match
- **WHEN** the search text matches no available language
- **THEN** the picker shows a text saying nothing matches, and no language

#### Scenario: Chosen languages are not offered
- **WHEN** Spanish is in "I speak" and the member opens the picker under either list
- **THEN** Spanish is not offered

#### Scenario: Backing out of the picker
- **WHEN** the member closes the picker, or closes the level choice after picking a language
- **THEN** no language is added and the lists are unchanged

#### Scenario: A large catalog
- **WHEN** the catalog holds more than a hundred languages
- **THEN** the picker scrolls through all of them, also with the keyboard open

### Requirement: Choosing a level
The level choice SHALL list the levels in the order of the scale, each with its short name and a one-line
description, and mark the entry's current level when changing one. Under "I speak" it SHALL offer A1, A2, B1,
B2, C1, C2 and Native; under "I'm learning" it SHALL offer A1 to C2 and SHALL NOT offer Native. Each entry's
level control SHALL reopen the level choice for that entry.

#### Scenario: Changing a level
- **WHEN** Spanish is at B1 under "I speak", the member activates its level control and chooses Native
- **THEN** Spanish shows Native and keeps its position

#### Scenario: Native is not offered for a language being learned
- **WHEN** the member opens the level choice for an entry of "I'm learning"
- **THEN** the choices are A1, A2, B1, B2, C1 and C2

#### Scenario: Closing the level choice
- **WHEN** the member closes the level choice of an existing entry without choosing
- **THEN** the entry keeps its level

### Requirement: Ordering a list
Each entry SHALL have a "move up" and a "move down" control that swaps it with its neighbour in the same list.
The first entry's "move up" and the last entry's "move down" SHALL be disabled. The order shown SHALL be the
order saved. An entry SHALL NOT move between the two lists.

#### Scenario: Moving up
- **WHEN** "I speak" shows Spanish, English, and the member activates "move up" on English
- **THEN** "I speak" shows English, Spanish

#### Scenario: Ends of the list
- **WHEN** a list holds two or more entries
- **THEN** "move up" is disabled on the first entry and "move down" on the last

#### Scenario: A single entry
- **WHEN** a list holds one entry
- **THEN** both of its move controls are disabled

#### Scenario: The saved order
- **WHEN** the member reorders a list and saves
- **THEN** the request holds that list's entries in the order shown

### Requirement: Removing a language
Each entry SHALL have a remove control that takes it out of its list at once, without confirmation. A removed
language SHALL be offered by the picker again.

#### Scenario: Removing
- **WHEN** the member activates the remove control of Spanish
- **THEN** Spanish is no longer in its list and the picker offers Spanish again

#### Scenario: Removing the last entry
- **WHEN** the member removes the only entry of a list
- **THEN** the list shows no entry and still offers "Add a language"

### Requirement: Saving the complete selection
Save SHALL send one request holding the member's complete selection: both lists, each as an array in the order
shown, an empty list as an empty array. The app SHALL apply no rule of its own before sending: no minimum, no
maximum, no duplicate check, no check of codes or levels. On success the editor SHALL close and return to
the profile edit screen that opened it.

#### Scenario: Saving after editing one list
- **WHEN** the member changes only "I'm learning" and saves
- **THEN** the request holds both `spoken` and `learning` as arrays, with "I speak" as it was loaded

#### Scenario: Clearing everything
- **WHEN** the member removes every language from both lists and saves
- **THEN** the request holds `spoken` and `learning` as empty arrays, and the editor closes on success

#### Scenario: One empty list
- **WHEN** the member saves with languages in one list and none in the other
- **THEN** the request is sent without any client-side error

#### Scenario: More entries than the server allows
- **WHEN** a list holds more entries than the server accepts
- **THEN** "Add a language" stays available and Save sends the selection as shown

#### Scenario: Success
- **WHEN** the server answers the save with 200
- **THEN** the editor closes without asking about unsaved changes and the profile edit screen is shown

### Requirement: Save availability and a save in flight
Save SHALL be available only when the selection differs from the one last loaded. While a save is in flight
the Save control SHALL show that it is working, every control of the editor SHALL be disabled, a second save
SHALL NOT be sent, and attempts to leave the editor SHALL be ignored.

#### Scenario: Nothing changed
- **WHEN** the editor has loaded and the member has changed nothing
- **THEN** Save is disabled

#### Scenario: Changing back
- **WHEN** the member moves an entry down and then up again, so the selection equals the loaded one
- **THEN** Save is disabled again

#### Scenario: Double activation
- **WHEN** the member activates Save twice before the server answers
- **THEN** exactly one save request is sent

#### Scenario: Back during a save
- **WHEN** the member presses back while the save is in flight
- **THEN** the editor stays open until the save answers

### Requirement: Save failures
A failed save SHALL keep both lists exactly as the member arranged them and SHALL leave Save available for
another attempt. Every message SHALL be the app's own localized text, never text from the server. Validation
errors the server reports on a list SHALL be shown under that list; every other failure SHALL be shown as one
message above the lists. Changing either list SHALL clear the messages.

#### Scenario: A validation error on one list
- **WHEN** the server answers 422 with `too_many` on `spoken`
- **THEN** the "too many languages" text is shown under "I speak", the lists are unchanged and the first
  list with an error is brought into view

#### Scenario: Errors on both lists
- **WHEN** the server answers 422 with an error on `spoken` and one on `learning`
- **THEN** each list shows its own error

#### Scenario: A validation error the app cannot place
- **WHEN** the server answers 422 with no field the app knows
- **THEN** the general "check your input" message is shown above the lists

#### Scenario: Rate limited, unavailable, offline, timed out or unexpected
- **WHEN** the save fails with 429, 503, a network failure, a timeout, or any other failure
- **THEN** the matching localized message is shown above the lists, with the wait when the server gave one,
  and the lists are unchanged

#### Scenario: Retrying after a failure
- **WHEN** the member activates Save again after a failed save
- **THEN** the same complete selection is sent again

#### Scenario: Editing clears the messages
- **WHEN** an error is shown and the member adds, removes, moves or re-levels an entry
- **THEN** the errors under both lists and the message above them are no longer shown

#### Scenario: The session ended during the save
- **WHEN** the save fails because the session ended
- **THEN** the editor shows no error and the app goes to the log in screen

### Requirement: Cancelling and unsaved changes
The editor SHALL offer a Cancel control, and Cancel, the app bar's back control and the system back action
SHALL behave alike. With no unsaved change they SHALL close the editor at once. With unsaved changes they SHALL
ask the member to confirm discarding them, and SHALL close the editor only on confirmation. Leaving SHALL send
no save.

#### Scenario: Leaving with nothing changed
- **WHEN** the member has changed nothing and activates Cancel or goes back
- **THEN** the editor closes with no question and no save request

#### Scenario: Leaving with changes asks first
- **WHEN** the member has added, removed, moved or re-levelled an entry and activates Cancel or goes back
- **THEN** a dialog asks whether to discard the changes, with "Keep editing" and "Discard"

#### Scenario: Keep editing
- **WHEN** the member chooses "Keep editing", or dismisses the dialog
- **THEN** the editor stays open with the lists as the member arranged them

#### Scenario: Discard
- **WHEN** the member chooses "Discard"
- **THEN** the editor closes, no save request is sent, and the profile edit screen is shown as the member left
  it

#### Scenario: Leaving before the load finished
- **WHEN** the editor is loading or shows a load error and the member goes back
- **THEN** the editor closes with no question

#### Scenario: The session ends with unsaved changes
- **WHEN** the session ends while the editor has unsaved changes, with or without a sheet or dialog open
- **THEN** the app goes to the log in screen without asking, and nothing of the editor stays on screen

### Requirement: Profile shows the result
When the member returns to the profile page at `/profile` after visiting the editor, whether they saved,
cancelled or went back, and however they left the profile edit screen in between, the page's languages area
SHALL load the catalog and the selection again and show what the server has stored.

#### Scenario: After a save
- **WHEN** the member saves a changed selection, the editor closes, and the member goes back from the edit
  screen to the profile page
- **THEN** the languages area shows the new selection, in the new order

#### Scenario: After cancelling
- **WHEN** the member leaves the editor without saving and returns to the profile page
- **THEN** the languages area reloads and shows the stored selection

#### Scenario: The reload fails
- **WHEN** the languages reload after returning fails
- **THEN** the languages area shows its error and retry, and the rest of the profile page is unaffected

### Requirement: Telling the member their languages are public
The editor SHALL show, above the lists and whenever the lists are shown, a notice that other members will be
able to see the member's languages and levels. The languages area of the profile page SHALL carry no such notice.

#### Scenario: The notice precedes the save
- **WHEN** the editor has loaded
- **THEN** the notice is visible before the member can reach Save

### Requirement: Privacy of the member's languages
The app SHALL NOT write a member's language codes or levels to any log or console output, SHALL NOT place them
in a route, and SHALL send them only in the body of the member's own save request. The editor SHALL render no
token, no email address and no text taken from a server response other than catalog names.

#### Scenario: Nothing printed
- **WHEN** the member loads, edits, fails to save and saves languages
- **THEN** the app prints nothing

#### Scenario: Server text is never shown
- **WHEN** a failed save's response body contains arbitrary text
- **THEN** none of that text appears on screen

### Requirement: Accessibility of the editor
Every control of the editor, its picker, its level choice and its discard dialog SHALL have a text label that
names what it acts on, a tap target of at least 48 by 48 dp, and sufficient contrast in the light and the dark
theme. Every state SHALL remain usable, with every control reachable and nothing overflowing, at twice the
normal text size on a 320 dp wide screen.

#### Scenario: Labels name the language
- **WHEN** a screen reader reads the controls of the Spanish entry
- **THEN** it reads the language with its level, and "move up", "move down" and "remove" each with "Spanish"

#### Scenario: Large text on a small screen
- **WHEN** the editor is shown at text scale 2.0 on a 320 by 480 dp screen with several entries in each list
- **THEN** nothing overflows and Save, Cancel and every entry's controls can be scrolled to and activated

#### Scenario: Errors are announced
- **WHEN** a save fails
- **THEN** the message is announced to assistive technology as the other screens' error banners are

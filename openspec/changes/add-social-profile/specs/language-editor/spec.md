# Spec Delta

## ADDED Requirements

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

## MODIFIED Requirements

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

## REMOVED Requirements

### Requirement: Reaching the editor from Profile
**Reason**: The profile page at `/profile` is now read-only and no longer holds the profile form or an "Edit
languages" control. The editor is opened from the profile edit screen instead.
**Migration**: Use the "Languages" control on `/profile/edit`, described by "Reaching the editor from the
profile edit screen". The editor's route, `/profile/languages`, and everything it does are unchanged.

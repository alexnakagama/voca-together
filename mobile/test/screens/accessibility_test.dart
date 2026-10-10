import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/api/report_reason.dart';
import 'package:vocatogether/auth/google_identity_exception.dart';
import 'package:vocatogether/router.dart';
import 'package:vocatogether/ui/widgets/google_sign_in_button.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/form_notice_banner.dart';
import 'package:vocatogether/ui/widgets/profile_avatar.dart';

import '../support/fakes.dart';
import '../support/pictures.dart';
import 'harness.dart';

/// One screen in one state: how to script the backend, whether it starts
/// signed in, how to get there, and the controls that must stay reachable.
final class _Case {
  const _Case(
    this.name, {
    this.script,
    this.google,
    this.photos,
    this.signedIn = false,
    this.drive,
    required this.action,
    this.also = const [],
    this.largeTextWithKeyboard = false,
  });

  final String name;
  final void Function(FakeServer server)? script;

  /// Scripts Google; when set, the app has Google sign-in.
  final void Function(FakeGoogleIdentity google)? google;

  /// Scripts the photo chooser.
  final void Function(FakePhotoSource photos)? photos;
  final bool signedIn;
  final Future<void> Function(WidgetTester tester)? drive;
  final String action;

  /// More texts that must be reachable, besides [action].
  final List<String> also;

  /// Whether the state must also work at twice the text size on the small
  /// screen with the keyboard open: the states of a form one types in.
  final bool largeTextWithKeyboard;
}

Future<void> _logInAttempt(WidgetTester tester) async {
  await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
  await tester.enterText(field(l10n.passwordLabel), 'correct horse');
  await tapAndSettle(
    tester,
    find.widgetWithText(FilledButton, l10n.logInButton),
  );
}

Future<void> _openRegister(WidgetTester tester) =>
    tapAndSettle(tester, find.text(l10n.createAccountLink));

Future<void> _openForgot(WidgetTester tester) =>
    tapAndSettle(tester, find.text(l10n.forgotPasswordLink));

Future<void> _openProfile(WidgetTester tester) => tapAndSettle(
  tester,
  find.widgetWithText(OutlinedButton, l10n.profileButton),
);

/// From home: the profile page, then its edit screen, by "Edit Profile" or,
/// with no profile saved, by the empty state's button.
Future<void> _openForm(WidgetTester tester) async {
  await _openProfile(tester);
  final edit = find.widgetWithText(OutlinedButton, l10n.profileEditButton);
  await tapAndSettle(
    tester,
    edit.evaluate().isNotEmpty
        ? edit
        : find.widgetWithText(FilledButton, l10n.profileEmptyButton),
  );
}

Future<void> _typeProfile(WidgetTester tester) async {
  await _openForm(tester);
  await tester.enterText(field(l10n.displayNameLabel), 'Ana López');
  await tester.enterText(
    field(l10n.bioLabel),
    'I’m learning Japanese.\n\nEvenings work best for me.',
  );
  // Typing makes the field scroll its caret into view a moment later. Let
  // that finish before scrolling to a button, as it has by the time a
  // person does: on a device the form scrolls to Save with the keyboard
  // open and stays there, so this orders the test, it hides no layout fault.
  await tester.pumpAndSettle();
}

Future<void> _saveProfile(WidgetTester tester) async {
  await _typeProfile(tester);
  await tapAndSettle(
    tester,
    find.widgetWithText(FilledButton, l10n.profileSaveButton),
  );
}

void _home(FakeServer s) => s
  ..once('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()))
  // No picture, unless a case scripts one.
  ..always('GET', ApiPaths.myAvatar, (_) => noAvatar());

/// The profile page's languages section, for a member with [spoken] and
/// [learning] (none by default).
void _languages(
  FakeServer s, {
  List<(String, String)> spoken = const [],
  List<(String, String)> learning = const [],
}) => s
  ..once('GET', ApiPaths.languages, (_) => jsonResponse(200, catalogBody()))
  ..once(
    'GET',
    ApiPaths.myLanguages,
    (_) => jsonResponse(200, languagesBody(spoken: spoken, learning: learning)),
  );

/// A signed-in member with a profile and [spoken] and [learning] stored,
/// for the languages editor: the profile page and its edit screen each load
/// the profile, and the page's section and the editor each load the catalog
/// and the selection.
void _editor(
  FakeServer s, {
  List<(String, String)> spoken = const [],
  List<(String, String)> learning = const [],
}) {
  _home(s);
  s
    ..always(
      'GET',
      ApiPaths.profile,
      (_) => jsonResponse(200, profileBody(displayName: 'Ana')),
    )
    ..always('GET', ApiPaths.languages, (_) => jsonResponse(200, catalogBody()))
    ..always(
      'GET',
      ApiPaths.myLanguages,
      (_) =>
          jsonResponse(200, languagesBody(spoken: spoken, learning: learning)),
    );
}

Future<void> _openEditor(WidgetTester tester) async {
  await _openForm(tester);
  await tapAndSettle(
    tester,
    find.widgetWithText(ListTile, l10n.profileEditLanguagesButton),
  );
}

/// A long name and a long text, as a profile page must fit them.
const _longName = 'Wolfeschlegelsteinhausenbergerdorff Maria-Magdalena';
const _longBio =
    'I teach German and Dutch in the evenings and I am looking for someone '
    'patient to practise Portuguese with.\n\nWeekends only, mornings if '
    'you are in Asia, and I am happy to help with exam preparation.';

/// A member with no profile saved: the page's empty state, then the form
/// to create one. [save] answers the form's save.
void _newProfile(FakeServer s, {Responder? save}) {
  _home(s);
  s.always('GET', ApiPaths.profile, (_) => noProfile());
  if (save != null) s.once('PUT', ApiPaths.profile, save);
}

/// A member with a profile, on the page (which loads their languages) or
/// on the form.
void _savedProfile(FakeServer s) {
  _home(s);
  _languages(s, spoken: [('es', 'native')], learning: [('ja', 'a2')]);
  s.always(
    'GET',
    ApiPaths.profile,
    (_) => jsonResponse(
      200,
      profileBody(displayName: 'Ana', bio: 'Evenings work best for me.'),
    ),
  );
}

/// A member with a profile and a picture, on the page or on the form.
void _withPicture(FakeServer s) {
  _savedProfile(s);
  s.always('GET', ApiPaths.myAvatar, (_) => imageResponse(testPicture));
}

/// From home: the form, then a tap on the picture control's [label].
Future<void> _pictureAction(WidgetTester tester, String label) async {
  await _openForm(tester);
  await tapAndSettle(tester, find.widgetWithText(OutlinedButton, label));
}

/// From home: the profile page, then the member's own public profile.
Future<void> _openPublic(WidgetTester tester) async {
  await _openProfile(tester);
  await tapAndSettle(tester, find.byTooltip(l10n.profileSeePublicButton));
}

/// A member with a profile on their page, whose public profile answers
/// [member] and, when it says there is a picture, [avatar]. [catalog] names
/// the languages for both screens.
void _publicProfile(
  FakeServer s,
  Responder member, {
  Responder? avatar,
  Map<String, Object?>? catalog,
}) {
  _home(s);
  s
    ..always(
      'GET',
      ApiPaths.profile,
      (_) => jsonResponse(200, profileBody(displayName: 'Ana')),
    )
    ..always(
      'GET',
      ApiPaths.languages,
      (_) => jsonResponse(200, catalog ?? catalogBody()),
    )
    ..always(
      'GET',
      ApiPaths.myLanguages,
      (_) => jsonResponse(200, languagesBody()),
    )
    ..always('GET', ApiPaths.memberProfile(testMemberId), member);
  if (avatar != null) {
    s.always('GET', ApiPaths.memberAvatar(testMemberId), avatar);
  }
}

/// A member with a profile, and another member's profile with a long name,
/// a long text and languages. [block] answers the block of that member,
/// and [unblock] its removal.
void _otherMember(FakeServer s, {Responder? block, Responder? unblock}) {
  _home(s);
  s
    ..always(
      'GET',
      ApiPaths.profile,
      (_) => jsonResponse(200, profileBody(displayName: 'Ana')),
    )
    ..always('GET', ApiPaths.languages, (_) => jsonResponse(200, catalogBody()))
    ..always(
      'GET',
      ApiPaths.memberProfile(otherMemberId),
      (_) => jsonResponse(
        200,
        memberProfileBody(
          id: otherMemberId,
          displayName: _longName,
          bio: _longBio,
          languages: languagesBody(
            spoken: [('es', 'native')],
            learning: [('ja', 'a2')],
          ),
        ),
      ),
    );
  if (block != null) {
    s.once('PUT', ApiPaths.myBlock(otherMemberId), block);
  }
  if (unblock != null) {
    s.once('DELETE', ApiPaths.myBlock(otherMemberId), unblock);
  }
}

/// From home: another member's profile. Nothing in the app leads to one
/// yet, so the route is pushed as a screen would push it.
Future<void> _openOther(WidgetTester tester) async {
  unawaited(
    GoRouter.of(tester.element(find.byType(Navigator).first))
        .push<void>(Routes.member(otherMemberId)),
  );
  await tester.pumpAndSettle();
}

/// From home: another member's profile, with its menu open.
Future<void> _openMenu(WidgetTester tester) async {
  await _openOther(tester);
  await tapAndSettle(tester, find.byTooltip(l10n.memberMenuTooltip));
}

/// From home: another member's profile, with the block confirmation open.
Future<void> _askBlock(WidgetTester tester) async {
  await _openMenu(tester);
  await tapAndSettle(tester, find.text(l10n.memberMenuBlock));
}

/// From home: another member's profile, after confirming the block.
Future<void> _block(WidgetTester tester) async {
  await _askBlock(tester);
  await tapAndSettle(
    tester,
    find.widgetWithText(TextButton, l10n.memberBlockConfirm),
  );
}

/// From home: another member's profile, blocked, with the confirmation of
/// the unblock open.
Future<void> _askUnblock(WidgetTester tester) async {
  await _block(tester);
  await tapAndSettle(
    tester,
    find.widgetWithText(OutlinedButton, l10n.unblockButton),
  );
}

/// From home: another member's profile, blocked, after confirming the
/// unblock.
Future<void> _unblock(WidgetTester tester) async {
  await _askUnblock(tester);
  await tapAndSettle(
    tester,
    find.widgetWithText(TextButton, l10n.unblockButton),
  );
}

/// The five reasons of the report form.
final _reportReasons = [
  l10n.reportReasonHarassment,
  l10n.reportReasonInappropriateContent,
  l10n.reportReasonSpam,
  l10n.reportReasonImpersonation,
  l10n.reportReasonOther,
];

/// A member on another member's profile, whose report answers [send].
void _report(FakeServer s, {Responder? send}) {
  _otherMember(s);
  if (send != null) {
    s.once('PUT', ApiPaths.myReport(otherMemberId), send);
  }
}

/// From home: the report form of another member, opened from the menu of
/// their profile.
Future<void> _openReport(WidgetTester tester) async {
  await _openMenu(tester);
  await tapAndSettle(tester, find.text(l10n.memberMenuReport));
}

/// Scrolls the report screen back to its top, so that no reason is left cut
/// in half under the app bar.
Future<void> _reportTop(WidgetTester tester) async {
  await tester.drag(
    find.byType(SingleChildScrollView).last,
    const Offset(0, 5000),
    warnIfMissed: false,
  );
  await tester.pumpAndSettle();
}

/// From home: the report form with a reason chosen and a long text typed.
Future<void> _fillReport(WidgetTester tester) async {
  await _openReport(tester);
  await tapAndSettle(tester, find.text(l10n.reportReasonImpersonation));
  await tester.enterText(field(l10n.reportDetailsLabel), _longBio);
  await tester.pumpAndSettle();
  await _reportTop(tester);
}

/// From home: the report form, after activating "Send report" with nothing
/// chosen.
Future<void> _sendEmptyReport(WidgetTester tester) async {
  await _openReport(tester);
  await tapAndSettle(
    tester,
    find.widgetWithText(FilledButton, l10n.reportSendButton),
  );
}

/// From home: the report form, filled and sent.
Future<void> _sendReport(WidgetTester tester) async {
  await _fillReport(tester);
  await tapAndSettle(
    tester,
    find.widgetWithText(FilledButton, l10n.reportSendButton),
  );
  await _reportTop(tester);
}

/// A second blocked member, with a name as long as the first's.
const _secondLongName = 'Maximiliana Hohenzollern-Sigmaringen von Brandenburg';
const _secondMemberId = '3e4f5a6b-7c8d-4e9f-8a0b-1c2d3e4f5a6b';

/// A member on home whose list of blocked members answers [list]: two
/// members with long names by default. [unblock] answers the unblock of the
/// first.
void _blockedList(FakeServer s, {Responder? list, Responder? unblock}) {
  _home(s);
  s.always(
    'GET',
    ApiPaths.myBlocks,
    list ??
        (_) => jsonResponse(
          200,
          blocksBody([
            (otherMemberId, _longName),
            (_secondMemberId, _secondLongName),
          ]),
        ),
  );
  if (unblock != null) {
    s.once('DELETE', ApiPaths.myBlock(otherMemberId), unblock);
  }
}

/// From home: the blocked members.
Future<void> _openBlocked(WidgetTester tester) => tapAndSettle(
  tester,
  find.widgetWithText(OutlinedButton, l10n.blockedMembersButton),
);

/// From home: the blocked members, with the confirmation of the first
/// member's unblock open.
Future<void> _askListUnblock(WidgetTester tester) async {
  await _openBlocked(tester);
  await tapAndSettle(
    tester,
    find.widgetWithText(OutlinedButton, l10n.unblockButton).first,
  );
}

/// From home: the blocked members, after confirming the first member's
/// unblock.
Future<void> _listUnblock(WidgetTester tester) async {
  await _askListUnblock(tester);
  await tapAndSettle(
    tester,
    find.widgetWithText(TextButton, l10n.unblockButton),
  );
}

/// Opens the picker of "I speak".
Future<void> _openPicker(WidgetTester tester) async {
  await _openEditor(tester);
  await tapAndSettle(tester, find.text(l10n.languagesAddButton).first);
}

final _cases = <_Case>[
  _Case('login', action: l10n.logInButton),
  _Case(
    'login, empty fields',
    drive: (t) =>
        tapAndSettle(t, find.widgetWithText(FilledButton, l10n.logInButton)),
    action: l10n.logInButton,
  ),
  _Case(
    'login, 401',
    script: (s) => s.once(
      'POST',
      ApiPaths.login,
      (_) => errorResponse(401, 'invalid_credentials'),
    ),
    drive: _logInAttempt,
    action: l10n.logInButton,
  ),
  _Case(
    'login, not verified, resent',
    script: (s) => s
      ..once(
        'POST',
        ApiPaths.login,
        (_) => errorResponse(403, 'email_not_verified'),
      )
      ..once('POST', ApiPaths.resendVerification, (_) => accepted()),
    drive: (t) async {
      await _logInAttempt(t);
      await tapAndSettle(t, find.text(l10n.resendVerificationButton));
    },
    action: l10n.logInButton,
  ),
  _Case(
    'register, errors',
    drive: (t) async {
      await _openRegister(t);
      await t.enterText(field(l10n.passwordLabel), 'a');
      await t.enterText(field(l10n.confirmPasswordLabel), 'b');
      await tapAndSettle(
        t,
        find.widgetWithText(FilledButton, l10n.registerButton),
      );
    },
    action: l10n.registerButton,
  ),
  _Case(
    'register, check your email',
    script: (s) => s.once('POST', ApiPaths.register, (_) => accepted()),
    drive: (t) async {
      await _openRegister(t);
      await t.enterText(field(l10n.emailLabel), 'ana@example.com');
      await t.enterText(field(l10n.passwordLabel), 'correct horse');
      await t.enterText(field(l10n.confirmPasswordLabel), 'correct horse');
      await tapAndSettle(
        t,
        find.widgetWithText(FilledButton, l10n.registerButton),
      );
    },
    action: l10n.backToLogIn,
  ),
  _Case(
    'forgot password, 429',
    script: (s) => s.once(
      'POST',
      ApiPaths.forgotPassword,
      (_) => errorResponse(429, 'rate_limited', headers: {'retry-after': '60'}),
    ),
    drive: (t) async {
      await _openForgot(t);
      await t.enterText(field(l10n.emailLabel), 'ana@example.com');
      await tapAndSettle(
        t,
        find.widgetWithText(FilledButton, l10n.forgotPasswordButton),
      );
    },
    action: l10n.forgotPasswordButton,
  ),
  _Case(
    'forgot password, sent',
    script: (s) => s.once('POST', ApiPaths.forgotPassword, (_) => accepted()),
    drive: (t) async {
      await _openForgot(t);
      await t.enterText(field(l10n.emailLabel), 'ana@example.com');
      await tapAndSettle(
        t,
        find.widgetWithText(FilledButton, l10n.forgotPasswordButton),
      );
    },
    action: l10n.backToLogIn,
  ),
  _Case(
    'home',
    signedIn: true,
    script: (s) =>
        s.once('GET', ApiPaths.me, (_) => jsonResponse(200, meBody())),
    action: l10n.logOutButton,
  ),
  _Case('login with Google', google: (_) {}, action: l10n.continueWithGoogle),
  _Case(
    'login with Google, account exists',
    google: (g) => g.next(googleIdToken('one')),
    script: (s) => s.once(
      'POST',
      ApiPaths.google,
      (_) => errorResponse(409, 'account_exists'),
    ),
    drive: (t) => tapAndSettle(t, find.byType(GoogleSignInButton)),
    action: l10n.continueWithGoogle,
  ),
  _Case(
    'register with Google, Google unavailable',
    google: (g) => g.fail(
      const GoogleIdentityException(GoogleIdentityFailure.unavailable),
    ),
    drive: (t) async {
      await _openRegister(t);
      await tapAndSettle(t, find.byType(GoogleSignInButton));
    },
    action: l10n.continueWithGoogle,
  ),
  _Case(
    'home, failed',
    signedIn: true,
    script: (s) => s.once('GET', ApiPaths.me, networkFailure),
    action: l10n.logOutButton,
  ),
  _Case(
    'home, profile entry',
    signedIn: true,
    script: _home,
    action: l10n.profileButton,
    also: [l10n.blockedMembersButton],
  ),
  _Case(
    'profile, none saved',
    signedIn: true,
    script: _newProfile,
    drive: _openProfile,
    action: l10n.profileEmptyButton,
  ),
  _Case(
    'profile, load failed',
    signedIn: true,
    script: (s) {
      _home(s);
      s.once('GET', ApiPaths.profile, networkFailure);
    },
    drive: _openProfile,
    action: l10n.tryAgain,
  ),
  _Case(
    'profile, name only',
    signedIn: true,
    script: (s) {
      _home(s);
      _languages(s);
      s.once(
        'GET',
        ApiPaths.profile,
        (_) => jsonResponse(200, profileBody(displayName: 'Ana')),
      );
    },
    drive: _openProfile,
    action: l10n.profileEditButton,
    also: [l10n.profileFriendsComingLater, l10n.languagesEmpty],
  ),
  _Case(
    'profile, a long name, a long text and languages',
    signedIn: true,
    script: (s) {
      _home(s);
      _languages(
        s,
        spoken: [('es', 'native'), ('en', 'c1')],
        learning: [('ja', 'a2')],
      );
      s.once(
        'GET',
        ApiPaths.profile,
        (_) => jsonResponse(
          200,
          profileBody(displayName: _longName, bio: _longBio),
        ),
      );
    },
    drive: _openProfile,
    action: l10n.profileEditButton,
    also: [
      _longName,
      l10n.profileFriendsHeading,
      l10n.profileFriendsComingLater,
      l10n.languagesLearningHeading,
      'Japanese',
    ],
  ),
  _Case(
    'profile, languages failed',
    signedIn: true,
    script: (s) {
      _home(s);
      s
        ..once(
          'GET',
          ApiPaths.profile,
          (_) => jsonResponse(200, profileBody(displayName: 'Ana')),
        )
        ..once(
          'GET',
          ApiPaths.languages,
          (_) => jsonResponse(200, catalogBody()),
        )
        ..once('GET', ApiPaths.myLanguages, networkFailure);
    },
    drive: _openProfile,
    action: l10n.tryAgain,
    also: [l10n.profileEditButton],
  ),
  _Case(
    'profile edit, new',
    signedIn: true,
    script: _newProfile,
    drive: _openForm,
    action: l10n.profileSaveButton,
    also: [
      l10n.profileAvatarAddButton,
      l10n.profileEditLanguagesButton,
      l10n.profileCancelButton,
    ],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile edit, load failed',
    signedIn: true,
    script: (s) {
      _savedProfile(s);
      s
        ..once(
          'GET',
          ApiPaths.profile,
          (_) => jsonResponse(200, profileBody(displayName: 'Ana')),
        )
        ..once('GET', ApiPaths.profile, networkFailure);
    },
    drive: _openForm,
    action: l10n.tryAgain,
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile edit, a saved profile',
    signedIn: true,
    script: _savedProfile,
    drive: _openForm,
    action: l10n.profileSaveButton,
    also: [
      l10n.profileAvatarAddButton,
      l10n.profileEditLanguagesButton,
      l10n.profileCancelButton,
    ],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile edit, typed',
    signedIn: true,
    script: _savedProfile,
    drive: _typeProfile,
    action: l10n.profileSaveButton,
    also: [l10n.profileEditLanguagesButton, l10n.profileCancelButton],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile edit, empty name',
    signedIn: true,
    script: _newProfile,
    drive: (t) async {
      await _openForm(t);
      await tapAndSettle(
        t,
        find.widgetWithText(FilledButton, l10n.profileSaveButton),
      );
    },
    action: l10n.profileSaveButton,
    also: [l10n.profileEditLanguagesButton, l10n.profileCancelButton],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile edit, field errors',
    signedIn: true,
    script: (s) => _newProfile(
      s,
      save: (_) => jsonResponse(422, {
        'error': {
          'code': 'validation_failed',
          'fields': [
            {'field': 'display_name', 'code': 'invalid'},
            {'field': 'bio', 'code': 'too_long'},
          ],
        },
      }),
    ),
    drive: _saveProfile,
    action: l10n.profileSaveButton,
    also: [l10n.profileEditLanguagesButton, l10n.profileCancelButton],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile edit, save failed',
    signedIn: true,
    script: (s) => _newProfile(
      s,
      save: (_) =>
          errorResponse(429, 'rate_limited', headers: {'retry-after': '6'}),
    ),
    drive: _saveProfile,
    action: l10n.profileSaveButton,
    also: [l10n.profileEditLanguagesButton, l10n.profileCancelButton],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile edit, discard question',
    signedIn: true,
    script: _savedProfile,
    drive: (t) async {
      await _typeProfile(t);
      await tapAndSettle(
        t,
        find.widgetWithText(OutlinedButton, l10n.profileCancelButton),
      );
    },
    action: l10n.languagesDiscardConfirm,
    also: [l10n.languagesDiscardKeep],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile, with a picture',
    signedIn: true,
    script: _withPicture,
    drive: _openProfile,
    action: l10n.profileEditButton,
  ),
  _Case(
    'profile edit, with a picture',
    signedIn: true,
    script: _withPicture,
    drive: _openForm,
    action: l10n.profileAvatarChangeButton,
    also: [
      l10n.profileAvatarRemoveButton,
      l10n.profileSaveButton,
      l10n.profileEditLanguagesButton,
      l10n.profileCancelButton,
    ],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile edit, the picture failed to load',
    signedIn: true,
    script: (s) {
      _savedProfile(s);
      s
        ..once('GET', ApiPaths.myAvatar, (_) => noAvatar())
        ..once('GET', ApiPaths.myAvatar, networkFailure);
    },
    drive: _openForm,
    action: l10n.profileAvatarAddButton,
    also: [l10n.profileSaveButton],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile edit, photo refused',
    signedIn: true,
    script: (s) {
      _withPicture(s);
      s.once(
        'PUT',
        ApiPaths.myAvatar,
        (_) => jsonResponse(422, {
          'error': {
            'code': 'validation_failed',
            'fields': [
              {'field': 'avatar', 'code': 'dimensions_too_large'},
            ],
          },
        }),
      );
    },
    photos: (p) => p.next(const [1, 2, 3]),
    drive: (t) => _pictureAction(t, l10n.profileAvatarChangeButton),
    action: l10n.profileAvatarChangeButton,
    also: [
      l10n.errorAvatarDimensionsTooLarge,
      l10n.profileAvatarRemoveButton,
      l10n.profileSaveButton,
      l10n.profileEditLanguagesButton,
      l10n.profileCancelButton,
    ],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile edit, photo unreadable',
    signedIn: true,
    script: _savedProfile,
    photos: (p) => p.fail(),
    drive: (t) => _pictureAction(t, l10n.profileAvatarAddButton),
    action: l10n.profileAvatarAddButton,
    also: [l10n.errorPhotoUnusable, l10n.profileSaveButton],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile edit, removal failed',
    signedIn: true,
    script: (s) {
      _withPicture(s);
      s.once(
        'DELETE',
        ApiPaths.myAvatar,
        (_) =>
            errorResponse(429, 'rate_limited', headers: {'retry-after': '90'}),
      );
    },
    drive: (t) async {
      await _pictureAction(t, l10n.profileAvatarRemoveButton);
      await tapAndSettle(t, find.text(l10n.profileAvatarRemoveConfirm));
      // Scrolling to "Remove photo" left the button above it cut by the
      // top of the list, and a cut button measures short. Back to the top,
      // where the whole control is in view.
      await t.ensureVisible(find.byType(ProfileAvatar));
      await t.pumpAndSettle();
    },
    action: l10n.profileAvatarRemoveButton,
    also: [l10n.profileAvatarChangeButton, l10n.profileSaveButton],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'profile edit, removal question',
    signedIn: true,
    script: _withPicture,
    drive: (t) => _pictureAction(t, l10n.profileAvatarRemoveButton),
    action: l10n.profileAvatarRemoveConfirm,
    also: [l10n.profileAvatarRemoveKeep],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'languages editor, load failed',
    signedIn: true,
    script: (s) {
      _editor(s);
      s
        ..once(
          'GET',
          ApiPaths.myLanguages,
          (_) => jsonResponse(200, languagesBody()),
        )
        ..once('GET', ApiPaths.myLanguages, networkFailure);
    },
    drive: _openEditor,
    action: l10n.tryAgain,
  ),
  _Case(
    'languages editor, none chosen',
    signedIn: true,
    script: _editor,
    drive: _openEditor,
    action: l10n.languagesAddButton,
  ),
  _Case(
    'languages editor, languages',
    signedIn: true,
    script: (s) => _editor(
      s,
      spoken: [('es', 'native'), ('en', 'c1')],
      learning: [('ja', 'a2')],
    ),
    drive: _openEditor,
    action: l10n.languagesCancelButton,
  ),
  _Case(
    'languages editor, picker',
    signedIn: true,
    script: (s) => _editor(s, learning: [('ja', 'a2')]),
    drive: _openPicker,
    action: 'Spanish',
  ),
  _Case(
    'languages editor, picker without a match',
    signedIn: true,
    script: _editor,
    drive: (t) async {
      await _openPicker(t);
      await t.enterText(
        find.widgetWithText(TextField, l10n.languagePickerSearchLabel),
        'klingon',
      );
      await t.pumpAndSettle();
    },
    action: l10n.languagePickerNoMatch,
  ),
  _Case(
    'languages editor, level choice',
    signedIn: true,
    script: (s) => _editor(s, spoken: [('es', 'b2')]),
    drive: (t) async {
      await _openEditor(t);
      await tapAndSettle(t, find.text(l10n.languageLevelB2));
    },
    action: l10n.languageLevelNative,
  ),
  _Case(
    'languages editor, save failed',
    signedIn: true,
    script: (s) {
      _editor(s, spoken: [('es', 'native'), ('en', 'c1')]);
      s.once(
        'PUT',
        ApiPaths.myLanguages,
        (_) => jsonResponse(422, {
          'error': {
            'code': 'validation_failed',
            'fields': [
              {'field': 'spoken', 'code': 'too_many'},
              {'field': 'learning', 'code': 'duplicate'},
              {'field': 'kind', 'code': 'unknown'},
            ],
          },
        }),
      );
    },
    drive: (t) async {
      await _openEditor(t);
      await tapAndSettle(t, find.byTooltip(l10n.languageMoveUp('English')));
      await tapAndSettle(
        t,
        find.widgetWithText(FilledButton, l10n.languagesSaveButton),
      );
    },
    action: l10n.languagesSaveButton,
  ),
  _Case(
    'languages editor, discard question',
    signedIn: true,
    script: (s) => _editor(s, spoken: [('es', 'native'), ('en', 'c1')]),
    drive: (t) async {
      await _openEditor(t);
      await tapAndSettle(t, find.byTooltip(l10n.languageRemove('English')));
      await tapAndSettle(
        t,
        find.widgetWithText(OutlinedButton, l10n.languagesCancelButton),
      );
    },
    action: l10n.languagesDiscardConfirm,
  ),
  _Case(
    'member profile, name only',
    signedIn: true,
    script: (s) => _publicProfile(
      s,
      (_) => jsonResponse(200, memberProfileBody(displayName: 'Ana')),
    ),
    drive: _openPublic,
    action: l10n.memberLanguagesEmpty,
    also: ['Ana', l10n.languagesHeading],
  ),
  _Case(
    'member profile, a long name, a long text, a picture and five languages '
    'in each list',
    signedIn: true,
    script: (s) => _publicProfile(
      s,
      (_) => jsonResponse(
        200,
        memberProfileBody(
          displayName: _longName,
          bio: _longBio,
          hasAvatar: true,
          languages: languagesBody(
            spoken: [
              ('xaa', 'native'),
              ('xab', 'c2'),
              ('xac', 'c1'),
              ('xad', 'b2'),
              ('xae', 'b1'),
            ],
            learning: [
              ('xaf', 'a1'),
              ('xag', 'a2'),
              ('xah', 'b1'),
              ('xai', 'b2'),
              // The catalog doesn't name this one: shown by its code.
              ('zzz', 'c1'),
            ],
          ),
        ),
      ),
      avatar: (_) => imageResponse(testPicture),
      catalog: largeCatalogBody(),
    ),
    drive: _openPublic,
    action: 'zzz',
    also: [
      _longName,
      l10n.languagesHeading,
      l10n.languagesSpokenHeading,
      'Language 000',
      l10n.languagesLearningHeading,
      'Language 008',
    ],
  ),
  _Case(
    'member profile, picture failed',
    signedIn: true,
    script: (s) => _publicProfile(
      s,
      (_) => jsonResponse(
        200,
        memberProfileBody(
          displayName: 'Ana',
          bio: 'Evenings work best for me.',
          hasAvatar: true,
          languages: languagesBody(spoken: [('es', 'native')]),
        ),
      ),
      avatar: networkFailure,
    ),
    drive: _openPublic,
    action: 'Spanish',
    also: ['Ana', 'Evenings work best for me.'],
  ),
  _Case(
    'member profile, unavailable',
    signedIn: true,
    script: (s) => _publicProfile(s, (_) => noProfile()),
    drive: _openPublic,
    action: l10n.memberProfileUnavailable,
  ),
  _Case(
    'member profile, load failed',
    signedIn: true,
    script: (s) => _publicProfile(s, networkFailure),
    drive: _openPublic,
    action: l10n.tryAgain,
  ),
  _Case(
    'member profile, another member',
    signedIn: true,
    script: _otherMember,
    drive: _openOther,
    action: 'Japanese',
    also: [_longName, 'Spanish'],
  ),
  _Case(
    'member profile, the menu open',
    signedIn: true,
    script: _otherMember,
    drive: _openMenu,
    action: l10n.memberMenuBlock,
    also: [l10n.memberMenuReport],
  ),
  _Case(
    'member profile, block confirmation',
    signedIn: true,
    script: _otherMember,
    drive: _askBlock,
    action: l10n.memberBlockConfirm,
    also: [
      l10n.memberBlockTitle,
      l10n.memberBlockMessage,
      l10n.memberBlockCancel,
    ],
  ),
  _Case(
    'member profile, block failed',
    signedIn: true,
    script: (s) => _otherMember(s, block: networkFailure),
    drive: _block,
    action: l10n.errorNetwork,
    also: [_longName, 'Japanese'],
  ),
  _Case(
    'member profile, too many blocked members',
    signedIn: true,
    script: (s) =>
        _otherMember(s, block: (_) => fieldError('blocks', 'too_many')),
    drive: _block,
    action: l10n.errorBlocksTooMany,
    also: [_longName],
  ),
  _Case(
    'member profile, blocked',
    signedIn: true,
    script: (s) => _otherMember(s, block: (_) => noContent()),
    drive: _block,
    action: l10n.unblockButton,
    also: [l10n.memberBlocked],
  ),
  _Case(
    'member profile, unblock confirmation',
    signedIn: true,
    script: (s) => _otherMember(s, block: (_) => noContent()),
    drive: _askUnblock,
    action: l10n.unblockButton,
    also: [l10n.unblockTitle, l10n.memberUnblockMessage, l10n.unblockCancel],
  ),
  _Case(
    'member profile, unblock failed',
    signedIn: true,
    script: (s) =>
        _otherMember(s, block: (_) => noContent(), unblock: networkFailure),
    drive: _unblock,
    action: l10n.unblockButton,
    also: [l10n.memberBlocked, l10n.errorNetwork],
  ),
  _Case(
    'blocked members, nobody',
    signedIn: true,
    script: (s) =>
        _blockedList(s, list: (_) => jsonResponse(200, blocksBody())),
    drive: _openBlocked,
    action: l10n.blockedMembersEmpty,
  ),
  _Case(
    'blocked members, load failed',
    signedIn: true,
    script: (s) => _blockedList(s, list: networkFailure),
    drive: _openBlocked,
    action: l10n.tryAgain,
  ),
  _Case(
    'blocked members, two long names',
    signedIn: true,
    script: _blockedList,
    drive: _openBlocked,
    action: l10n.unblockButton,
    also: [_longName, _secondLongName],
  ),
  _Case(
    'blocked members, unblock confirmation',
    signedIn: true,
    script: _blockedList,
    drive: _askListUnblock,
    action: l10n.unblockButton,
    also: [
      l10n.unblockTitle,
      l10n.blockedMembersUnblockMessage(_longName),
      l10n.unblockCancel,
    ],
  ),
  _Case(
    'blocked members, unblock failed',
    signedIn: true,
    script: (s) => _blockedList(s, unblock: networkFailure),
    drive: _listUnblock,
    action: l10n.unblockButton,
    also: [l10n.errorNetwork, _longName, _secondLongName],
  ),
  _Case(
    'blocked members, the last one unblocked',
    signedIn: true,
    script: (s) => _blockedList(
      s,
      list: (_) => jsonResponse(200, blocksBody([(otherMemberId, _longName)])),
      unblock: (_) => noContent(),
    ),
    drive: _listUnblock,
    action: l10n.blockedMembersEmpty,
  ),
  _Case(
    'report, empty form',
    signedIn: true,
    script: _report,
    drive: _openReport,
    action: l10n.reportSendButton,
    also: [
      l10n.reportPrivacyNotice,
      l10n.reportReasonHeading,
      ..._reportReasons,
    ],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'report, a reason chosen and a long text',
    signedIn: true,
    script: _report,
    drive: _fillReport,
    action: l10n.reportSendButton,
    also: _reportReasons,
    largeTextWithKeyboard: true,
  ),
  _Case(
    'report, no reason chosen',
    signedIn: true,
    script: _report,
    drive: _sendEmptyReport,
    action: l10n.reportSendButton,
    also: [l10n.errorReportReasonRequired, ..._reportReasons],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'report, field errors',
    signedIn: true,
    script: (s) => _report(
      s,
      send: (_) => jsonResponse(422, {
        'error': {
          'code': 'validation_failed',
          'fields': [
            {'field': 'reason', 'code': 'invalid'},
            {'field': 'details', 'code': 'too_long'},
          ],
        },
      }),
    ),
    drive: _sendReport,
    action: l10n.reportSendButton,
    also: [l10n.errorReportReasonInvalid, ..._reportReasons],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'report, send failed',
    signedIn: true,
    script: (s) => _report(
      s,
      send: (_) =>
          errorResponse(429, 'rate_limited', headers: {'retry-after': '60'}),
    ),
    drive: _sendReport,
    action: l10n.reportSendButton,
    also: ['Too many attempts. Try again in 1 minute.', ..._reportReasons],
    largeTextWithKeyboard: true,
  ),
  _Case(
    'report, sent',
    signedIn: true,
    script: (s) => _report(s, send: (_) => noContent()),
    drive: _sendReport,
    // Only the control is probed: at twice the text size the confirmation
    // is taller than what an open keyboard leaves, and scrolls like the rest
    // (its text and announcement are checked in the screen's own tests).
    action: l10n.reportBackToProfile,
    largeTextWithKeyboard: true,
  ),
];

Future<void> _reach(
  WidgetTester tester,
  _Case c, {
  Size? size,
  double textScale = 1,
  double keyboard = 0,
  Brightness brightness = Brightness.light,
}) async {
  final server = FakeServer();
  c.script?.call(server);
  FakeGoogleIdentity? google;
  if (c.google case final script?) {
    google = FakeGoogleIdentity();
    script(google);
  }
  final photos = FakePhotoSource();
  c.photos?.call(photos);
  await pumpApp(
    tester,
    server: server,
    google: google,
    photos: photos,
    signedIn: c.signedIn,
    size: size,
    textScale: textScale,
    keyboard: keyboard,
    brightness: brightness,
  );
  await c.drive?.call(tester);
  expect(tester.takeException(), isNull, reason: c.name);
}

/// The main control can be scrolled to and receives the tap.
Future<void> _expectReachable(WidgetTester tester, String label) async {
  final finder = find.text(label).last;
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  final center = tester.getCenter(finder);
  final view = tester.view.physicalSize / tester.view.devicePixelRatio;
  expect(center.dy, inInclusiveRange(0, view.height), reason: label);
  final hit = tester.hitTestOnBinding(center);
  expect(
    hit.path.any((e) => e.target == tester.renderObject(finder)),
    isTrue,
    reason: '$label is covered',
  );
}

void main() {
  for (final c in _cases) {
    group(c.name, () {
      for (final brightness in Brightness.values) {
        testWidgets('meets the tap-target, label and contrast guidelines '
            '(${brightness.name})', (tester) async {
          final handle = tester.ensureSemantics();
          await _reach(tester, c, brightness: brightness);
          await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
          await expectLater(tester, meetsGuideline(labeledTapTargetGuideline));
          await expectLater(tester, meetsGuideline(textContrastGuideline));
          handle.dispose();
        });
      }

      testWidgets('large text on a small screen', (tester) async {
        await _reach(tester, c, size: const Size(320, 480), textScale: 2);
        for (final label in [c.action, ...c.also]) {
          await _expectReachable(tester, label);
        }
      });

      testWidgets('with the keyboard open', (tester) async {
        await _reach(tester, c, size: const Size(360, 640), keyboard: 300);
        for (final label in [c.action, ...c.also]) {
          await _expectReachable(tester, label);
        }
      });

      if (c.largeTextWithKeyboard) {
        testWidgets('large text on a small screen with the keyboard open', (
          tester,
        ) async {
          await _reach(
            tester,
            c,
            size: const Size(320, 480),
            textScale: 2,
            keyboard: 240,
          );
          for (final label in [c.action, ...c.also]) {
            await _expectReachable(tester, label);
          }
        });
      }
    });
  }

  // States that never settle, so they can't go through the cases above:
  // each is reached with its request left unanswered.
  final loading = <String, (Future<void> Function(WidgetTester), String)>{
    'profile, loading': (
      (tester) async {
        final server = FakeServer();
        _home(server);
        server.once('GET', ApiPaths.profile, neverAnswers);
        await pumpApp(tester, server: server, signedIn: true);
        await tester.tap(
          find.widgetWithText(OutlinedButton, l10n.profileButton),
        );
      },
      l10n.profileLoading,
    ),
    'profile edit, loading': (
      (tester) async {
        final server = FakeServer();
        _savedProfile(server);
        await pumpApp(tester, server: server, signedIn: true);
        await _openProfile(tester);
        server.once('GET', ApiPaths.profile, neverAnswers);
        final edit = find.widgetWithText(
          OutlinedButton,
          l10n.profileEditButton,
        );
        await tester.ensureVisible(edit);
        await tester.pumpAndSettle();
        await tester.tap(edit);
      },
      l10n.profileLoading,
    ),
    'profile edit, picture loading': (
      (tester) async {
        final server = FakeServer();
        _savedProfile(server);
        await pumpApp(tester, server: server, signedIn: true);
        await _openProfile(tester);
        server.once('GET', ApiPaths.myAvatar, neverAnswers);
        final edit = find.widgetWithText(
          OutlinedButton,
          l10n.profileEditButton,
        );
        await tester.ensureVisible(edit);
        await tester.pumpAndSettle();
        await tester.tap(edit);
      },
      l10n.profileAvatarLoading,
    ),
    // A busy button keeps its label, so that is what names the state.
    'profile edit, uploading': (
      (tester) async {
        final server = FakeServer();
        _withPicture(server);
        server.once('PUT', ApiPaths.myAvatar, neverAnswers);
        final photos = FakePhotoSource()..next(const [1, 2, 3]);
        await pumpApp(tester, server: server, photos: photos, signedIn: true);
        await _openForm(tester);
        final change = find.widgetWithText(
          OutlinedButton,
          l10n.profileAvatarChangeButton,
        );
        await tester.ensureVisible(change);
        await tester.pumpAndSettle();
        await tester.tap(change);
      },
      l10n.profileAvatarChangeButton,
    ),
    'profile edit, removing': (
      (tester) async {
        final server = FakeServer();
        _withPicture(server);
        server.once('DELETE', ApiPaths.myAvatar, neverAnswers);
        await pumpApp(tester, server: server, signedIn: true);
        await _pictureAction(tester, l10n.profileAvatarRemoveButton);
        await tester.tap(find.text(l10n.profileAvatarRemoveConfirm));
      },
      l10n.profileAvatarRemoveButton,
    ),
    'member profile, loading': (
      (tester) async {
        final server = FakeServer();
        _publicProfile(server, neverAnswers);
        await pumpApp(tester, server: server, signedIn: true);
        await _openProfile(tester);
        await tester.tap(find.byTooltip(l10n.profileSeePublicButton));
      },
      l10n.memberProfileLoading,
    ),
    'member profile, blocking': (
      (tester) async {
        final server = FakeServer();
        _otherMember(server, block: neverAnswers);
        await pumpApp(tester, server: server, signedIn: true);
        await _askBlock(tester);
        final confirm = find.widgetWithText(
          TextButton,
          l10n.memberBlockConfirm,
        );
        await tester.ensureVisible(confirm);
        await tester.pumpAndSettle();
        await tester.tap(confirm);
      },
      l10n.memberBlockProgress,
    ),
    'member profile, unblocking': (
      (tester) async {
        final server = FakeServer();
        _otherMember(server, block: (_) => noContent(), unblock: neverAnswers);
        await pumpApp(tester, server: server, signedIn: true);
        await _askUnblock(tester);
        final confirm = find.widgetWithText(TextButton, l10n.unblockButton);
        await tester.ensureVisible(confirm);
        await tester.pumpAndSettle();
        await tester.tap(confirm);
      },
      l10n.unblockProgress,
    ),
    'blocked members, loading': (
      (tester) async {
        final server = FakeServer();
        _blockedList(server, list: neverAnswers);
        await pumpApp(tester, server: server, signedIn: true);
        await tester.tap(
          find.widgetWithText(OutlinedButton, l10n.blockedMembersButton),
        );
      },
      l10n.blockedMembersLoading,
    ),
    'blocked members, unblocking': (
      (tester) async {
        final server = FakeServer();
        _blockedList(server, unblock: neverAnswers);
        await pumpApp(tester, server: server, signedIn: true);
        await _askListUnblock(tester);
        final confirm = find.widgetWithText(TextButton, l10n.unblockButton);
        await tester.ensureVisible(confirm);
        await tester.pumpAndSettle();
        await tester.tap(confirm);
      },
      l10n.unblockProgress,
    ),
    // The busy button keeps its label, which is what says it is working.
    'report, sending': (
      (tester) async {
        final server = FakeServer();
        _report(server, send: neverAnswers);
        await pumpApp(tester, server: server, signedIn: true);
        await _fillReport(tester);
        final send = find.widgetWithText(FilledButton, l10n.reportSendButton);
        await tester.ensureVisible(send);
        await tester.pumpAndSettle();
        await tester.tap(send);
      },
      l10n.reportSendButton,
    ),
    'languages editor, loading': (
      (tester) async {
        final server = FakeServer();
        _editor(server);
        await pumpApp(tester, server: server, signedIn: true);
        await _openForm(tester);
        server.once('GET', ApiPaths.myLanguages, neverAnswers);
        final row = find.widgetWithText(
          ListTile,
          l10n.profileEditLanguagesButton,
        );
        await tester.ensureVisible(row);
        await tester.pumpAndSettle();
        await tester.tap(row);
      },
      l10n.languagesLoading,
    ),
  };
  loading.forEach((name, state) {
    final (open, label) = state;
    group(name, () {
      Future<void> reach(
        WidgetTester tester, {
        Size? size,
        double textScale = 1,
        Brightness brightness = Brightness.light,
      }) async {
        if (size != null) {
          tester.view
            ..devicePixelRatio = 1
            ..physicalSize = size;
          addTearDown(tester.view.reset);
        }
        tester.platformDispatcher
          ..textScaleFactorTestValue = textScale
          ..platformBrightnessTestValue = brightness;
        addTearDown(tester.platformDispatcher.clearAllTestValues);
        await open(tester);
        await tester.pump();
        await tester.pump(const Duration(seconds: 1));
        expect(find.bySemanticsLabel(label), findsOneWidget);
        expect(tester.takeException(), isNull);
      }

      for (final brightness in Brightness.values) {
        testWidgets('meets the tap-target, label and contrast guidelines '
            '(${brightness.name})', (tester) async {
          final handle = tester.ensureSemantics();
          await reach(tester, brightness: brightness);
          await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
          await expectLater(tester, meetsGuideline(labeledTapTargetGuideline));
          await expectLater(tester, meetsGuideline(textContrastGuideline));
          handle.dispose();
          // Lets the request time out, so no timer outlives the test.
          await tester.pump(const Duration(seconds: 16));
          await tester.pumpAndSettle();
        });
      }

      testWidgets('large text on a small screen', (tester) async {
        final handle = tester.ensureSemantics();
        await reach(tester, size: const Size(320, 480), textScale: 2);
        // The way back is there while it loads.
        expect(find.byType(BackButton).hitTestable(), findsOneWidget);
        handle.dispose();
        await tester.pump(const Duration(seconds: 16));
        await tester.pumpAndSettle();
        expect(tester.takeException(), isNull);
      });
    });
  });

  group('semantics', () {
    testWidgets('"See public profile" is a labelled control that stays '
        'reachable with large text on a small screen', (tester) async {
      final server = FakeServer();
      _publicProfile(
        server,
        (_) => jsonResponse(200, memberProfileBody(displayName: _longName)),
      );
      server.always(
        'GET',
        ApiPaths.profile,
        (_) => jsonResponse(
          200,
          profileBody(displayName: _longName, bio: _longBio),
        ),
      );
      final handle = tester.ensureSemantics();
      await pumpApp(
        tester,
        server: server,
        signedIn: true,
        size: const Size(320, 480),
        textScale: 2,
      );
      await _openProfile(tester);
      expect(tester.takeException(), isNull);

      final action = find.byTooltip(l10n.profileSeePublicButton);
      expect(action.hitTestable(), findsOneWidget);
      expect(
        tester.getSemantics(find.byIcon(Icons.visibility_outlined)),
        isSemantics(
          isButton: true,
          hasTapAction: true,
          tooltip: l10n.profileSeePublicButton,
        ),
      );
      expect(
        tester
            .getSize(find.widgetWithIcon(IconButton, Icons.visibility_outlined))
            .shortestSide,
        greaterThanOrEqualTo(48),
      );

      await tester.tap(action);
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      // The public profile reads its picture as the member's, by name, and
      // its name and section as headings.
      expect(
        tester.getSemantics(find.byType(ProfileAvatar)),
        isSemantics(
          isImage: true,
          label: l10n.memberAvatarPlaceholderLabel(_longName),
        ),
      );
      expect(
        tester.getSemantics(find.text(_longName)),
        isSemantics(label: _longName, isHeader: true),
      );
      expect(
        tester.getSemantics(find.text(l10n.languagesHeading)),
        isSemantics(label: l10n.languagesHeading, isHeader: true),
      );
      expect(find.byType(BackButton).hitTestable(), findsOneWidget);
      handle.dispose();
    });

    testWidgets('the member menu is a labelled 48 dp control, its items '
        'are buttons, and the blocked text is read', (tester) async {
      final server = FakeServer();
      _otherMember(server, block: (_) => noContent());
      final handle = tester.ensureSemantics();
      await pumpApp(
        tester,
        server: server,
        signedIn: true,
        size: const Size(320, 480),
        textScale: 2,
      );
      await _openOther(tester);
      expect(tester.takeException(), isNull);

      final menu = find.byTooltip(l10n.memberMenuTooltip);
      expect(menu.hitTestable(), findsOneWidget);
      expect(
        tester.getSemantics(find.byIcon(Icons.adaptive.more)),
        isSemantics(
          isButton: true,
          hasTapAction: true,
          tooltip: l10n.memberMenuTooltip,
        ),
      );
      expect(
        tester
            .getSize(find.widgetWithIcon(IconButton, Icons.adaptive.more))
            .shortestSide,
        greaterThanOrEqualTo(48),
      );
      // The way back is beside it.
      expect(find.byType(BackButton).hitTestable(), findsOneWidget);

      await tapAndSettle(tester, menu);
      for (final label in [l10n.memberMenuReport, l10n.memberMenuBlock]) {
        final node = tester.getSemantics(find.text(label)).getSemanticsData();
        expect(node.label, label);
        expect(node.flagsCollection.isButton, isTrue, reason: label);
      }
      await tapAndSettle(tester, find.text(l10n.memberMenuBlock));
      for (final label in [l10n.memberBlockCancel, l10n.memberBlockConfirm]) {
        expect(
          tester.getSize(find.widgetWithText(TextButton, label)).shortestSide,
          greaterThanOrEqualTo(48),
          reason: label,
        );
      }
      await tapAndSettle(
        tester,
        find.widgetWithText(TextButton, l10n.memberBlockConfirm),
      );
      expect(tester.takeException(), isNull);
      expect(
        tester.getSemantics(find.text(l10n.memberBlocked)),
        isSemantics(label: l10n.memberBlocked),
      );
      expect(find.byTooltip(l10n.memberMenuTooltip), findsNothing);
      expect(find.byType(BackButton).hitTestable(), findsOneWidget);
      // The way to undo it is a labelled 48 dp button.
      final unblock = find.widgetWithText(OutlinedButton, l10n.unblockButton);
      expect(
        tester.getSemantics(unblock),
        isSemantics(
          label: l10n.unblockButton,
          isButton: true,
          hasTapAction: true,
        ),
      );
      expect(tester.getSize(unblock).shortestSide, greaterThanOrEqualTo(48));
      handle.dispose();
    });

    testWidgets('each "Unblock" in the list is a 48 dp button announced with '
        'the name of the member it unblocks, with large text on a small '
        'screen', (tester) async {
      final server = FakeServer();
      _blockedList(server);
      final handle = tester.ensureSemantics();
      await pumpApp(
        tester,
        server: server,
        signedIn: true,
        size: const Size(320, 480),
        textScale: 2,
      );
      await _openBlocked(tester);
      expect(tester.takeException(), isNull);

      final buttons = find.widgetWithText(OutlinedButton, l10n.unblockButton);
      expect(buttons, findsNWidgets(2));
      final names = [_longName, _secondLongName];
      for (var i = 0; i < names.length; i++) {
        final button = buttons.at(i);
        await tester.ensureVisible(button);
        await tester.pumpAndSettle();
        expect(button.hitTestable(), findsOneWidget, reason: names[i]);
        expect(
          tester.getSemantics(button),
          isSemantics(
            label: l10n.blockedMembersUnblockLabel(names[i]),
            isButton: true,
            hasTapAction: true,
          ),
        );
        expect(tester.getSize(button).shortestSide, greaterThanOrEqualTo(48));
        // The name is read whole, and nothing is cut off beside it.
        expect(
          tester.getSize(find.text(names[i])).width,
          lessThanOrEqualTo(320),
        );
      }
      // The confirmation names the member too.
      await tapAndSettle(tester, buttons.first);
      expect(
        find.text(l10n.blockedMembersUnblockMessage(_longName)),
        findsOneWidget,
      );
      for (final label in [l10n.unblockCancel, l10n.unblockButton]) {
        expect(
          tester.getSize(find.widgetWithText(TextButton, label)).shortestSide,
          greaterThanOrEqualTo(48),
          reason: label,
        );
      }
      expect(tester.takeException(), isNull);
      handle.dispose();
    });

    testWidgets('the reasons of a report are one choice: each a 48 dp '
        'option announced with whether it is chosen, with large text on a '
        'small screen and the keyboard open', (tester) async {
      final server = FakeServer();
      _report(server, send: neverAnswers);
      final handle = tester.ensureSemantics();
      await pumpApp(
        tester,
        server: server,
        signedIn: true,
        size: const Size(320, 480),
        textScale: 2,
        keyboard: 240,
      );
      await _openReport(tester);
      expect(tester.takeException(), isNull);

      expect(
        tester.getSemantics(find.text(l10n.reportReasonHeading)),
        isSemantics(label: l10n.reportReasonHeading, isHeader: true),
      );
      Finder radio(String label) =>
          find.widgetWithText(RadioListTile<ReportReason>, label);
      Future<void> expectReasons({String? chosen, bool enabled = true}) async {
        for (final label in _reportReasons) {
          await tester.ensureVisible(radio(label));
          await tester.pump(const Duration(seconds: 1));
          expect(
            tester.getSemantics(radio(label)),
            isSemantics(
              label: label,
              hasCheckedState: true,
              isChecked: label == chosen,
              isInMutuallyExclusiveGroup: true,
              hasEnabledState: true,
              isEnabled: enabled,
              hasTapAction: enabled,
            ),
            reason: label,
          );
          expect(
            tester.getSize(radio(label)).shortestSide,
            greaterThanOrEqualTo(48),
            reason: label,
          );
          expect(tester.getSize(radio(label)).width, lessThanOrEqualTo(320));
        }
      }

      await expectReasons();
      await tapAndSettle(tester, find.text(l10n.reportReasonSpam));
      await expectReasons(chosen: l10n.reportReasonSpam);
      // The details field is labelled, and the notice is read.
      expect(
        tester
            .getSemantics(field(l10n.reportDetailsLabel))
            .getSemanticsData()
            .label,
        l10n.reportDetailsLabel,
      );
      expect(
        tester.getSemantics(find.text(l10n.reportPrivacyNotice)),
        isSemantics(label: l10n.reportPrivacyNotice),
      );

      // Sending: every reason reads as unavailable, with the choice kept,
      // and the button keeps its label.
      final send = find.widgetWithText(FilledButton, l10n.reportSendButton);
      await tester.ensureVisible(send);
      await tester.pumpAndSettle();
      await tester.tap(send);
      await tester.pump();
      await expectReasons(chosen: l10n.reportReasonSpam, enabled: false);
      expect(
        tester.getSemantics(send),
        isSemantics(
          label: l10n.reportSendButton,
          isButton: true,
          hasEnabledState: true,
          isEnabled: false,
        ),
      );
      expect(tester.takeException(), isNull);
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      handle.dispose();
    });

    testWidgets('screen titles are headers', (tester) async {
      final handle = tester.ensureSemantics();
      await pumpApp(tester);
      expect(
        tester.getSemantics(find.text(l10n.logInTitle).first),
        isSemantics(label: l10n.logInTitle, isHeader: true),
      );
      handle.dispose();
    });

    testWidgets('field errors are part of the field\'s semantics', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      await pumpApp(tester);
      await tapAndSettle(
        tester,
        find.widgetWithText(FilledButton, l10n.logInButton),
      );
      final node = tester.getSemantics(field(l10n.emailLabel));
      final data = node.getSemanticsData();
      expect(
        '${data.label}\n${data.value}\n${data.hint}',
        contains(l10n.emailRequired),
      );
      handle.dispose();
    });

    testWidgets('banners are live regions with a text label for the icon', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final server = FakeServer()
        ..once(
          'POST',
          ApiPaths.login,
          (_) => errorResponse(403, 'email_not_verified'),
        )
        ..once('POST', ApiPaths.resendVerification, networkFailure);
      await pumpApp(tester, server: server);
      await _logInAttempt(tester);
      expect(
        tester.getSemantics(find.byType(FormNoticeBanner)),
        isSemantics(
          isLiveRegion: true,
          label:
              '${l10n.noticeLabel}\n'
              '${l10n.emailNotVerifiedNotice('ana@example.com')}',
        ),
      );
      await tapAndSettle(tester, find.text(l10n.resendVerificationButton));
      expect(
        tester.getSemantics(find.byType(FormErrorBanner)),
        isSemantics(
          isLiveRegion: true,
          label: '${l10n.errorLabel}\n${l10n.errorNetwork}',
        ),
      );
      handle.dispose();
    });

    testWidgets('a failed profile save is announced like the other banners', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final server = FakeServer();
      _newProfile(server, save: networkFailure);
      await pumpApp(tester, server: server, signedIn: true);
      await _saveProfile(tester);
      expect(
        tester.getSemantics(find.byType(FormErrorBanner)),
        isSemantics(
          isLiveRegion: true,
          label: '${l10n.errorLabel}\n${l10n.errorNetwork}',
        ),
      );
      handle.dispose();
    });

    testWidgets('the picture control says whether there is a picture, and '
        'names each of its buttons', (tester) async {
      final handle = tester.ensureSemantics();
      final server = FakeServer();
      _savedProfile(server);
      server
        ..once('PUT', ApiPaths.myAvatar, (_) => imageResponse(testPicture))
        ..once('DELETE', ApiPaths.myAvatar, networkFailure);
      final photos = FakePhotoSource()..next(const [1, 2, 3]);
      await pumpApp(tester, server: server, photos: photos, signedIn: true);
      await _openForm(tester);

      expect(
        tester.getSemantics(find.byType(ProfileAvatar)),
        isSemantics(isImage: true, label: l10n.profileAvatarPlaceholderLabel),
      );
      final add = find.widgetWithText(
        OutlinedButton,
        l10n.profileAvatarAddButton,
      );
      expect(
        tester.getSemantics(add),
        isSemantics(
          label: l10n.profileAvatarAddButton,
          isButton: true,
          hasEnabledState: true,
          isEnabled: true,
          hasTapAction: true,
          isFocusable: true,
        ),
      );

      await tapAndSettle(tester, add);
      expect(
        tester.getSemantics(find.byType(ProfileAvatar)),
        isSemantics(isImage: true, label: l10n.profileAvatarLabel),
      );
      for (final label in [
        l10n.profileAvatarChangeButton,
        l10n.profileAvatarRemoveButton,
      ]) {
        expect(
          tester.getSemantics(find.widgetWithText(OutlinedButton, label)),
          isSemantics(
            label: label,
            isButton: true,
            hasEnabledState: true,
            isEnabled: true,
            hasTapAction: true,
            isFocusable: true,
          ),
        );
      }

      // A failed picture action is announced like the other banners.
      await tapAndSettle(
        tester,
        find.widgetWithText(OutlinedButton, l10n.profileAvatarRemoveButton),
      );
      await tapAndSettle(tester, find.text(l10n.profileAvatarRemoveConfirm));
      expect(
        tester.getSemantics(find.byType(FormErrorBanner)),
        isSemantics(
          isLiveRegion: true,
          label: '${l10n.errorLabel}\n${l10n.errorNetwork}',
        ),
      );
      handle.dispose();
    });

    testWidgets('the password toggle is labelled in both states', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      await pumpApp(tester);
      expect(find.byTooltip(l10n.showPassword), findsOneWidget);
      await tester.tap(find.byTooltip(l10n.showPassword));
      await tester.pump();
      expect(find.byTooltip(l10n.hidePassword), findsOneWidget);
      handle.dispose();
    });

    testWidgets('a busy button keeps its label for screen readers', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final server = FakeServer()..once('POST', ApiPaths.login, neverAnswers);
      await pumpApp(tester, server: server);
      await tester.enterText(field(l10n.emailLabel), 'ana@example.com');
      await tester.enterText(field(l10n.passwordLabel), 'correct horse');
      await tester.tap(find.widgetWithText(FilledButton, l10n.logInButton));
      await tester.pump();
      expect(
        tester.getSemantics(find.byType(FilledButton)),
        isSemantics(
          label: l10n.logInButton,
          isButton: true,
          hasEnabledState: true,
          isEnabled: false,
        ),
      );
      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      handle.dispose();
    });

    testWidgets('a running Google sign-in is announced, and its button reads '
        'as disabled', (tester) async {
      final handle = tester.ensureSemantics();
      final chooser = Completer<String>();
      final google = FakeGoogleIdentity()..wait(chooser);
      await pumpApp(tester, google: google);
      await tester.ensureVisible(find.byType(GoogleSignInButton));
      await tester.tap(find.byType(GoogleSignInButton));
      await tester.pump();

      expect(find.bySemanticsLabel(l10n.googleSignInProgress), findsOneWidget);
      expect(
        tester.getSemantics(find.byType(OutlinedButton)),
        isSemantics(
          label: l10n.continueWithGoogle,
          isButton: true,
          hasEnabledState: true,
          isEnabled: false,
        ),
      );
      await expectLater(tester, meetsGuideline(textContrastGuideline));

      chooser.completeError(
        const GoogleIdentityException(GoogleIdentityFailure.cancelled),
      );
      await tester.pumpAndSettle();
      handle.dispose();
    });
  });
}

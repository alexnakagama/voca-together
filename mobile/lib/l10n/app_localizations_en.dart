// ignore: unused_import
import 'package:intl/intl.dart' as intl;

import 'app_localizations.dart';

// ignore_for_file: type=lint

/// The translations for English (`en`).
class AppLocalizationsEn extends AppLocalizations {
  AppLocalizationsEn([String locale = 'en']) : super(locale);

  @override
  String get appTitle => 'VocaTogether';

  @override
  String get emailLabel => 'Email';

  @override
  String get passwordLabel => 'Password';

  @override
  String get showPassword => 'Show password';

  @override
  String get hidePassword => 'Hide password';

  @override
  String get continueWithGoogle => 'Continue with Google';

  @override
  String get errorLabel => 'Error';

  @override
  String get noticeLabel => 'Notice';

  @override
  String get splashLoading => 'Loading';

  @override
  String get backToLogIn => 'Back to log in';

  @override
  String get tryAgain => 'Try again';

  @override
  String get logInTitle => 'Log in';

  @override
  String get logInButton => 'Log in';

  @override
  String get forgotPasswordLink => 'Forgot password?';

  @override
  String get createAccountLink => 'Create an account';

  @override
  String emailNotVerifiedNotice(String email) {
    return 'Verify your email address to log in. Open the link we sent to $email, then log in again.';
  }

  @override
  String get resendVerificationButton => 'Send a new verification email';

  @override
  String resendVerificationSent(String email) {
    return 'If $email is waiting to be verified, we’ve sent a new link. It can take a few minutes to arrive.';
  }

  @override
  String get registerTitle => 'Create account';

  @override
  String get registerButton => 'Create account';

  @override
  String get confirmPasswordLabel => 'Confirm password';

  @override
  String get haveAccountLink => 'Already have an account? Log in';

  @override
  String get checkEmailTitle => 'Check your email';

  @override
  String registerSent(String email) {
    return 'We’ve sent a message to $email with the next steps. Open it to finish setting up your account, then log in.';
  }

  @override
  String get forgotPasswordTitle => 'Reset your password';

  @override
  String get forgotPasswordIntro =>
      'Enter the email address you use for VocaTogether and we’ll send you an email with the next steps.';

  @override
  String get forgotPasswordButton => 'Send reset link';

  @override
  String forgotPasswordSent(String email) {
    return 'If there’s a VocaTogether account for $email, we’ve sent it an email with the next steps. Open the link in that email to choose a new password, then log in here.';
  }

  @override
  String get homeLoading => 'Loading your account';

  @override
  String get homeSignedInTitle => 'You’re signed in';

  @override
  String homeSignedInAs(String email) {
    return 'Signed in as $email';
  }

  @override
  String homeMemberSince(DateTime date) {
    final intl.DateFormat dateDateFormat = intl.DateFormat.yMMMMd(localeName);
    final String dateString = dateDateFormat.format(date);

    return 'Member since $dateString';
  }

  @override
  String get logOutButton => 'Log out';

  @override
  String get emailRequired => 'Enter your email address';

  @override
  String get passwordRequired => 'Enter your password';

  @override
  String get confirmPasswordRequired => 'Enter your password again';

  @override
  String get passwordsDoNotMatch => 'The passwords don’t match';

  @override
  String get errorEmailInvalid => 'Enter a valid email address';

  @override
  String get errorPasswordTooShort =>
      'This password is too short. Choose a longer one.';

  @override
  String get errorPasswordTooLong =>
      'This password is too long. Choose a shorter one.';

  @override
  String get errorPasswordTooCommon =>
      'This password is too common. Choose one that’s harder to guess.';

  @override
  String get errorPasswordSameAsEmail =>
      'Your password can’t be your email address.';

  @override
  String get errorCheckInput => 'Check the details you entered and try again.';

  @override
  String get errorInvalidCredentials => 'Incorrect email or password.';

  @override
  String get errorEmailNotVerified =>
      'Verify your email address before logging in.';

  @override
  String errorRateLimited(String wait) {
    return 'Too many attempts. Try again in $wait.';
  }

  @override
  String get errorRateLimitedNoWait =>
      'Too many attempts. Please wait a moment and try again.';

  @override
  String errorUnavailable(String wait) {
    return 'VocaTogether is busy right now. Try again in $wait.';
  }

  @override
  String get errorUnavailableNoWait =>
      'VocaTogether is busy right now. Please try again in a moment.';

  @override
  String get errorSessionInvalid =>
      'We couldn’t confirm your session. Try again.';

  @override
  String get errorUnexpected =>
      'Something went wrong on our side. Please try again.';

  @override
  String get errorNetwork =>
      'Couldn’t connect. Check your internet connection and try again.';

  @override
  String get errorTimeout =>
      'The connection timed out. Check your internet connection and try again.';

  @override
  String waitSeconds(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count seconds',
      one: '1 second',
    );
    return '$_temp0';
  }

  @override
  String waitMinutes(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count minutes',
      one: '1 minute',
    );
    return '$_temp0';
  }

  @override
  String waitHours(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count hours',
      one: '1 hour',
    );
    return '$_temp0';
  }

  @override
  String get googleSignInDivider => 'or';

  @override
  String get googleSignInProgress => 'Signing in with Google';

  @override
  String get errorGoogleUnavailable =>
      'Google sign-in isn’t available right now. Try again.';

  @override
  String get errorGoogleRejected => 'Google sign-in didn’t work. Try again.';

  @override
  String get errorGoogleEmailUnusable =>
      'This Google account can’t be used: Google hasn’t confirmed its email address.';

  @override
  String get errorAccountExists =>
      'There’s already a VocaTogether account for this Google account’s email. Log in the way that account was set up.';

  @override
  String get profileButton => 'Profile';

  @override
  String get profileTitle => 'Your profile';

  @override
  String get profileLoading => 'Loading your profile';

  @override
  String get profileCreateHeading => 'Create your profile';

  @override
  String get profileEditHeading => 'Edit your profile';

  @override
  String get profileEmptyMessage =>
      'You haven’t created your profile yet. Add your name so other members can get to know you.';

  @override
  String get profileEmptyButton => 'Create your profile';

  @override
  String get profileEditButton => 'Edit Profile';

  @override
  String get profileAvatarPlaceholderLabel =>
      'Your profile picture: no photo yet';

  @override
  String get profileAvatarLabel => 'Your profile picture';

  @override
  String get profileAvatarLoading => 'Loading your profile picture';

  @override
  String get profileAvatarAddButton => 'Add photo';

  @override
  String get profileAvatarChangeButton => 'Change photo';

  @override
  String get profileAvatarRemoveButton => 'Remove photo';

  @override
  String get profileAvatarRemoveTitle => 'Remove your photo?';

  @override
  String get profileAvatarRemoveMessage =>
      'Other members will no longer see it.';

  @override
  String get profileAvatarRemoveConfirm => 'Remove';

  @override
  String get profileAvatarRemoveKeep => 'Keep photo';

  @override
  String get profileFriendsHeading => 'Friends';

  @override
  String get profileFriendsComingLater => 'Coming later';

  @override
  String get profileSeePublicButton => 'See public profile';

  @override
  String get memberProfileTitle => 'Profile';

  @override
  String get memberProfileLoading => 'Loading the profile';

  @override
  String get memberProfileUnavailable => 'This profile isn’t available.';

  @override
  String memberAvatarLabel(String name) {
    return '$name’s profile picture';
  }

  @override
  String memberAvatarPlaceholderLabel(String name) {
    return '$name’s profile picture: no photo';
  }

  @override
  String get memberLanguagesEmpty => 'No languages added yet.';

  @override
  String get memberMenuTooltip => 'More options';

  @override
  String get memberMenuReport => 'Report';

  @override
  String get memberMenuBlock => 'Block';

  @override
  String get reportTitle => 'Report member';

  @override
  String get reportReasonHeading => 'Why are you reporting this member?';

  @override
  String get reportReasonHarassment => 'Harassment or bullying';

  @override
  String get reportReasonInappropriateContent => 'Inappropriate content';

  @override
  String get reportReasonSpam => 'Spam';

  @override
  String get reportReasonImpersonation => 'Pretending to be someone else';

  @override
  String get reportReasonOther => 'Something else';

  @override
  String get reportDetailsLabel => 'Details (optional)';

  @override
  String get reportPrivacyNotice =>
      'Your report is private. This member won’t be told who reported them.';

  @override
  String get reportSendButton => 'Send report';

  @override
  String get reportSent =>
      'Your report was sent. You can also block this member from their profile.';

  @override
  String get reportBackToProfile => 'Back to profile';

  @override
  String get memberBlockTitle => 'Block this member?';

  @override
  String get memberBlockMessage =>
      'You won’t see each other’s profiles. They won’t be told that you blocked them.';

  @override
  String get memberBlockConfirm => 'Block';

  @override
  String get memberBlockCancel => 'Cancel';

  @override
  String get memberBlockProgress => 'Blocking the member';

  @override
  String get memberBlocked =>
      'You’ve blocked this member. “Unblock” undoes it.';

  @override
  String get unblockTitle => 'Unblock this member?';

  @override
  String get memberUnblockMessage =>
      'Your block will be removed. They won’t be told.';

  @override
  String get unblockButton => 'Unblock';

  @override
  String get unblockCancel => 'Cancel';

  @override
  String get unblockProgress => 'Unblocking the member';

  @override
  String get blockedMembersButton => 'Blocked members';

  @override
  String blockedMembersUnblockMessage(String name) {
    return 'You and $name will be able to see each other’s profiles again. They won’t be told.';
  }

  @override
  String get blockedMembersTitle => 'Blocked members';

  @override
  String get blockedMembersLoading => 'Loading blocked members';

  @override
  String get blockedMembersEmpty => 'You haven’t blocked anyone.';

  @override
  String blockedMembersUnblockLabel(String name) {
    return 'Unblock $name';
  }

  @override
  String get profileVisibilityNotice =>
      'Other members will be able to see your name, your picture and what you write about yourself. Your email address stays private.';

  @override
  String get displayNameLabel => 'Name';

  @override
  String get bioLabel => 'About you (optional)';

  @override
  String get profileSaveButton => 'Save';

  @override
  String get profileCancelButton => 'Cancel';

  @override
  String get profileEditLanguagesButton => 'Languages';

  @override
  String get profileDiscardMessage =>
      'Your changes to your profile haven’t been saved.';

  @override
  String get displayNameRequired => 'Enter your name';

  @override
  String get errorDisplayNameTooLong =>
      'This name is too long. Use a shorter one.';

  @override
  String get errorDisplayNameInvalid =>
      'This name can’t be used. It needs at least one letter or number and can’t contain hidden or special control characters.';

  @override
  String get errorBioTooLong => 'This text is too long. Make it shorter.';

  @override
  String get errorBioInvalid =>
      'This text contains characters that can’t be used. Remove them and try again.';

  @override
  String get errorLanguagesTooMany =>
      'This list has too many languages. Remove some and try again.';

  @override
  String get errorLanguageUnknown =>
      'One of these languages isn’t available. Remove it and try again.';

  @override
  String get errorLanguageLevelInvalid =>
      'One of these levels can’t be used here. Choose a different level and try again.';

  @override
  String get errorLanguageDuplicate =>
      'A language can only be added once, in one of the two lists. Remove the repeated one and try again.';

  @override
  String get errorAvatarRequired =>
      'That photo is empty. Choose a different one.';

  @override
  String get errorAvatarTooLarge =>
      'That photo’s file is too large. Choose a smaller one.';

  @override
  String get errorAvatarUnsupportedType =>
      'That kind of file can’t be used. Choose a JPEG or PNG photo.';

  @override
  String get errorAvatarInvalidImage =>
      'That photo couldn’t be read and may be damaged. Choose a different one.';

  @override
  String get errorAvatarDimensionsTooLarge =>
      'That photo has too many pixels. Choose a smaller one.';

  @override
  String get errorPhotoUnusable =>
      'That photo couldn’t be used. Choose a different one.';

  @override
  String get errorReportReasonRequired => 'Choose a reason for the report.';

  @override
  String get errorReportReasonInvalid =>
      'That reason can’t be used. Choose a different one.';

  @override
  String get errorReportDetailsTooLong =>
      'This text is too long. Make it shorter.';

  @override
  String get errorReportDetailsInvalid =>
      'This text contains characters that can’t be used. Remove them and try again.';

  @override
  String get errorBlocksTooMany =>
      'You’ve reached the limit of blocked members. Unblock someone to make room.';

  @override
  String get languagesHeading => 'Languages';

  @override
  String get languagesLoading => 'Loading your languages';

  @override
  String get languagesEmpty => 'You haven’t added any languages yet.';

  @override
  String get languagesSpokenHeading => 'I speak';

  @override
  String get languagesLearningHeading => 'I’m learning';

  @override
  String get languageLevelA1 => 'A1';

  @override
  String get languageLevelA2 => 'A2';

  @override
  String get languageLevelB1 => 'B1';

  @override
  String get languageLevelB2 => 'B2';

  @override
  String get languageLevelC1 => 'C1';

  @override
  String get languageLevelC2 => 'C2';

  @override
  String get languageLevelNative => 'Native';

  @override
  String get languagesEditorTitle => 'Your languages';

  @override
  String get languagesVisibilityNotice =>
      'Other members will be able to see the languages you speak and are learning, and your level in each.';

  @override
  String get languagesAddButton => 'Add a language';

  @override
  String get languagesSaveButton => 'Save';

  @override
  String get languagesCancelButton => 'Cancel';

  @override
  String get languagesDiscardTitle => 'Discard changes?';

  @override
  String get languagesDiscardMessage =>
      'Your changes to your languages haven’t been saved.';

  @override
  String get languagesDiscardConfirm => 'Discard';

  @override
  String get languagesDiscardKeep => 'Keep editing';

  @override
  String get languagePickerTitle => 'Choose a language';

  @override
  String get languagePickerSearchLabel => 'Search';

  @override
  String get languagePickerNoMatch => 'No language matches your search.';

  @override
  String languageLevelPickerTitle(String name) {
    return 'Your level in $name';
  }

  @override
  String languageLevelSemantics(String name, String level) {
    return '$name, level $level';
  }

  @override
  String languageRemove(String name) {
    return 'Remove $name';
  }

  @override
  String languageMoveUp(String name) {
    return 'Move $name up';
  }

  @override
  String languageMoveDown(String name) {
    return 'Move $name down';
  }

  @override
  String get languageLevelDescriptionA1 => 'Beginner';

  @override
  String get languageLevelDescriptionA2 => 'Elementary';

  @override
  String get languageLevelDescriptionB1 => 'Intermediate';

  @override
  String get languageLevelDescriptionB2 => 'Upper intermediate';

  @override
  String get languageLevelDescriptionC1 => 'Advanced';

  @override
  String get languageLevelDescriptionC2 => 'Proficient';

  @override
  String get languageLevelDescriptionNative => 'Native speaker';
}

import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:intl/intl.dart' as intl;

import 'app_localizations_en.dart';

// ignore_for_file: type=lint

/// Callers can lookup localized strings with an instance of AppLocalizations
/// returned by `AppLocalizations.of(context)`.
///
/// Applications need to include `AppLocalizations.delegate()` in their app's
/// `localizationDelegates` list, and the locales they support in the app's
/// `supportedLocales` list. For example:
///
/// ```dart
/// import 'l10n/app_localizations.dart';
///
/// return MaterialApp(
///   localizationsDelegates: AppLocalizations.localizationsDelegates,
///   supportedLocales: AppLocalizations.supportedLocales,
///   home: MyApplicationHome(),
/// );
/// ```
///
/// ## Update pubspec.yaml
///
/// Please make sure to update your pubspec.yaml to include the following
/// packages:
///
/// ```yaml
/// dependencies:
///   # Internationalization support.
///   flutter_localizations:
///     sdk: flutter
///   intl: any # Use the pinned version from flutter_localizations
///
///   # Rest of dependencies
/// ```
///
/// ## iOS Applications
///
/// iOS applications define key application metadata, including supported
/// locales, in an Info.plist file that is built into the application bundle.
/// To configure the locales supported by your app, you’ll need to edit this
/// file.
///
/// First, open your project’s ios/Runner.xcworkspace Xcode workspace file.
/// Then, in the Project Navigator, open the Info.plist file under the Runner
/// project’s Runner folder.
///
/// Next, select the Information Property List item, select Add Item from the
/// Editor menu, then select Localizations from the pop-up menu.
///
/// Select and expand the newly-created Localizations item then, for each
/// locale your application supports, add a new item and select the locale
/// you wish to add from the pop-up menu in the Value field. This list should
/// be consistent with the languages listed in the AppLocalizations.supportedLocales
/// property.
abstract class AppLocalizations {
  AppLocalizations(String locale)
    : localeName = intl.Intl.canonicalizedLocale(locale.toString());

  final String localeName;

  static AppLocalizations of(BuildContext context) {
    return Localizations.of<AppLocalizations>(context, AppLocalizations)!;
  }

  static const LocalizationsDelegate<AppLocalizations> delegate =
      _AppLocalizationsDelegate();

  /// A list of this localizations delegate along with the default localizations
  /// delegates.
  ///
  /// Returns a list of localizations delegates containing this delegate along with
  /// GlobalMaterialLocalizations.delegate, GlobalCupertinoLocalizations.delegate,
  /// and GlobalWidgetsLocalizations.delegate.
  ///
  /// Additional delegates can be added by appending to this list in
  /// MaterialApp. This list does not have to be used at all if a custom list
  /// of delegates is preferred or required.
  static const List<LocalizationsDelegate<dynamic>> localizationsDelegates =
      <LocalizationsDelegate<dynamic>>[
        delegate,
        GlobalMaterialLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
      ];

  /// A list of this localizations delegate's supported locales.
  static const List<Locale> supportedLocales = <Locale>[Locale('en')];

  /// The app's name, shown by Android in the recent-apps switcher.
  ///
  /// In en, this message translates to:
  /// **'VocaTogether'**
  String get appTitle;

  /// Label of an email address text field.
  ///
  /// In en, this message translates to:
  /// **'Email'**
  String get emailLabel;

  /// Label of a password text field.
  ///
  /// In en, this message translates to:
  /// **'Password'**
  String get passwordLabel;

  /// Tooltip and accessibility label of the button that reveals the password being typed.
  ///
  /// In en, this message translates to:
  /// **'Show password'**
  String get showPassword;

  /// Tooltip and accessibility label of the button that hides the password being typed again.
  ///
  /// In en, this message translates to:
  /// **'Hide password'**
  String get hidePassword;

  /// Text of the Sign in with Google button. Must be one of Google's approved button texts (or its official translation).
  ///
  /// In en, this message translates to:
  /// **'Continue with Google'**
  String get continueWithGoogle;

  /// Accessibility label of the icon in front of a form-level error message.
  ///
  /// In en, this message translates to:
  /// **'Error'**
  String get errorLabel;

  /// Accessibility label of the icon in front of a neutral notice, such as a confirmation that an email was sent.
  ///
  /// In en, this message translates to:
  /// **'Notice'**
  String get noticeLabel;

  /// Accessibility label of the progress indicator shown while the app starts.
  ///
  /// In en, this message translates to:
  /// **'Loading'**
  String get splashLoading;

  /// Button that returns to the log-in screen.
  ///
  /// In en, this message translates to:
  /// **'Back to log in'**
  String get backToLogIn;

  /// Button that repeats a request that failed.
  ///
  /// In en, this message translates to:
  /// **'Try again'**
  String get tryAgain;

  /// Title of the log-in screen.
  ///
  /// In en, this message translates to:
  /// **'Log in'**
  String get logInTitle;

  /// Button that submits the log-in form.
  ///
  /// In en, this message translates to:
  /// **'Log in'**
  String get logInButton;

  /// Link from the log-in screen to the screen that sends a password reset email.
  ///
  /// In en, this message translates to:
  /// **'Forgot password?'**
  String get forgotPasswordLink;

  /// Link from the log-in screen to the registration screen.
  ///
  /// In en, this message translates to:
  /// **'Create an account'**
  String get createAccountLink;

  /// Shown on the log-in screen when the password was right but the address is not verified yet. {email} is the address the user typed.
  ///
  /// In en, this message translates to:
  /// **'Verify your email address to log in. Open the link we sent to {email}, then log in again.'**
  String emailNotVerifiedNotice(String email);

  /// Button that asks for a new email address verification link.
  ///
  /// In en, this message translates to:
  /// **'Send a new verification email'**
  String get resendVerificationButton;

  /// Confirmation after asking for a new verification link. Must not say whether an account exists. {email} is the address the user typed.
  ///
  /// In en, this message translates to:
  /// **'If {email} is waiting to be verified, we’ve sent a new link. It can take a few minutes to arrive.'**
  String resendVerificationSent(String email);

  /// Title of the registration screen.
  ///
  /// In en, this message translates to:
  /// **'Create account'**
  String get registerTitle;

  /// Button that submits the registration form.
  ///
  /// In en, this message translates to:
  /// **'Create account'**
  String get registerButton;

  /// Label of the field where the user types the new password a second time.
  ///
  /// In en, this message translates to:
  /// **'Confirm password'**
  String get confirmPasswordLabel;

  /// Link from the registration screen to the log-in screen.
  ///
  /// In en, this message translates to:
  /// **'Already have an account? Log in'**
  String get haveAccountLink;

  /// Title shown after the registration form was accepted.
  ///
  /// In en, this message translates to:
  /// **'Check your email'**
  String get checkEmailTitle;

  /// Shown after the registration form was accepted. Must not say whether a new account was created. {email} is the address the user typed.
  ///
  /// In en, this message translates to:
  /// **'We’ve sent a message to {email} with the next steps. Open it to finish setting up your account, then log in.'**
  String registerSent(String email);

  /// Title of the screen that sends a password reset email.
  ///
  /// In en, this message translates to:
  /// **'Reset your password'**
  String get forgotPasswordTitle;

  /// Explanation at the top of the password reset request screen.
  ///
  /// In en, this message translates to:
  /// **'Enter the email address you use for VocaTogether and we’ll send you an email with the next steps.'**
  String get forgotPasswordIntro;

  /// Button that asks for a password reset email.
  ///
  /// In en, this message translates to:
  /// **'Send reset link'**
  String get forgotPasswordButton;

  /// Shown after a password reset was requested. Must read the same whether or not the account exists. {email} is the address the user typed.
  ///
  /// In en, this message translates to:
  /// **'If there’s a VocaTogether account for {email}, we’ve sent it an email with the next steps. Open the link in that email to choose a new password, then log in here.'**
  String forgotPasswordSent(String email);

  /// Accessibility label of the progress indicator while the signed-in account loads.
  ///
  /// In en, this message translates to:
  /// **'Loading your account'**
  String get homeLoading;

  /// Heading of the home screen once the account has loaded.
  ///
  /// In en, this message translates to:
  /// **'You’re signed in'**
  String get homeSignedInTitle;

  /// The signed-in account’s email address on the home screen.
  ///
  /// In en, this message translates to:
  /// **'Signed in as {email}'**
  String homeSignedInAs(String email);

  /// When the account was created, on the home screen.
  ///
  /// In en, this message translates to:
  /// **'Member since {date}'**
  String homeMemberSince(DateTime date);

  /// Button that signs the user out.
  ///
  /// In en, this message translates to:
  /// **'Log out'**
  String get logOutButton;

  /// Field error when the email field was left empty.
  ///
  /// In en, this message translates to:
  /// **'Enter your email address'**
  String get emailRequired;

  /// Field error when the password field was left empty.
  ///
  /// In en, this message translates to:
  /// **'Enter your password'**
  String get passwordRequired;

  /// Field error when the password confirmation field was left empty.
  ///
  /// In en, this message translates to:
  /// **'Enter your password again'**
  String get confirmPasswordRequired;

  /// Field error when the confirmation differs from the password.
  ///
  /// In en, this message translates to:
  /// **'The passwords don’t match'**
  String get passwordsDoNotMatch;

  /// Field error when the server refused the email address.
  ///
  /// In en, this message translates to:
  /// **'Enter a valid email address'**
  String get errorEmailInvalid;

  /// Field error when the server refused a new password as too short. Gives no number on purpose: the rule belongs to the server.
  ///
  /// In en, this message translates to:
  /// **'This password is too short. Choose a longer one.'**
  String get errorPasswordTooShort;

  /// Field error when the server refused a new password as too long. Gives no number on purpose.
  ///
  /// In en, this message translates to:
  /// **'This password is too long. Choose a shorter one.'**
  String get errorPasswordTooLong;

  /// Field error when the server refused a new password as too common.
  ///
  /// In en, this message translates to:
  /// **'This password is too common. Choose one that’s harder to guess.'**
  String get errorPasswordTooCommon;

  /// Field error when the new password equals the email address.
  ///
  /// In en, this message translates to:
  /// **'Your password can’t be your email address.'**
  String get errorPasswordSameAsEmail;

  /// Form error when the server refused the input for a reason the app cannot show on a field.
  ///
  /// In en, this message translates to:
  /// **'Check the details you entered and try again.'**
  String get errorCheckInput;

  /// Form error when logging in failed. Must not say which of the two was wrong.
  ///
  /// In en, this message translates to:
  /// **'Incorrect email or password.'**
  String get errorInvalidCredentials;

  /// Form error when an unverified account tries to log in.
  ///
  /// In en, this message translates to:
  /// **'Verify your email address before logging in.'**
  String get errorEmailNotVerified;

  /// Form error when the server limits requests. {wait} is a duration such as "2 minutes".
  ///
  /// In en, this message translates to:
  /// **'Too many attempts. Try again in {wait}.'**
  String errorRateLimited(String wait);

  /// Form error when the server limits requests and gives no waiting time.
  ///
  /// In en, this message translates to:
  /// **'Too many attempts. Please wait a moment and try again.'**
  String get errorRateLimitedNoWait;

  /// Form error when the server is temporarily overloaded. {wait} is a duration such as "5 seconds".
  ///
  /// In en, this message translates to:
  /// **'VocaTogether is busy right now. Try again in {wait}.'**
  String errorUnavailable(String wait);

  /// Form error when the server is temporarily overloaded and gives no waiting time.
  ///
  /// In en, this message translates to:
  /// **'VocaTogether is busy right now. Please try again in a moment.'**
  String get errorUnavailableNoWait;

  /// Error on a signed-in screen when the server refused the session.
  ///
  /// In en, this message translates to:
  /// **'We couldn’t confirm your session. Try again.'**
  String get errorSessionInvalid;

  /// Generic error for server failures and unexpected responses.
  ///
  /// In en, this message translates to:
  /// **'Something went wrong on our side. Please try again.'**
  String get errorUnexpected;

  /// Error when the server could not be reached.
  ///
  /// In en, this message translates to:
  /// **'Couldn’t connect. Check your internet connection and try again.'**
  String get errorNetwork;

  /// Error when the server took too long to answer.
  ///
  /// In en, this message translates to:
  /// **'The connection timed out. Check your internet connection and try again.'**
  String get errorTimeout;

  /// A waiting time in seconds, used inside error messages.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 second} other{{count} seconds}}'**
  String waitSeconds(int count);

  /// A waiting time in minutes, used inside error messages.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 minute} other{{count} minutes}}'**
  String waitMinutes(int count);

  /// A waiting time in hours, used inside error messages.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 hour} other{{count} hours}}'**
  String waitHours(int count);

  /// Separates the email and password form from the Continue with Google button.
  ///
  /// In en, this message translates to:
  /// **'or'**
  String get googleSignInDivider;

  /// Accessibility label of the progress indicator shown while a Google sign-in is running.
  ///
  /// In en, this message translates to:
  /// **'Signing in with Google'**
  String get googleSignInProgress;

  /// Error when Google gave the app no sign-in result (other than the user closing Google's account chooser, which shows nothing).
  ///
  /// In en, this message translates to:
  /// **'Google sign-in isn’t available right now. Try again.'**
  String get errorGoogleUnavailable;

  /// Error when the server did not accept the Google sign-in. Trying again starts a new Google sign-in.
  ///
  /// In en, this message translates to:
  /// **'Google sign-in didn’t work. Try again.'**
  String get errorGoogleRejected;

  /// Error when a Google account can't create a VocaTogether account because Google reports no verified email address for it.
  ///
  /// In en, this message translates to:
  /// **'This Google account can’t be used: Google hasn’t confirmed its email address.'**
  String get errorGoogleEmailUnusable;

  /// Error when signing in with Google and the Google account's email address already belongs to a VocaTogether account that isn't connected to this Google account. Must not mention a password: that account may have none.
  ///
  /// In en, this message translates to:
  /// **'There’s already a VocaTogether account for this Google account’s email. Log in the way that account was set up.'**
  String get errorAccountExists;

  /// Button on the home screen that opens the user’s own profile.
  ///
  /// In en, this message translates to:
  /// **'Profile'**
  String get profileButton;

  /// Title of the user’s own profile page and of the screen where they edit their profile.
  ///
  /// In en, this message translates to:
  /// **'Your profile'**
  String get profileTitle;

  /// Accessibility label of the progress indicator while the user’s profile loads.
  ///
  /// In en, this message translates to:
  /// **'Loading your profile'**
  String get profileLoading;

  /// Heading of the profile form when the user hasn’t saved a profile yet.
  ///
  /// In en, this message translates to:
  /// **'Create your profile'**
  String get profileCreateHeading;

  /// Heading of the profile form when the user already has a profile.
  ///
  /// In en, this message translates to:
  /// **'Edit your profile'**
  String get profileEditHeading;

  /// Text of the user’s own profile page when they haven’t saved a profile yet.
  ///
  /// In en, this message translates to:
  /// **'You haven’t created your profile yet. Add your name so other members can get to know you.'**
  String get profileEmptyMessage;

  /// Button on the user’s own profile page, shown when they haven’t saved a profile yet, that opens the screen where they create it.
  ///
  /// In en, this message translates to:
  /// **'Create your profile'**
  String get profileEmptyButton;

  /// Button on the user’s own profile page that opens the screen where they edit their name, their text, their picture and their languages.
  ///
  /// In en, this message translates to:
  /// **'Edit Profile'**
  String get profileEditButton;

  /// Accessibility label of the placeholder shown on the user’s own profile page and on the profile form in place of a profile picture.
  ///
  /// In en, this message translates to:
  /// **'Your profile picture: no photo yet'**
  String get profileAvatarPlaceholderLabel;

  /// Accessibility label of the user’s own profile picture, on their profile page and on the profile form.
  ///
  /// In en, this message translates to:
  /// **'Your profile picture'**
  String get profileAvatarLabel;

  /// Accessibility label of the progress indicator shown on the profile form while the user’s profile picture loads.
  ///
  /// In en, this message translates to:
  /// **'Loading your profile picture'**
  String get profileAvatarLoading;

  /// Button on the profile form, shown when the user has no profile picture, that opens the device’s photo chooser. The chosen photo becomes their picture at once.
  ///
  /// In en, this message translates to:
  /// **'Add photo'**
  String get profileAvatarAddButton;

  /// Button on the profile form, shown when the user has a profile picture, that opens the device’s photo chooser to replace it. The chosen photo becomes their picture at once.
  ///
  /// In en, this message translates to:
  /// **'Change photo'**
  String get profileAvatarChangeButton;

  /// Button on the profile form, shown when the user has a profile picture, that removes it after a confirmation.
  ///
  /// In en, this message translates to:
  /// **'Remove photo'**
  String get profileAvatarRemoveButton;

  /// Title of the dialog that asks the user to confirm removing their profile picture.
  ///
  /// In en, this message translates to:
  /// **'Remove your photo?'**
  String get profileAvatarRemoveTitle;

  /// Text of the dialog that asks the user to confirm removing their profile picture.
  ///
  /// In en, this message translates to:
  /// **'Other members will no longer see it.'**
  String get profileAvatarRemoveMessage;

  /// Dialog button that removes the user’s profile picture.
  ///
  /// In en, this message translates to:
  /// **'Remove'**
  String get profileAvatarRemoveConfirm;

  /// Dialog button that closes the dialog and keeps the user’s profile picture.
  ///
  /// In en, this message translates to:
  /// **'Keep photo'**
  String get profileAvatarRemoveKeep;

  /// Heading of the friends area of the user’s own profile page. The feature doesn’t exist yet.
  ///
  /// In en, this message translates to:
  /// **'Friends'**
  String get profileFriendsHeading;

  /// Text under the Friends heading of the profile page saying the feature isn’t available yet. Must not state a number or a date.
  ///
  /// In en, this message translates to:
  /// **'Coming later'**
  String get profileFriendsComingLater;

  /// Tooltip and accessibility label of the button on the user’s own profile page that opens their profile as other members see it.
  ///
  /// In en, this message translates to:
  /// **'See public profile'**
  String get profileSeePublicButton;

  /// Title of the screen that shows a member’s public profile, read-only.
  ///
  /// In en, this message translates to:
  /// **'Profile'**
  String get memberProfileTitle;

  /// Accessibility label of the progress indicator while a member’s public profile loads.
  ///
  /// In en, this message translates to:
  /// **'Loading the profile'**
  String get memberProfileLoading;

  /// Text of the member profile screen when the profile doesn’t exist or can no longer be seen. Must not say why.
  ///
  /// In en, this message translates to:
  /// **'This profile isn’t available.'**
  String get memberProfileUnavailable;

  /// Accessibility label of a member’s profile picture on their public profile.
  ///
  /// In en, this message translates to:
  /// **'{name}’s profile picture'**
  String memberAvatarLabel(String name);

  /// Accessibility label of the placeholder shown on a member’s public profile in place of a profile picture.
  ///
  /// In en, this message translates to:
  /// **'{name}’s profile picture: no photo'**
  String memberAvatarPlaceholderLabel(String name);

  /// Text of the languages section of a member’s public profile when that member has chosen no language, spoken or learning.
  ///
  /// In en, this message translates to:
  /// **'No languages added yet.'**
  String get memberLanguagesEmpty;

  /// Tooltip and accessibility label of the menu button in the app bar of another member’s public profile. The menu holds “Report” and “Block”.
  ///
  /// In en, this message translates to:
  /// **'More options'**
  String get memberMenuTooltip;

  /// Menu item on another member’s public profile that opens the form to report that member. Separate from “Block”.
  ///
  /// In en, this message translates to:
  /// **'Report'**
  String get memberMenuReport;

  /// Menu item on another member’s public profile that blocks that member, after a confirmation. Separate from “Report”.
  ///
  /// In en, this message translates to:
  /// **'Block'**
  String get memberMenuBlock;

  /// Title of the screen where the user reports another member. Must not show the member’s name.
  ///
  /// In en, this message translates to:
  /// **'Report member'**
  String get reportTitle;

  /// Heading above the five reasons of the report form, of which the user chooses one.
  ///
  /// In en, this message translates to:
  /// **'Why are you reporting this member?'**
  String get reportReasonHeading;

  /// One of the five reasons of the report form: the member harasses or bullies others.
  ///
  /// In en, this message translates to:
  /// **'Harassment or bullying'**
  String get reportReasonHarassment;

  /// One of the five reasons of the report form: the member’s name, text or picture is inappropriate.
  ///
  /// In en, this message translates to:
  /// **'Inappropriate content'**
  String get reportReasonInappropriateContent;

  /// One of the five reasons of the report form: the member’s profile is spam or advertising.
  ///
  /// In en, this message translates to:
  /// **'Spam'**
  String get reportReasonSpam;

  /// One of the five reasons of the report form: the member pretends to be another person.
  ///
  /// In en, this message translates to:
  /// **'Pretending to be someone else'**
  String get reportReasonImpersonation;

  /// One of the five reasons of the report form: none of the other four.
  ///
  /// In en, this message translates to:
  /// **'Something else'**
  String get reportReasonOther;

  /// Label of the free-text field of the report form, where the user may describe what happened. Says the field is optional. Must not state a length.
  ///
  /// In en, this message translates to:
  /// **'Details (optional)'**
  String get reportDetailsLabel;

  /// Notice on the report form: the report is private and the reported member is not told who reported them. Must not promise an answer or an outcome.
  ///
  /// In en, this message translates to:
  /// **'Your report is private. This member won’t be told who reported them.'**
  String get reportPrivacyNotice;

  /// Button that sends the report.
  ///
  /// In en, this message translates to:
  /// **'Send report'**
  String get reportSendButton;

  /// Confirmation shown in place of the report form once the report was sent. Says the member can also be blocked from their profile: reporting does not block. Must not promise an answer or an outcome, and must not show the member’s name.
  ///
  /// In en, this message translates to:
  /// **'Your report was sent. You can also block this member from their profile.'**
  String get reportSent;

  /// Button under the confirmation of a sent report that returns to the reported member’s profile.
  ///
  /// In en, this message translates to:
  /// **'Back to profile'**
  String get reportBackToProfile;

  /// Title of the dialog that asks the user to confirm blocking the member whose profile they are looking at.
  ///
  /// In en, this message translates to:
  /// **'Block this member?'**
  String get memberBlockTitle;

  /// Text of the dialog that asks the user to confirm blocking a member. Says that neither member will see the other’s profile and that the other member is not told.
  ///
  /// In en, this message translates to:
  /// **'You won’t see each other’s profiles. They won’t be told that you blocked them.'**
  String get memberBlockMessage;

  /// Dialog button that blocks the member.
  ///
  /// In en, this message translates to:
  /// **'Block'**
  String get memberBlockConfirm;

  /// Dialog button that closes the dialog without blocking the member.
  ///
  /// In en, this message translates to:
  /// **'Cancel'**
  String get memberBlockCancel;

  /// Accessibility label of the progress indicator shown on a member’s public profile while the block is being sent.
  ///
  /// In en, this message translates to:
  /// **'Blocking the member'**
  String get memberBlockProgress;

  /// Text shown in place of a member’s public profile once the user has blocked them, above the “Unblock” button. Says the member is blocked and that “Unblock” undoes it. Must not show the member’s name, and must not say the member can be unblocked from the “Blocked members” screen.
  ///
  /// In en, this message translates to:
  /// **'You’ve blocked this member. “Unblock” undoes it.'**
  String get memberBlocked;

  /// Title of the dialog that asks the user to confirm unblocking a member, on the “Blocked members” screen and on the public profile of a member they have just blocked. Must not show the member’s name: the text under it says whom, where the app knows it.
  ///
  /// In en, this message translates to:
  /// **'Unblock this member?'**
  String get unblockTitle;

  /// Text of the dialog that asks the user to confirm unblocking the member they have just blocked. Must not promise that the member’s profile will be shown again.
  ///
  /// In en, this message translates to:
  /// **'Your block will be removed. They won’t be told.'**
  String get memberUnblockMessage;

  /// Button that removes the user’s block of a member, after a confirmation: beside each name on the “Blocked members” screen, under the text shown once a member is blocked, and as the confirming answer of the dialog.
  ///
  /// In en, this message translates to:
  /// **'Unblock'**
  String get unblockButton;

  /// Dialog button that closes the dialog without unblocking the member.
  ///
  /// In en, this message translates to:
  /// **'Cancel'**
  String get unblockCancel;

  /// Accessibility label of the progress indicator shown while an unblock is being sent.
  ///
  /// In en, this message translates to:
  /// **'Unblocking the member'**
  String get unblockProgress;

  /// Button on the home screen that opens the list of the members the user has blocked.
  ///
  /// In en, this message translates to:
  /// **'Blocked members'**
  String get blockedMembersButton;

  /// Text of the dialog that asks the user to confirm unblocking a member chosen on the “Blocked members” screen. Names the member.
  ///
  /// In en, this message translates to:
  /// **'You and {name} will be able to see each other’s profiles again. They won’t be told.'**
  String blockedMembersUnblockMessage(String name);

  /// Title of the screen that lists the members the user has blocked.
  ///
  /// In en, this message translates to:
  /// **'Blocked members'**
  String get blockedMembersTitle;

  /// Accessibility label of the progress indicator while the list of blocked members loads.
  ///
  /// In en, this message translates to:
  /// **'Loading blocked members'**
  String get blockedMembersLoading;

  /// Text shown on the “Blocked members” screen when the user has blocked nobody.
  ///
  /// In en, this message translates to:
  /// **'You haven’t blocked anyone.'**
  String get blockedMembersEmpty;

  /// Accessibility label of the “Unblock” button beside a member’s name on the “Blocked members” screen: says whom the button unblocks.
  ///
  /// In en, this message translates to:
  /// **'Unblock {name}'**
  String blockedMembersUnblockLabel(String name);

  /// Notice on the profile form saying which information other members will see.
  ///
  /// In en, this message translates to:
  /// **'Other members will be able to see your name, your picture and what you write about yourself. Your email address stays private.'**
  String get profileVisibilityNotice;

  /// Label of the field for the name shown to other members.
  ///
  /// In en, this message translates to:
  /// **'Name'**
  String get displayNameLabel;

  /// Label of the optional multi-line field where the user introduces themselves.
  ///
  /// In en, this message translates to:
  /// **'About you (optional)'**
  String get bioLabel;

  /// Button that saves the profile form.
  ///
  /// In en, this message translates to:
  /// **'Save'**
  String get profileSaveButton;

  /// Button that leaves the profile form without saving.
  ///
  /// In en, this message translates to:
  /// **'Cancel'**
  String get profileCancelButton;

  /// Row on the profile form that opens the screen where the user edits the languages they speak and are learning.
  ///
  /// In en, this message translates to:
  /// **'Languages'**
  String get profileEditLanguagesButton;

  /// Text of the dialog shown when the user leaves the profile form with unsaved changes to their name or their text.
  ///
  /// In en, this message translates to:
  /// **'Your changes to your profile haven’t been saved.'**
  String get profileDiscardMessage;

  /// Error when the profile form is submitted without a name.
  ///
  /// In en, this message translates to:
  /// **'Enter your name'**
  String get displayNameRequired;

  /// Error when the server finds the name too long. Must not state a number.
  ///
  /// In en, this message translates to:
  /// **'This name is too long. Use a shorter one.'**
  String get errorDisplayNameTooLong;

  /// Error when the server refuses the name because of its characters.
  ///
  /// In en, this message translates to:
  /// **'This name can’t be used. It needs at least one letter or number and can’t contain hidden or special control characters.'**
  String get errorDisplayNameInvalid;

  /// Error when the server finds the “about you” text too long. Must not state a number.
  ///
  /// In en, this message translates to:
  /// **'This text is too long. Make it shorter.'**
  String get errorBioTooLong;

  /// Error when the server refuses the “about you” text because of its characters.
  ///
  /// In en, this message translates to:
  /// **'This text contains characters that can’t be used. Remove them and try again.'**
  String get errorBioInvalid;

  /// Error under a list of the member’s languages (spoken or learning) when the server finds too many in it. Must not state a number.
  ///
  /// In en, this message translates to:
  /// **'This list has too many languages. Remove some and try again.'**
  String get errorLanguagesTooMany;

  /// Error under a list of the member’s languages when the server doesn’t know one of them. The server doesn’t say which.
  ///
  /// In en, this message translates to:
  /// **'One of these languages isn’t available. Remove it and try again.'**
  String get errorLanguageUnknown;

  /// Error under a list of the member’s languages when the server refuses a level, for example “native” for a language being learned. The server doesn’t say which.
  ///
  /// In en, this message translates to:
  /// **'One of these levels can’t be used here. Choose a different level and try again.'**
  String get errorLanguageLevelInvalid;

  /// Error under a list of the member’s languages when a language appears twice, in that list or in both lists.
  ///
  /// In en, this message translates to:
  /// **'A language can only be added once, in one of the two lists. Remove the repeated one and try again.'**
  String get errorLanguageDuplicate;

  /// Error by the profile picture control when the server received a photo with no data.
  ///
  /// In en, this message translates to:
  /// **'That photo is empty. Choose a different one.'**
  String get errorAvatarRequired;

  /// Error by the profile picture control when the server finds the photo’s file too large. Must not state a number.
  ///
  /// In en, this message translates to:
  /// **'That photo’s file is too large. Choose a smaller one.'**
  String get errorAvatarTooLarge;

  /// Error by the profile picture control when the chosen file is not a JPEG or PNG image.
  ///
  /// In en, this message translates to:
  /// **'That kind of file can’t be used. Choose a JPEG or PNG photo.'**
  String get errorAvatarUnsupportedType;

  /// Error by the profile picture control when the server could not decode the photo.
  ///
  /// In en, this message translates to:
  /// **'That photo couldn’t be read and may be damaged. Choose a different one.'**
  String get errorAvatarInvalidImage;

  /// Error by the profile picture control when the photo is wider or taller than the server accepts. Must not state a number.
  ///
  /// In en, this message translates to:
  /// **'That photo has too many pixels. Choose a smaller one.'**
  String get errorAvatarDimensionsTooLarge;

  /// Error by the profile picture control when the device could not give the app the photo the user chose. Nothing was sent.
  ///
  /// In en, this message translates to:
  /// **'That photo couldn’t be used. Choose a different one.'**
  String get errorPhotoUnusable;

  /// Error by the reasons of the report form when the user sends it with no reason chosen, or the server received a report with no reason.
  ///
  /// In en, this message translates to:
  /// **'Choose a reason for the report.'**
  String get errorReportReasonRequired;

  /// Error by the reasons of the report form when the server does not accept the reason sent.
  ///
  /// In en, this message translates to:
  /// **'That reason can’t be used. Choose a different one.'**
  String get errorReportReasonInvalid;

  /// Error under the details field of the report form when the server finds the text too long. Must not state a number.
  ///
  /// In en, this message translates to:
  /// **'This text is too long. Make it shorter.'**
  String get errorReportDetailsTooLong;

  /// Error under the details field of the report form when the server refuses the text because of its characters.
  ///
  /// In en, this message translates to:
  /// **'This text contains characters that can’t be used. Remove them and try again.'**
  String get errorReportDetailsInvalid;

  /// Error when the member tries to block one more member than the server allows. Says that unblocking someone makes room. Must not state a number.
  ///
  /// In en, this message translates to:
  /// **'You’ve reached the limit of blocked members. Unblock someone to make room.'**
  String get errorBlocksTooMany;

  /// Heading of the section of the profile screen that shows the languages the user speaks and is learning.
  ///
  /// In en, this message translates to:
  /// **'Languages'**
  String get languagesHeading;

  /// Accessibility label of the progress indicator while the user’s languages load.
  ///
  /// In en, this message translates to:
  /// **'Loading your languages'**
  String get languagesLoading;

  /// Text of the languages section of the profile when the user has chosen no language, spoken or learning.
  ///
  /// In en, this message translates to:
  /// **'You haven’t added any languages yet.'**
  String get languagesEmpty;

  /// Heading of the list of languages the user knows and can offer to other members.
  ///
  /// In en, this message translates to:
  /// **'I speak'**
  String get languagesSpokenHeading;

  /// Heading of the list of languages the user wants to practise.
  ///
  /// In en, this message translates to:
  /// **'I’m learning'**
  String get languagesLearningHeading;

  /// Short name of the CEFR level A1 (beginner), shown next to a language.
  ///
  /// In en, this message translates to:
  /// **'A1'**
  String get languageLevelA1;

  /// Short name of the CEFR level A2 (elementary), shown next to a language.
  ///
  /// In en, this message translates to:
  /// **'A2'**
  String get languageLevelA2;

  /// Short name of the CEFR level B1 (intermediate), shown next to a language.
  ///
  /// In en, this message translates to:
  /// **'B1'**
  String get languageLevelB1;

  /// Short name of the CEFR level B2 (upper intermediate), shown next to a language.
  ///
  /// In en, this message translates to:
  /// **'B2'**
  String get languageLevelB2;

  /// Short name of the CEFR level C1 (advanced), shown next to a language.
  ///
  /// In en, this message translates to:
  /// **'C1'**
  String get languageLevelC1;

  /// Short name of the CEFR level C2 (proficient), shown next to a language.
  ///
  /// In en, this message translates to:
  /// **'C2'**
  String get languageLevelC2;

  /// Short name of the level of a native language, shown next to a language the user speaks.
  ///
  /// In en, this message translates to:
  /// **'Native'**
  String get languageLevelNative;

  /// Title of the screen where the user edits the languages they speak and are learning.
  ///
  /// In en, this message translates to:
  /// **'Your languages'**
  String get languagesEditorTitle;

  /// Notice at the top of the languages editor saying that the user’s languages and levels will be visible to other members.
  ///
  /// In en, this message translates to:
  /// **'Other members will be able to see the languages you speak and are learning, and your level in each.'**
  String get languagesVisibilityNotice;

  /// Button under a list of the user’s languages (spoken or learning) that opens the language picker.
  ///
  /// In en, this message translates to:
  /// **'Add a language'**
  String get languagesAddButton;

  /// Button that saves the user’s languages.
  ///
  /// In en, this message translates to:
  /// **'Save'**
  String get languagesSaveButton;

  /// Button that leaves the languages editor without saving.
  ///
  /// In en, this message translates to:
  /// **'Cancel'**
  String get languagesCancelButton;

  /// Title of the dialog shown when the user leaves the languages editor or the profile form with unsaved changes.
  ///
  /// In en, this message translates to:
  /// **'Discard changes?'**
  String get languagesDiscardTitle;

  /// Text of the dialog shown when the user leaves the languages editor with unsaved changes.
  ///
  /// In en, this message translates to:
  /// **'Your changes to your languages haven’t been saved.'**
  String get languagesDiscardMessage;

  /// Dialog button that leaves the languages editor or the profile form and drops the unsaved changes.
  ///
  /// In en, this message translates to:
  /// **'Discard'**
  String get languagesDiscardConfirm;

  /// Dialog button that closes the dialog and stays in the languages editor or the profile form.
  ///
  /// In en, this message translates to:
  /// **'Keep editing'**
  String get languagesDiscardKeep;

  /// Title of the sheet where the user picks a language to add.
  ///
  /// In en, this message translates to:
  /// **'Choose a language'**
  String get languagePickerTitle;

  /// Label of the search field of the language picker.
  ///
  /// In en, this message translates to:
  /// **'Search'**
  String get languagePickerSearchLabel;

  /// Text of the language picker when the search finds no language.
  ///
  /// In en, this message translates to:
  /// **'No language matches your search.'**
  String get languagePickerNoMatch;

  /// Title of the sheet where the user chooses their level in a language.
  ///
  /// In en, this message translates to:
  /// **'Your level in {name}'**
  String languageLevelPickerTitle(String name);

  /// Accessibility label of the button that shows a language’s level and changes it.
  ///
  /// In en, this message translates to:
  /// **'{name}, level {level}'**
  String languageLevelSemantics(String name, String level);

  /// Tooltip and accessibility label of the button that removes a language from a list.
  ///
  /// In en, this message translates to:
  /// **'Remove {name}'**
  String languageRemove(String name);

  /// Tooltip and accessibility label of the button that moves a language one place up its list.
  ///
  /// In en, this message translates to:
  /// **'Move {name} up'**
  String languageMoveUp(String name);

  /// Tooltip and accessibility label of the button that moves a language one place down its list.
  ///
  /// In en, this message translates to:
  /// **'Move {name} down'**
  String languageMoveDown(String name);

  /// One-line description of the level A1, shown next to its short name where the user chooses a level.
  ///
  /// In en, this message translates to:
  /// **'Beginner'**
  String get languageLevelDescriptionA1;

  /// One-line description of the level A2, shown next to its short name where the user chooses a level.
  ///
  /// In en, this message translates to:
  /// **'Elementary'**
  String get languageLevelDescriptionA2;

  /// One-line description of the level B1, shown next to its short name where the user chooses a level.
  ///
  /// In en, this message translates to:
  /// **'Intermediate'**
  String get languageLevelDescriptionB1;

  /// One-line description of the level B2, shown next to its short name where the user chooses a level.
  ///
  /// In en, this message translates to:
  /// **'Upper intermediate'**
  String get languageLevelDescriptionB2;

  /// One-line description of the level C1, shown next to its short name where the user chooses a level.
  ///
  /// In en, this message translates to:
  /// **'Advanced'**
  String get languageLevelDescriptionC1;

  /// One-line description of the level C2, shown next to its short name where the user chooses a level.
  ///
  /// In en, this message translates to:
  /// **'Proficient'**
  String get languageLevelDescriptionC2;

  /// One-line description of the level Native, shown next to its short name where the user chooses a level.
  ///
  /// In en, this message translates to:
  /// **'Native speaker'**
  String get languageLevelDescriptionNative;
}

class _AppLocalizationsDelegate
    extends LocalizationsDelegate<AppLocalizations> {
  const _AppLocalizationsDelegate();

  @override
  Future<AppLocalizations> load(Locale locale) {
    return SynchronousFuture<AppLocalizations>(lookupAppLocalizations(locale));
  }

  @override
  bool isSupported(Locale locale) =>
      <String>['en'].contains(locale.languageCode);

  @override
  bool shouldReload(_AppLocalizationsDelegate old) => false;
}

AppLocalizations lookupAppLocalizations(Locale locale) {
  // Lookup logic when only language code is specified.
  switch (locale.languageCode) {
    case 'en':
      return AppLocalizationsEn();
  }

  throw FlutterError(
    'AppLocalizations.delegate failed to load unsupported locale "$locale". This is likely '
    'an issue with the localizations generation tool. Please file an issue '
    'on GitHub with a reproducible sample app and the gen-l10n configuration '
    'that was used.',
  );
}

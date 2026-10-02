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

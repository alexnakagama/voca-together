import 'api_exception.dart';
import 'languages.dart';

/// A member's public profile (`GET /v1/profiles/{id}`, decision 031), as
/// returned by `SessionManager.memberProfile()`: everything one signed-in
/// member can read about another, and nothing else. Holds no token.
/// [toString] is redacted: a profile is personal data.
final class MemberProfile {
  const MemberProfile({
    required this.id,
    required this.displayName,
    required this.bio,
    required this.hasAvatar,
    required this.languages,
  });

  /// Validates a decoded 200 body; anything off-contract, in the languages
  /// included, is an [ApiProtocolException] with
  /// [ProtocolFailure.malformedBody].
  factory MemberProfile.fromJson(Object? json) {
    if (json
        case {
          'id': final String id,
          'display_name': final String displayName,
          'bio': final String bio,
          'has_avatar': final bool hasAvatar,
          'languages': final Object? languages,
        }
        when id.isNotEmpty && displayName.isNotEmpty) {
      return MemberProfile(
        id: id,
        displayName: displayName,
        bio: bio,
        hasAvatar: hasAvatar,
        languages: UserLanguages.fromJson(languages),
      );
    }
    throw const ApiProtocolException(
      ProtocolFailure.malformedBody,
      statusCode: 200,
    );
  }

  /// The profile's public identifier. Never empty.
  final String id;

  /// The name shown for this member. Never empty.
  final String displayName;

  /// What the member wrote about themselves; empty when they wrote nothing.
  final String bio;

  /// Whether the member has a profile picture to ask for.
  final bool hasAvatar;

  /// The languages the member speaks and is learning, in their order.
  final UserLanguages languages;

  @override
  String toString() => 'MemberProfile(<redacted>)';
}

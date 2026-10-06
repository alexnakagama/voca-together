import 'api_exception.dart';

/// The signed-in user's own profile (`GET`/`PUT /v1/me/profile`, decision
/// 027), as returned by `SessionManager.profile()` and `saveProfile()`.
/// Holds no token. [toString] is redacted: what a member writes about
/// themselves is personal data.
final class Profile {
  const Profile({
    required this.id,
    required this.displayName,
    required this.bio,
    required this.createdAt,
    required this.updatedAt,
  });

  /// Validates a decoded 200 body; anything off-contract is an
  /// [ApiProtocolException] with [ProtocolFailure.malformedBody].
  factory Profile.fromJson(Object? json) {
    if (json
        case {
          'id': final String id,
          'display_name': final String displayName,
          'bio': final String bio,
          'created_at': final String createdAt,
          'updated_at': final String updatedAt,
        }
        when id.isNotEmpty && displayName.isNotEmpty) {
      final created = DateTime.tryParse(createdAt);
      final updated = DateTime.tryParse(updatedAt);
      if (created != null && updated != null) {
        return Profile(
          id: id,
          displayName: displayName,
          bio: bio,
          createdAt: created.toUtc(),
          updatedAt: updated.toUtc(),
        );
      }
    }
    throw const ApiProtocolException(
      ProtocolFailure.malformedBody,
      statusCode: 200,
    );
  }

  /// The profile's public identifier: what names this member to other
  /// members (decision 031). Never empty, and not the account's id.
  final String id;

  /// The name shown for this member. Never empty.
  final String displayName;

  /// What the member wrote about themselves; empty when they wrote nothing.
  final String bio;

  final DateTime createdAt;
  final DateTime updatedAt;

  @override
  String toString() => 'Profile(<redacted>)';
}

import 'api_exception.dart';

/// The signed-in user (`GET /v1/me`, decision 016), as returned by
/// `SessionManager.me()`. Holds no token. [toString] is redacted: [email] is
/// personal data.
final class Me {
  const Me({
    required this.id,
    required this.email,
    required this.emailVerifiedAt,
    required this.createdAt,
  });

  /// Validates a decoded 200 body; anything off-contract is an
  /// [ApiProtocolException] with [ProtocolFailure.malformedBody].
  factory Me.fromJson(Object? json) {
    if (json
        case {
          'id': final String id,
          'email': final String email,
          'email_verified_at': final String verifiedAt,
          'created_at': final String createdAt,
        }
        when id.isNotEmpty && email.isNotEmpty) {
      final verified = DateTime.tryParse(verifiedAt);
      final created = DateTime.tryParse(createdAt);
      if (verified != null && created != null) {
        return Me(
          id: id,
          email: email,
          emailVerifiedAt: verified.toUtc(),
          createdAt: created.toUtc(),
        );
      }
    }
    throw const ApiProtocolException(
      ProtocolFailure.malformedBody,
      statusCode: 200,
    );
  }

  final String id;
  final String email;
  final DateTime emailVerifiedAt;
  final DateTime createdAt;

  @override
  String toString() => 'Me(<redacted>)';
}

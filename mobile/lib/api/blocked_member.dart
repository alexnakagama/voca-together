import 'api_exception.dart';

/// One member the signed-in user has blocked (`GET /v1/me/blocks`, decision
/// 033), as returned by `SessionManager.blockedMembers()`: what names them
/// and nothing else. Holds no token. [toString] is redacted: whom a member
/// blocked is personal data.
final class BlockedMember {
  const BlockedMember({required this.id, required this.displayName});

  /// Validates one entry of the list; anything off-contract is an
  /// [ApiProtocolException] with [ProtocolFailure.malformedBody].
  factory BlockedMember.fromJson(Object? json) {
    if (json
        case {'id': final String id, 'display_name': final String displayName}
        when id.isNotEmpty && displayName.isNotEmpty) {
      return BlockedMember(id: id, displayName: displayName);
    }
    throw _malformed;
  }

  /// Validates a decoded 200 body of the list and returns its members in
  /// the server's order (most recently blocked first), as a list that can't
  /// be changed. The list must be an array: a missing or `null` one is never
  /// read as "nobody blocked". One malformed entry fails the whole body.
  static List<BlockedMember> listFromJson(Object? json) {
    if (json case {'blocks': final List<Object?> blocks}) {
      return List.unmodifiable([
        for (final entry in blocks) BlockedMember.fromJson(entry),
      ]);
    }
    throw _malformed;
  }

  /// The member's public identifier: what an unblock names. Never empty.
  final String id;

  /// The name shown for this member, as it is now. Never empty.
  final String displayName;

  @override
  String toString() => 'BlockedMember(<redacted>)';
}

const _malformed = ApiProtocolException(
  ProtocolFailure.malformedBody,
  statusCode: 200,
);

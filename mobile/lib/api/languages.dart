import 'api_exception.dart';

/// How well a member knows a language: the six CEFR levels, then [native]
/// (decision 029). The values are declared in the scale's order, lowest
/// first.
enum LanguageLevel {
  a1('a1'),
  a2('a2'),
  b1('b1'),
  b2('b2'),
  c1('c1'),
  c2('c2'),

  /// A native language. The backend accepts it only for a spoken language.
  native('native');

  const LanguageLevel(this.wire);

  /// The level's identifier in the API.
  final String wire;

  /// The level whose identifier is exactly [wire], or null. Identifiers are
  /// not text: nothing is trimmed or case-folded.
  static LanguageLevel? tryParse(String wire) {
    for (final level in values) {
      if (level.wire == wire) return level;
    }
    return null;
  }
}

/// One language of the catalog (`GET /v1/languages`, decision 029), as
/// returned by `SessionManager.languageCatalog()`. The same for every
/// member; holds no token.
final class Language {
  const Language({
    required this.code,
    required this.name,
    required this.endonym,
  });

  /// Validates one catalog entry; anything off-contract is an
  /// [ApiProtocolException] with [ProtocolFailure.malformedBody].
  factory Language.fromJson(Object? json) {
    if (json
        case {
          'code': final String code,
          'name': final String name,
          'endonym': final String endonym,
        }
        when code.isNotEmpty && name.isNotEmpty && endonym.isNotEmpty) {
      return Language(code: code, name: name, endonym: endonym);
    }
    throw _malformed;
  }

  /// Validates a decoded 200 body of the catalog and returns its languages
  /// in the server's order (by [name]), as a list that can't be changed. One
  /// malformed entry fails the whole body.
  static List<Language> catalogFromJson(Object? json) {
    if (json case {'languages': final List<Object?> languages}) {
      return List.unmodifiable([
        for (final entry in languages) Language.fromJson(entry),
      ]);
    }
    throw _malformed;
  }

  /// The language's identifier: a lowercase BCP 47 primary subtag, such as
  /// `es` or `yue`. What a [UserLanguage] refers to.
  final String code;

  /// The English name, such as "Spanish". Data from the catalog, not an app
  /// string.
  final String name;

  /// The language's own name, such as "Español".
  final String endonym;
}

/// One language of a member, with their level in it. [toString] is redacted:
/// a member's languages are personal data.
final class UserLanguage {
  const UserLanguage(this.code, this.level);

  /// Validates one entry of a selection; anything off-contract, a level this
  /// app doesn't know included, is an [ApiProtocolException] with
  /// [ProtocolFailure.malformedBody].
  factory UserLanguage.fromJson(Object? json) {
    if (json case {'language': final String code, 'level': final String wire}
        when code.isNotEmpty) {
      final level = LanguageLevel.tryParse(wire);
      if (level != null) return UserLanguage(code, level);
    }
    throw _malformed;
  }

  /// The [Language.code] of the language.
  final String code;

  final LanguageLevel level;

  Map<String, Object?> toJson() => {'language': code, 'level': level.wire};

  @override
  bool operator ==(Object other) =>
      other is UserLanguage && other.code == code && other.level == level;

  @override
  int get hashCode => Object.hash(code, level);

  @override
  String toString() => 'UserLanguage(<redacted>)';
}

/// The signed-in user's own languages (`GET`/`PUT /v1/me/languages`,
/// decision 029): the ones they speak and the ones they are learning, each
/// list in the member's order. Returned by `SessionManager.languages()` and
/// taken and returned by `saveLanguages()`. Holds no token. [toString] is
/// redacted: a member's languages are personal data.
///
/// Always the member's complete selection, never one list or a change to
/// one: a save replaces everything (decision 030). Either list, or both, may
/// be empty; "none chosen" is two empty lists.
final class UserLanguages {
  /// Copies both lists, so the selection can't change after it is built.
  UserLanguages({
    required List<UserLanguage> spoken,
    required List<UserLanguage> learning,
  }) : spoken = List.unmodifiable(spoken),
       learning = List.unmodifiable(learning);

  /// Validates a decoded 200 body; anything off-contract is an
  /// [ApiProtocolException] with [ProtocolFailure.malformedBody]. Both lists
  /// must be arrays: a missing or `null` one is never read as empty.
  factory UserLanguages.fromJson(Object? json) {
    if (json case {
      'spoken': final List<Object?> spoken,
      'learning': final List<Object?> learning,
    }) {
      return UserLanguages(
        spoken: [for (final entry in spoken) UserLanguage.fromJson(entry)],
        learning: [for (final entry in learning) UserLanguage.fromJson(entry)],
      );
    }
    throw _malformed;
  }

  /// The languages the member knows and can offer, the primary one first.
  final List<UserLanguage> spoken;

  /// The languages the member wants to practise, the primary one first.
  final List<UserLanguage> learning;

  /// The body of `PUT /v1/me/languages`. It always holds both `spoken` and
  /// `learning`, each as an array (`[]` when empty): the backend refuses a
  /// missing or `null` list with a 400 the member could do nothing about, so
  /// this must stay unable to produce one (decisions 029, 030).
  Map<String, Object?> toJson() => {
    'spoken': [for (final language in spoken) language.toJson()],
    'learning': [for (final language in learning) language.toJson()],
  };

  /// Two selections are equal when both lists hold the same languages at
  /// the same levels in the same order: the order is the member's, and part
  /// of what a save stores.
  @override
  bool operator ==(Object other) =>
      other is UserLanguages &&
      _sameList(other.spoken, spoken) &&
      _sameList(other.learning, learning);

  @override
  int get hashCode =>
      Object.hash(Object.hashAll(spoken), Object.hashAll(learning));

  @override
  String toString() => 'UserLanguages(<redacted>)';
}

bool _sameList(List<UserLanguage> a, List<UserLanguage> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

const _malformed = ApiProtocolException(
  ProtocolFailure.malformedBody,
  statusCode: 200,
);

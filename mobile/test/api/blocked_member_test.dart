import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/api/api_exception.dart';
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/api/blocked_member.dart';
import 'package:vocatogether/api/report_reason.dart';

import '../support/fakes.dart';

final _malformed = isA<ApiProtocolException>()
    .having((e) => e.failure, 'failure', ProtocolFailure.malformedBody)
    .having((e) => e.statusCode, 'statusCode', 200);

void main() {
  group('paths', () {
    test('are the backend\'s', () {
      expect(ApiPaths.myBlocks, '/v1/me/blocks');
      expect(ApiPaths.myBlock(testMemberId), '/v1/me/blocks/$testMemberId');
      expect(ApiPaths.myReport(testMemberId), '/v1/me/reports/$testMemberId');
    });

    test('a block or report path takes only a canonical identifier', () {
      for (final id in [
        '',
        'abc',
        'self',
        testMemberId.toUpperCase(),
        '{$testMemberId}',
        testMemberId.replaceAll('-', ''),
        testMemberId.substring(1),
        '${testMemberId}0',
        ' $testMemberId',
        '$testMemberId ',
        '$testMemberId\n',
        '$testMemberId/x',
        '$testMemberId?blocker=$otherMemberId',
        '../profile',
        testMemberId.replaceFirst('0', 'g'),
        testMemberId.replaceFirst('-', '_'),
      ]) {
        for (final path in [ApiPaths.myBlock, ApiPaths.myReport]) {
          expect(
            () => path(id),
            throwsA(
              isA<ArgumentError>().having(
                (e) => '$e',
                'toString',
                // The value is never echoed.
                id.isEmpty ? anything : isNot(contains(id)),
              ),
            ),
            reason: id,
          );
        }
      }
    });
  });

  group('BlockedMember.listFromJson', () {
    final body = blocksBody([
      (otherMemberId, 'Bea Ortiz'),
      (testMemberId, 'Chidi'),
    ]);

    test('parses the members in the server\'s order', () {
      final members = BlockedMember.listFromJson(body);
      expect(members.map((m) => m.id), [otherMemberId, testMemberId]);
      expect(members.map((m) => m.displayName), ['Bea Ortiz', 'Chidi']);
    });

    test('parses what jsonDecode gives', () {
      final members = BlockedMember.listFromJson(jsonDecode(jsonEncode(body)));
      expect(members, hasLength(2));
      expect(members.first.displayName, 'Bea Ortiz');
    });

    test('an empty list is nobody blocked', () {
      expect(BlockedMember.listFromJson(blocksBody()), isEmpty);
      expect(BlockedMember.listFromJson(jsonDecode('{"blocks":[]}')), isEmpty);
    });

    test('the list can\'t be changed', () {
      final members = BlockedMember.listFromJson(body);
      expect(members.clear, throwsUnsupportedError);
    });

    test('anything off-contract fails the whole response', () {
      const good = {'id': otherMemberId, 'display_name': 'Bea'};
      Map<String, Object?> withEntry(Object? entry) => {
        'blocks': [good, entry],
      };
      final cases = <String, Object?>{
        'no blocks': <String, Object?>{},
        'a null list': {'blocks': null},
        'the list as an object': {'blocks': <String, Object?>{}},
        'the list as text': {'blocks': ''},
        'another key': {'members': <Object?>[]},
        'a bare list': <Object?>[good],
        'null': null,
        'an entry with no id': withEntry({'display_name': 'Bea'}),
        'an entry with an empty id': withEntry({...good, 'id': ''}),
        'an entry with a null id': withEntry({...good, 'id': null}),
        'an entry with a numeric id': withEntry({...good, 'id': 7}),
        'an entry with no name': withEntry({'id': otherMemberId}),
        'an entry with an empty name': withEntry({...good, 'display_name': ''}),
        'an entry with a null name': withEntry({...good, 'display_name': null}),
        'a null entry': withEntry(null),
        'an entry that is text': withEntry(otherMemberId),
      };
      cases.forEach((name, json) {
        expect(
          () => BlockedMember.listFromJson(json),
          throwsA(_malformed),
          reason: name,
        );
      });
    });

    test('toString is redacted', () {
      final members = BlockedMember.listFromJson(body);
      expect('${members.first}', 'BlockedMember(<redacted>)');
      for (final text in ['${members.first}', '$members']) {
        for (final secret in [otherMemberId, testMemberId, 'Bea', 'Chidi']) {
          expect(text, isNot(contains(secret)));
        }
      }
    });
  });

  group('ReportReason', () {
    test('has the backend\'s five wire codes, in the order offered', () {
      expect(ReportReason.values.map((r) => r.wire), [
        'harassment',
        'inappropriate_content',
        'spam',
        'impersonation',
        'other',
      ]);
    });
  });
}

import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/api/report_reason.dart';
import 'package:vocatogether/router.dart';
import 'package:vocatogether/screens/home_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/screens/member_profile_screen.dart';
import 'package:vocatogether/screens/report_member_screen.dart';
import 'package:vocatogether/session.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/form_notice_banner.dart';

import '../support/fakes.dart';
import 'harness.dart';

/// The public identifier of the member who is reported.
const _id = otherMemberId;

final _profilePath = ApiPaths.memberProfile(_id);
final _reportPath = ApiPaths.myReport(_id);
final _blockPath = ApiPaths.myBlock(_id);
final _location = '/members/$_id/report';

/// The five reasons as the form offers them, with what each one sends.
final _reasons = <(String, String)>[
  (l10n.reportReasonHarassment, 'harassment'),
  (l10n.reportReasonInappropriateContent, 'inappropriate_content'),
  (l10n.reportReasonSpam, 'spam'),
  (l10n.reportReasonImpersonation, 'impersonation'),
  (l10n.reportReasonOther, 'other'),
];

Finder get _screen => find.byType(ReportMemberScreen);
Finder get _member => find.byType(MemberProfileScreen);
Finder get _send => find.widgetWithText(FilledButton, l10n.reportSendButton);
Finder get _back => find.widgetWithText(FilledButton, l10n.reportBackToProfile);
Finder get _detailsField => field(l10n.reportDetailsLabel);
Finder get _radios => find.byType(RadioListTile<ReportReason>);
Finder _radio(String label) =>
    find.widgetWithText(RadioListTile<ReportReason>, label);

/// The reason the form holds, or null with none chosen.
ReportReason? _chosen(WidgetTester tester) => tester
    .widget<RadioGroup<ReportReason>>(find.byType(RadioGroup<ReportReason>))
    .groupValue;

/// The text in the details field.
String _typed(WidgetTester tester) =>
    textFieldOf(tester, _detailsField).controller!.text;

/// Whether any control of the form takes input.
List<bool> _enabledControls(WidgetTester tester) => [
  for (final radio in tester.widgetList<RadioListTile<ReportReason>>(_radios))
    radio.enabled ?? true,
  textFieldOf(tester, _detailsField).enabled ?? true,
  tester.widget<FilledButton>(_send).enabled,
];

/// A backend for a signed-in user on home, who has a profile of their own,
/// and the profile of another member; the report is scripted by each test.
FakeServer _backend() => FakeServer()
  ..always('GET', ApiPaths.profile, (_) => jsonResponse(200, profileBody()))
  ..always('GET', ApiPaths.languages, (_) => jsonResponse(200, catalogBody()))
  ..always('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()))
  ..always('GET', ApiPaths.healthz, (_) => healthy())
  ..always('POST', ApiPaths.logout, (_) => noContent())
  ..always(
    'GET',
    _profilePath,
    (_) => jsonResponse(
      200,
      memberProfileBody(id: _id, displayName: 'Bea', bio: 'Mornings'),
    ),
  );

/// Signs in on home, opens the other member's profile and activates
/// "Report" in its menu.
Future<TestApp> _openReport(WidgetTester tester, FakeServer server) async {
  final app = await pumpApp(tester, server: server, signedIn: true);
  unawaited(
    GoRouter.of(tester.element(find.byType(Navigator).first))
        .push<void>(Routes.member(_id)),
  );
  await tester.pumpAndSettle();
  await tester.tap(find.byTooltip(l10n.memberMenuTooltip));
  await tester.pumpAndSettle();
  await tester.tap(find.text(l10n.memberMenuReport));
  await tester.pumpAndSettle();
  expect(_screen, findsOneWidget);
  return app;
}

/// Activates "Send report" and renders one frame, without waiting for the
/// answer.
Future<void> _tapSend(WidgetTester tester) async {
  await tester.ensureVisible(_send);
  await tester.pumpAndSettle();
  await tester.tap(_send);
  await tester.pump();
}

/// The decoded body of a report request.
Object? _body(http.Request request) => jsonDecode(request.body);

void main() {
  group('the report screen', () {
    testWidgets('"Report" opens the form: five reasons with none chosen, an '
        'empty details field and the notice, and no request', (tester) async {
      final server = _backend();
      final app = await pumpApp(tester, server: server, signedIn: true);
      unawaited(
        GoRouter.of(tester.element(find.byType(Navigator).first))
            .push<void>(Routes.member(_id)),
      );
      await tester.pumpAndSettle();
      final before = server.requests.length;

      await tester.tap(find.byTooltip(l10n.memberMenuTooltip));
      await tester.pumpAndSettle();
      await tester.tap(find.text(l10n.memberMenuReport));
      await tester.pumpAndSettle();

      expect(_screen, findsOneWidget);
      expect(app.location(tester), _location);
      expect(find.text(l10n.reportTitle), findsOneWidget);
      expect(find.text(l10n.reportPrivacyNotice), findsOneWidget);
      expect(
        tester
            .widgetList<RadioListTile<ReportReason>>(_radios)
            .map((r) => r.value),
        ReportReason.values,
      );
      for (final (label, _) in _reasons) {
        expect(_radio(label), findsOneWidget, reason: label);
      }
      expect(_chosen(tester), isNull);
      expect(_typed(tester), isEmpty);
      expect(_send, findsOneWidget);
      expect(_enabledControls(tester), everyElement(isTrue));
      // Nothing of the member is shown, and nothing was asked for.
      expect(find.text('Bea'), findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(find.byType(FormNoticeBanner), findsNothing);
      expect(server.requests, hasLength(before));
      expect(server.count(_reportPath), 0);
    });

    testWidgets('the reasons are one choice: choosing one leaves the others '
        'unchosen, and each is announced with whether it is chosen', (
      tester,
    ) async {
      final server = _backend();
      final handle = tester.ensureSemantics();
      await _openReport(tester, server);

      for (final (label, _) in _reasons) {
        expect(
          tester.getSemantics(_radio(label)),
          isSemantics(
            label: label,
            hasCheckedState: true,
            isChecked: false,
            isInMutuallyExclusiveGroup: true,
            hasTapAction: true,
          ),
          reason: label,
        );
      }

      await tapAndSettle(tester, find.text(l10n.reportReasonSpam));
      expect(_chosen(tester), ReportReason.spam);
      await tapAndSettle(tester, find.text(l10n.reportReasonOther));
      expect(_chosen(tester), ReportReason.other);
      for (final (label, _) in _reasons) {
        expect(
          tester.getSemantics(_radio(label)),
          isSemantics(
            label: label,
            hasCheckedState: true,
            isChecked: label == l10n.reportReasonOther,
            isInMutuallyExclusiveGroup: true,
          ),
          reason: label,
        );
      }
      // Choosing sends nothing.
      expect(server.count(_reportPath), 0);
      handle.dispose();
    });

    testWidgets('leaving a filled form asks nothing and sends nothing', (
      tester,
    ) async {
      final server = _backend();
      final app = await _openReport(tester, server);

      await tapAndSettle(tester, find.text(l10n.reportReasonSpam));
      await tester.enterText(_detailsField, 'They keep posting links');
      await tester.pageBack();
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsNothing);
      expect(_screen, findsNothing);
      expect(_member, findsOneWidget);
      expect(find.text('Bea'), findsOneWidget);
      expect(app.location(tester), '/members/$_id');
      expect(server.count(_reportPath), 0);
      expect(server.count(_profilePath), 1);
    });
  });

  group('the route', () {
    testWidgets('Routes.memberReport is the route and the id, and nothing '
        'else', (tester) async {
      expect(Routes.memberReport(_id), _location);
      expect(Routes.isMemberReport(Routes.memberReport(_id)), isTrue);
      expect(Routes.isMember(Routes.memberReport(_id)), isFalse);
      expect(Uri.parse(Routes.memberReport(_id)).hasQuery, isFalse);
    });

    testWidgets('a signed-out user is shown log in, and nothing is asked', (
      tester,
    ) async {
      final server = _backend();
      final app = await pumpApp(tester, server: server);
      GoRouter.of(tester.element(find.byType(Navigator).first))
          .go(Routes.memberReport(_id));
      await tester.pumpAndSettle();

      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(app.location(tester), '/login');
      expect(server.requests, isEmpty);
    });

    testWidgets('a malformed identifier is not a route: home, and no request '
        'about a member', (tester) async {
      final server = _backend();
      final app = await pumpApp(tester, server: server, signedIn: true);

      // A trailing slash is not here: go_router removes it before the
      // redirect runs, as for every route, so the path it asks about is the
      // canonical one. `authRedirect` itself refuses it (router_test).
      for (final location in [
        '/members/abc/report',
        '/members/${_id.toUpperCase()}/report',
        '/members/${_id.replaceAll('-', '')}/report',
        '/members/$_id/report/x',
        '/members//report',
        Routes.memberReport('abc'),
        Routes.memberReport(''),
      ]) {
        GoRouter.of(tester.element(find.byType(Navigator).first)).go(location);
        await tester.pumpAndSettle();
        expect(find.byType(HomeScreen), findsOneWidget, reason: location);
        expect(_screen, findsNothing, reason: location);
        expect(app.location(tester), '/home', reason: location);
      }
      expect(server.count(_reportPath), 0);
      expect(server.count(_profilePath), 0);
    });

    testWidgets('logging out elsewhere leaves the form for log in', (
      tester,
    ) async {
      final server = _backend();
      final app = await _openReport(tester, server);
      await tapAndSettle(tester, find.text(l10n.reportReasonSpam));
      await app.session.logout();
      await tester.pumpAndSettle();

      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(app.location(tester), '/login');
      expect(server.count(_reportPath), 0);
    });
  });

  group('sending a report', () {
    testWidgets('with no reason chosen: a text asks for one, announced, and '
        'nothing is sent; choosing one clears it', (tester) async {
      final server = _backend();
      final handle = tester.ensureSemantics();
      final app = await _openReport(tester, server);

      await tester.enterText(_detailsField, 'Something happened');
      await _tapSend(tester);
      await tester.pumpAndSettle();

      final missing = find.text(l10n.errorReportReasonRequired);
      expect(missing, findsOneWidget);
      expect(
        tester.getSemantics(missing),
        isSemantics(label: l10n.errorReportReasonRequired, isLiveRegion: true),
      );
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(_typed(tester), 'Something happened');
      expect(_enabledControls(tester), everyElement(isTrue));
      expect(server.count(_reportPath), 0);
      expect(app.location(tester), _location);

      await tapAndSettle(tester, find.text(l10n.reportReasonHarassment));
      expect(missing, findsNothing);
      expect(server.count(_reportPath), 0);
      handle.dispose();
    });

    for (final (label, wire) in _reasons) {
      testWidgets('"$label" sends $wire', (tester) async {
        final server = _backend()..once('PUT', _reportPath, (_) => noContent());
        await _openReport(tester, server);

        await tapAndSettle(tester, find.text(label));
        await _tapSend(tester);
        await tester.pumpAndSettle();

        // No details: the key is still sent, empty.
        expect(_body(server.to(_reportPath).single), {
          'reason': wire,
          'details': '',
        });
      });
    }

    testWidgets('one PUT with the reason and the details exactly as typed', (
      tester,
    ) async {
      const typed = '  They wrote:\n\t“buy now”  \n';
      final server = _backend()..once('PUT', _reportPath, (_) => noContent());
      await _openReport(tester, server);

      await tapAndSettle(tester, find.text(l10n.reportReasonSpam));
      await tester.enterText(_detailsField, typed);
      await _tapSend(tester);
      await tester.pumpAndSettle();

      final request = server.to(_reportPath).single;
      expect(request.method, 'PUT');
      expect(request.url.hasQuery, isFalse);
      expect(_body(request), {'reason': 'spam', 'details': typed});
    });

    testWidgets('sent: the confirmation replaces the form, and its control '
        'returns to the profile, still shown, not reloaded and not blocked', (
      tester,
    ) async {
      final server = _backend()..once('PUT', _reportPath, (_) => noContent());
      final handle = tester.ensureSemantics();
      final app = await _openReport(tester, server);
      final before = server.requests.length;

      await tapAndSettle(tester, find.text(l10n.reportReasonImpersonation));
      await tester.enterText(_detailsField, 'Uses my photo');
      await _tapSend(tester);
      await tester.pumpAndSettle();

      expect(find.text(l10n.reportSent), findsOneWidget);
      expect(
        tester.getSemantics(find.byType(FormNoticeBanner)),
        isSemantics(
          isLiveRegion: true,
          label: '${l10n.noticeLabel}\n${l10n.reportSent}',
        ),
      );
      expect(_radios, findsNothing);
      expect(_detailsField, findsNothing);
      expect(_send, findsNothing);
      expect(find.text('Uses my photo'), findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(app.location(tester), _location);

      await tapAndSettle(tester, _back);
      expect(_screen, findsNothing);
      expect(_member, findsOneWidget);
      expect(app.location(tester), '/members/$_id');
      // The profile as it was, with its menu: reporting blocked nobody.
      expect(find.text('Bea'), findsOneWidget);
      expect(find.text('Mornings'), findsOneWidget);
      expect(find.text(l10n.memberBlocked), findsNothing);
      expect(find.byTooltip(l10n.memberMenuTooltip), findsOneWidget);
      // The report is the only request since the form opened.
      expect(server.requests, hasLength(before + 1));
      expect(server.count(_reportPath), 1);
      expect(server.count(_profilePath), 1);
      expect(server.count(_blockPath), 0);
      expect(server.count(ApiPaths.myBlocks), 0);
      handle.dispose();
    });

    testWidgets('back from the confirmation returns to the profile too', (
      tester,
    ) async {
      final server = _backend()..once('PUT', _reportPath, (_) => noContent());
      final app = await _openReport(tester, server);

      await tapAndSettle(tester, find.text(l10n.reportReasonOther));
      await _tapSend(tester);
      await tester.pumpAndSettle();
      await tester.pageBack();
      await tester.pumpAndSettle();

      expect(_member, findsOneWidget);
      expect(find.text('Bea'), findsOneWidget);
      expect(app.location(tester), '/members/$_id');
      expect(server.count(_reportPath), 1);
      expect(server.count(_profilePath), 1);
    });

    testWidgets('a second report from the same profile starts from an empty '
        'form', (tester) async {
      final server = _backend()..once('PUT', _reportPath, (_) => noContent());
      await _openReport(tester, server);
      await tapAndSettle(tester, find.text(l10n.reportReasonSpam));
      await tester.enterText(_detailsField, 'First');
      await _tapSend(tester);
      await tester.pumpAndSettle();
      await tapAndSettle(tester, _back);

      await tester.tap(find.byTooltip(l10n.memberMenuTooltip));
      await tester.pumpAndSettle();
      await tester.tap(find.text(l10n.memberMenuReport));
      await tester.pumpAndSettle();

      expect(_chosen(tester), isNull);
      expect(_typed(tester), isEmpty);
      expect(find.text(l10n.reportSent), findsNothing);
      expect(server.count(_reportPath), 1);
    });
  });

  group('a failed report', () {
    testWidgets('details too long: the message is under the field, and the '
        'reason and the text are kept', (tester) async {
      final server = _backend()
        ..once('PUT', _reportPath, (_) => fieldError('details', 'too_long'))
        ..once('PUT', _reportPath, (_) => noContent());
      final app = await _openReport(tester, server);

      await tapAndSettle(tester, find.text(l10n.reportReasonHarassment));
      await tester.enterText(_detailsField, 'A very long text');
      await _tapSend(tester);
      await tester.pumpAndSettle();

      expect(
        errorOf(tester, l10n.reportDetailsLabel),
        l10n.errorReportDetailsTooLong,
      );
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(find.text(l10n.reportSent), findsNothing);
      expect(_chosen(tester), ReportReason.harassment);
      expect(_typed(tester), 'A very long text');
      expect(_enabledControls(tester), everyElement(isTrue));
      expect(app.location(tester), _location);

      // Shortened and sent again: the error goes with the new attempt.
      await tester.enterText(_detailsField, 'Shorter');
      await _tapSend(tester);
      await tester.pumpAndSettle();
      expect(find.text(l10n.reportSent), findsOneWidget);
      expect(_body(server.to(_reportPath).last), {
        'reason': 'harassment',
        'details': 'Shorter',
      });
    });

    testWidgets('both fields refused: each is explained beside it', (
      tester,
    ) async {
      final server = _backend()
        ..once(
          'PUT',
          _reportPath,
          (_) => jsonResponse(422, {
            'error': {
              'code': 'validation_failed',
              'fields': [
                {'field': 'reason', 'code': 'invalid'},
                {'field': 'details', 'code': 'invalid'},
              ],
            },
          }),
        );
      await _openReport(tester, server);

      await tapAndSettle(tester, find.text(l10n.reportReasonSpam));
      await tester.enterText(_detailsField, 'Text');
      await _tapSend(tester);
      await tester.pumpAndSettle();

      expect(find.text(l10n.errorReportReasonInvalid), findsOneWidget);
      expect(
        errorOf(tester, l10n.reportDetailsLabel),
        l10n.errorReportDetailsInvalid,
      );
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(_chosen(tester), ReportReason.spam);
      expect(_typed(tester), 'Text');
    });

    group('that can be retried keeps the form, with its message', () {
      final cases = <String, (Responder, String)>{
        'network': (networkFailure, l10n.errorNetwork),
        '429': (
          (_) => errorResponse(
            429,
            'rate_limited',
            headers: {'retry-after': '60'},
          ),
          'Too many attempts. Try again in 1 minute.',
        ),
        '503': (
          (_) => errorResponse(
            503,
            'service_unavailable',
            headers: {'retry-after': '5'},
          ),
          'VocaTogether is busy right now. Try again in 5 seconds.',
        ),
        '500': (
          (_) => errorResponse(500, 'internal_error'),
          l10n.errorUnexpected,
        ),
        // A report is never read back: an answer with a body is not a 204.
        'a 200 where a 204 is due': (
          (_) => jsonResponse(200, {'reason': 'spam'}),
          l10n.errorUnexpected,
        ),
        // The app never sends its own id; the answer has no text of its own.
        'member: self': (
          (_) => fieldError('member', 'self'),
          l10n.errorCheckInput,
        ),
        // Whatever a body says, the text shown is the app's own.
        'a body with text of its own': (
          (_) => http.Response(
            '{"error":{"code":"internal_error","message":"SERVERTEXT"}}',
            500,
            headers: {'content-type': 'application/json'},
          ),
          l10n.errorUnexpected,
        ),
      };
      cases.forEach((name, c) {
        final (responder, message) = c;
        testWidgets(name, (tester) async {
          final server = _backend()
            ..once('PUT', _reportPath, responder)
            ..once('PUT', _reportPath, (_) => noContent());
          final handle = tester.ensureSemantics();
          final app = await _openReport(tester, server);

          await tapAndSettle(tester, find.text(l10n.reportReasonSpam));
          await tester.enterText(_detailsField, 'Links, again');
          await _tapSend(tester);
          await tester.pumpAndSettle();

          expect(find.text(message), findsOneWidget);
          expect(find.textContaining('SERVERTEXT'), findsNothing);
          expect(
            tester.getSemantics(find.byType(FormErrorBanner)),
            isSemantics(
              isLiveRegion: true,
              label: '${l10n.errorLabel}\n$message',
            ),
          );
          expect(find.text(l10n.reportSent), findsNothing);
          expect(_chosen(tester), ReportReason.spam);
          expect(_typed(tester), 'Links, again');
          expect(errorOf(tester, l10n.reportDetailsLabel), isNull);
          expect(_enabledControls(tester), everyElement(isTrue));
          expect(server.count(_reportPath), 1);
          expect(app.session.status, SessionStatus.signedIn);
          expect(app.location(tester), _location);

          // Again, as it is: the same report, and the message goes.
          await _tapSend(tester);
          await tester.pumpAndSettle();
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(find.text(l10n.reportSent), findsOneWidget);
          expect(server.count(_reportPath), 2);
          expect(_body(server.to(_reportPath).last), {
            'reason': 'spam',
            'details': 'Links, again',
          });
          handle.dispose();
        });
      });
    });
  });

  group('a report in flight', () {
    testWidgets('one that hasn\'t answered: every control is disabled, back '
        'is ignored, and it ends as a timeout that keeps the form', (
      tester,
    ) async {
      final server = _backend()..once('PUT', _reportPath, neverAnswers);
      final app = await _openReport(tester, server);

      await tapAndSettle(tester, find.text(l10n.reportReasonSpam));
      await tester.enterText(_detailsField, 'Typed');
      await _tapSend(tester);
      await tester.pump(const Duration(seconds: 1));

      expect(_enabledControls(tester), everyElement(isFalse));
      // The button still says what it is doing for a screen reader.
      expect(find.text(l10n.reportSendButton), findsOneWidget);
      // No reason can be chosen, nothing sent again, and no way out.
      await tester.tap(find.text(l10n.reportReasonOther), warnIfMissed: false);
      await tester.tap(_send, warnIfMissed: false);
      await tester.pageBack();
      await tester.pump(const Duration(seconds: 1));
      await tester.binding.handlePopRoute();
      await tester.pump(const Duration(seconds: 1));
      expect(_screen, findsOneWidget);
      expect(_member, findsNothing);
      expect(app.location(tester), _location);
      expect(_chosen(tester), ReportReason.spam);
      expect(server.count(_reportPath), 1);

      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(find.text(l10n.reportSent), findsNothing);
      expect(_chosen(tester), ReportReason.spam);
      expect(_typed(tester), 'Typed');
      expect(_enabledControls(tester), everyElement(isTrue));
      expect(server.count(_reportPath), 1);

      // Leaving is possible again.
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(_member, findsOneWidget);
    });

    testWidgets('activating "Send report" twice sends one request', (
      tester,
    ) async {
      final reply = Completer<http.Response>();
      final server = _backend()..once('PUT', _reportPath, (_) => reply.future);
      await _openReport(tester, server);

      await tapAndSettle(tester, find.text(l10n.reportReasonSpam));
      await tester.ensureVisible(_send);
      await tester.pumpAndSettle();
      // Twice in one frame, and once more when the form is disabled.
      await tester.tap(_send);
      await tester.tap(_send, warnIfMissed: false);
      await tester.pump();
      await tester.tap(_send, warnIfMissed: false);
      await tester.pump(const Duration(seconds: 1));
      expect(server.count(_reportPath), 1);

      reply.complete(noContent());
      await tester.pumpAndSettle();
      expect(find.text(l10n.reportSent), findsOneWidget);
      expect(server.count(_reportPath), 1);
    });

    testWidgets('the session ending mid-request shows no error', (
      tester,
    ) async {
      final server = _backend()
        ..once(
          'PUT',
          _reportPath,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..once(
          'POST',
          ApiPaths.refresh,
          (_) => errorResponse(401, 'invalid_refresh_token'),
        );
      final app = await _openReport(tester, server);

      await tapAndSettle(tester, find.text(l10n.reportReasonSpam));
      await tester.enterText(_detailsField, 'Typed');
      await _tapSend(tester);
      await tester.pumpAndSettle();

      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(find.text(l10n.reportSent), findsNothing);
      expect(app.location(tester), '/login');
      expect(server.count(_reportPath), 1);
      expect(tester.takeException(), isNull);
    });

    testWidgets('logging out with the report unanswered leaves for log in, '
        'and a late answer is dropped', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()..once('PUT', _reportPath, (_) => reply.future);
      final app = await _openReport(tester, server);

      await tapAndSettle(tester, find.text(l10n.reportReasonSpam));
      await _tapSend(tester);
      await tester.pump(const Duration(seconds: 1));
      await app.session.logout();
      await tester.pumpAndSettle();
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);

      reply.complete(errorResponse(500, 'internal_error'));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(app.location(tester), '/login');
      expect(server.count(_reportPath), 1);
    });
  });
}

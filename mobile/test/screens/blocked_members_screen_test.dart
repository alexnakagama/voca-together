import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:http/http.dart' as http;
import 'package:vocatogether/api/api_paths.dart';
import 'package:vocatogether/router.dart';
import 'package:vocatogether/screens/blocked_members_screen.dart';
import 'package:vocatogether/screens/home_screen.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/session.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';

import '../support/fakes.dart';
import 'harness.dart';

/// The public identifiers of two blocked members.
const _bea = otherMemberId;
const _caro = '3e4f5a6b-7c8d-4e9f-8a0b-1c2d3e4f5a6b';

final _beaPath = ApiPaths.myBlock(_bea);
final _caroPath = ApiPaths.myBlock(_caro);

Finder get _screen => find.byType(BlockedMembersScreen);
Finder get _retry => find.widgetWithText(FilledButton, l10n.tryAgain);
Finder get _open =>
    find.widgetWithText(OutlinedButton, l10n.blockedMembersButton);

/// Every "Unblock" in the list.
Finder get _unblocks => find.descendant(
  of: _screen,
  matching: find.widgetWithText(OutlinedButton, l10n.unblockButton),
);

/// The "Unblock" beside the member called [name].
Finder _unblock(String name) => find.descendant(
  of: find.ancestor(of: find.text(name), matching: find.byType(Wrap)),
  matching: find.byType(OutlinedButton),
);

/// The answers of the confirmation.
Finder get _confirm => find.widgetWithText(TextButton, l10n.unblockButton);
Finder get _cancel => find.widgetWithText(TextButton, l10n.unblockCancel);

/// Whether the "Unblock" beside [name] takes a tap.
bool _enabled(WidgetTester tester, String name) =>
    tester.widget<OutlinedButton>(_unblock(name)).enabled;

/// A backend for a signed-in user on home; the block routes are scripted by
/// each test.
FakeServer _backend() => FakeServer()
  ..always('GET', ApiPaths.me, (_) => jsonResponse(200, meBody()))
  ..always('GET', ApiPaths.healthz, (_) => healthy())
  ..always('POST', ApiPaths.logout, (_) => noContent());

/// Caro, blocked last, and then Bea.
http.Response _two(http.Request _) =>
    jsonResponse(200, blocksBody([(_caro, 'Caro'), (_bea, 'Bea')]));

/// Signs in on home and opens the screen from its control, whose load must
/// be scripted.
Future<TestApp> _openScreen(
  WidgetTester tester,
  FakeServer server, {
  bool settle = true,
}) async {
  final app = await pumpApp(tester, server: server, signedIn: true);
  await tester.tap(_open);
  if (settle) {
    await tester.pumpAndSettle();
  } else {
    await tester.pump();
    await tester.pump();
  }
  expect(_screen, findsOneWidget);
  return app;
}

/// Opens the confirmation for the member called [name].
Future<void> _choose(WidgetTester tester, String name) async {
  await tester.tap(_unblock(name));
  await tester.pumpAndSettle();
  expect(find.text(l10n.blockedMembersUnblockMessage(name)), findsOneWidget);
}

void main() {
  group('the blocked members screen', () {
    testWidgets('the members, in the order returned, each with an "Unblock" '
        'that says whom it unblocks', (tester) async {
      final server = _backend()..once('GET', ApiPaths.myBlocks, _two);
      final handle = tester.ensureSemantics();
      final app = await _openScreen(tester, server);

      expect(app.location(tester), '/blocked');
      expect(find.text(l10n.blockedMembersTitle), findsOneWidget);
      expect(find.text('Caro'), findsOneWidget);
      expect(find.text('Bea'), findsOneWidget);
      expect(
        tester.getTopLeft(find.text('Caro')).dy,
        lessThan(tester.getTopLeft(find.text('Bea')).dy),
      );
      expect(_unblocks, findsNWidgets(2));
      for (final name in ['Caro', 'Bea']) {
        expect(
          tester.getSemantics(_unblock(name)),
          isSemantics(
            label: l10n.blockedMembersUnblockLabel(name),
            isButton: true,
            hasTapAction: true,
          ),
        );
      }
      expect(l10n.blockedMembersUnblockLabel('Bea'), 'Unblock Bea');
      expect(find.text(l10n.blockedMembersEmpty), findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);

      final request = server.to(ApiPaths.myBlocks).single;
      expect(request.method, 'GET');
      expect(request.url.path, '/v1/me/blocks');
      expect(request.url.hasQuery, isFalse);
      expect(request.bodyBytes, isEmpty);
      // Nothing about a member is asked for: no profile and no picture.
      expect(
        server.requests.where((r) => r.url.path.startsWith('/v1/profiles')),
        isEmpty,
      );
      handle.dispose();
    });

    testWidgets('nobody blocked: one text, and no "Unblock"', (tester) async {
      final server = _backend()
        ..once(
          'GET',
          ApiPaths.myBlocks,
          (_) => jsonResponse(200, blocksBody()),
        );
      await _openScreen(tester, server);

      expect(find.text(l10n.blockedMembersEmpty), findsOneWidget);
      expect(_unblocks, findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
    });

    testWidgets('a labelled spinner until the list answers', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.myBlocks, (_) => reply.future);
      final handle = tester.ensureSemantics();
      await _openScreen(tester, server, settle: false);
      await tester.pump(const Duration(seconds: 1));

      expect(find.bySemanticsLabel(l10n.blockedMembersLoading), findsOneWidget);
      expect(find.text(l10n.blockedMembersEmpty), findsNothing);
      expect(_unblocks, findsNothing);

      reply.complete(_two(http.Request('GET', Uri())));
      await tester.pumpAndSettle();
      expect(find.bySemanticsLabel(l10n.blockedMembersLoading), findsNothing);
      expect(_unblocks, findsNWidgets(2));
      handle.dispose();
    });

    group('a failed load shows its message and "Try again", which loads '
        'again', () {
      final cases = <String, (Responder, String)>{
        'network': (networkFailure, l10n.errorNetwork),
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
        // A missing list is never read as "nobody blocked".
        'a 200 with no list': (
          (_) => jsonResponse(200, {'blocks': null}),
          l10n.errorUnexpected,
        ),
        'a 200 with a malformed entry': (
          (_) => jsonResponse(200, {
            'blocks': [
              {'id': _bea, 'display_name': 'Bea'},
              {'id': _caro},
            ],
          }),
          l10n.errorUnexpected,
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
            ..once('GET', ApiPaths.myBlocks, responder)
            ..once('GET', ApiPaths.myBlocks, _two);
          final app = await _openScreen(tester, server);

          expect(find.text(message), findsOneWidget);
          expect(find.textContaining('SERVERTEXT'), findsNothing);
          expect(find.text(l10n.blockedMembersEmpty), findsNothing);
          expect(find.text('Bea'), findsNothing);
          expect(_unblocks, findsNothing);
          expect(app.session.status, SessionStatus.signedIn);
          expect(app.location(tester), '/blocked');

          await tapAndSettle(tester, _retry);
          expect(find.text(message), findsNothing);
          expect(_unblocks, findsNWidgets(2));
          expect(server.count(ApiPaths.myBlocks), 2);
        });
      });

      testWidgets('no answer in time', (tester) async {
        final server = _backend()
          ..once('GET', ApiPaths.myBlocks, neverAnswers)
          ..once('GET', ApiPaths.myBlocks, _two);
        await _openScreen(tester, server, settle: false);
        await tester.pump(const Duration(seconds: 16));
        await tester.pumpAndSettle();
        expect(find.text(l10n.errorTimeout), findsOneWidget);

        await tapAndSettle(tester, _retry);
        expect(_unblocks, findsNWidgets(2));
      });
    });

    testWidgets('the list is requested again on each entry', (tester) async {
      final server = _backend()
        ..once('GET', ApiPaths.myBlocks, _two)
        ..once(
          'GET',
          ApiPaths.myBlocks,
          (_) => jsonResponse(200, blocksBody([(_bea, 'Beatriz')])),
        );
      await _openScreen(tester, server);
      expect(find.text('Caro'), findsOneWidget);

      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(_screen, findsNothing);

      await tapAndSettle(tester, _open);
      expect(server.count(ApiPaths.myBlocks), 2);
      // What the server says now, and nothing kept from before.
      expect(find.text('Caro'), findsNothing);
      expect(find.text('Beatriz'), findsOneWidget);
      expect(_unblocks, findsOneWidget);
    });

    testWidgets('a list answered after leaving is dropped', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.myBlocks, (_) => reply.future);
      await _openScreen(tester, server, settle: false);

      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(find.byType(HomeScreen), findsOneWidget);

      reply.complete(errorResponse(500, 'internal_error'));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.byType(FormErrorBanner), findsNothing);
    });

    testWidgets('a session that ended during the load shows no error', (
      tester,
    ) async {
      final server = _backend()
        ..once(
          'GET',
          ApiPaths.myBlocks,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..once(
          'POST',
          ApiPaths.refresh,
          (_) => errorResponse(401, 'invalid_refresh_token'),
        );
      final app = await pumpApp(tester, server: server, signedIn: true);
      await tester.tap(_open);

      // Every frame until the router has moved on shows no error.
      for (var i = 0; i < 30; i++) {
        await tester.pump(const Duration(milliseconds: 50));
        expect(find.byType(FormErrorBanner), findsNothing);
      }
      await tester.pumpAndSettle();
      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(app.location(tester), '/login');
    });

    testWidgets('a signed-out user is shown log in, and nothing is asked', (
      tester,
    ) async {
      final server = FakeServer();
      final app = await pumpApp(tester, server: server);

      GoRouter.of(tester.element(find.byType(Navigator).first))
          .go(Routes.blocked);
      await tester.pumpAndSettle();

      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(app.location(tester), '/login');
      expect(server.requests, isEmpty);
    });
  });

  group('unblocking', () {
    testWidgets('"Unblock" asks first, naming the member; dismissing it '
        'sends nothing', (tester) async {
      final server = _backend()..once('GET', ApiPaths.myBlocks, _two);
      await _openScreen(tester, server);

      await _choose(tester, 'Bea');
      expect(find.text(l10n.unblockTitle), findsOneWidget);
      // Nothing is sent before the answer.
      expect(server.count(_beaPath), 0);

      await tester.tap(_cancel);
      await tester.pumpAndSettle();
      expect(find.text(l10n.unblockTitle), findsNothing);

      // Dismissed by a tap outside it, and by back.
      await _choose(tester, 'Bea');
      await tester.tapAt(const Offset(5, 5));
      await tester.pumpAndSettle();
      expect(find.text(l10n.unblockTitle), findsNothing);
      await _choose(tester, 'Bea');
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.text(l10n.unblockTitle), findsNothing);

      expect(_screen, findsOneWidget);
      expect(find.text('Caro'), findsOneWidget);
      expect(find.text('Bea'), findsOneWidget);
      expect(_unblocks, findsNWidgets(2));
      expect(_enabled(tester, 'Bea'), isTrue);
      expect(server.requests.map((r) => r.method).toSet(), {'GET'});
    });

    testWidgets('confirming sends one unblock for that member, who leaves '
        'the list without a reload; after the last one, the empty text', (
      tester,
    ) async {
      final server = _backend()
        ..once('GET', ApiPaths.myBlocks, _two)
        ..once('DELETE', _beaPath, (_) => noContent())
        ..once('DELETE', _caroPath, (_) => noContent());
      final app = await _openScreen(tester, server);

      await _choose(tester, 'Bea');
      await tester.tap(_confirm);
      await tester.pumpAndSettle();

      final request = server.to(_beaPath).single;
      expect(request.method, 'DELETE');
      expect(request.url.path, '/v1/me/blocks/$_bea');
      expect(request.url.hasQuery, isFalse);
      expect(request.bodyBytes, isEmpty);

      expect(find.text('Bea'), findsNothing);
      expect(find.text('Caro'), findsOneWidget);
      expect(_unblocks, findsOneWidget);
      expect(_enabled(tester, 'Caro'), isTrue);
      expect(find.text(l10n.blockedMembersEmpty), findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(find.byType(LinearProgressIndicator), findsNothing);
      expect(app.location(tester), '/blocked');
      // The list was read once, and nothing but the unblock was written.
      expect(server.count(ApiPaths.myBlocks), 1);
      expect(server.count(_caroPath), 0);
      expect(server.requests.where((r) => r.method != 'GET'), hasLength(1));

      await _choose(tester, 'Caro');
      await tester.tap(_confirm);
      await tester.pumpAndSettle();
      expect(server.to(_caroPath).single.method, 'DELETE');
      expect(find.text('Caro'), findsNothing);
      expect(_unblocks, findsNothing);
      expect(find.text(l10n.blockedMembersEmpty), findsOneWidget);
      expect(server.count(ApiPaths.myBlocks), 1);
    });

    group('a failed unblock keeps the member in the list, with its message, '
        'and can be tried again', () {
      final cases = <String, (Responder, String)>{
        'network': (networkFailure, l10n.errorNetwork),
        '429': (
          (_) => errorResponse(
            429,
            'rate_limited',
            headers: {'retry-after': '10'},
          ),
          'Too many attempts. Try again in 10 seconds.',
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
        'a 200 where a 204 is due': (
          (_) => jsonResponse(200, {'unblocked': true}),
          l10n.errorUnexpected,
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
            ..once('GET', ApiPaths.myBlocks, _two)
            ..once('DELETE', _beaPath, responder)
            ..once('DELETE', _beaPath, (_) => noContent());
          final handle = tester.ensureSemantics();
          final app = await _openScreen(tester, server);

          await _choose(tester, 'Bea');
          await tester.tap(_confirm);
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
          expect(find.text('Caro'), findsOneWidget);
          expect(find.text('Bea'), findsOneWidget);
          expect(_enabled(tester, 'Bea'), isTrue);
          expect(_enabled(tester, 'Caro'), isTrue);
          expect(find.byType(LinearProgressIndicator), findsNothing);
          expect(server.count(_beaPath), 1);
          expect(server.count(ApiPaths.myBlocks), 1);
          expect(app.session.status, SessionStatus.signedIn);
          expect(app.location(tester), '/blocked');

          // Again: the message goes with the new attempt.
          await _choose(tester, 'Bea');
          await tester.tap(_confirm);
          await tester.pumpAndSettle();
          expect(find.byType(FormErrorBanner), findsNothing);
          expect(find.text('Bea'), findsNothing);
          expect(find.text('Caro'), findsOneWidget);
          expect(server.count(_beaPath), 2);
          handle.dispose();
        });
      });
    });

    testWidgets('an unblock that hasn\'t answered: no other "Unblock" is '
        'accepted, and it ends as a timeout', (tester) async {
      final server = _backend()
        ..once('GET', ApiPaths.myBlocks, _two)
        ..once('DELETE', _beaPath, neverAnswers);
      final handle = tester.ensureSemantics();
      final app = await _openScreen(tester, server);

      await _choose(tester, 'Bea');
      await tester.tap(_confirm);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));

      expect(find.text(l10n.unblockTitle), findsNothing);
      expect(find.bySemanticsLabel(l10n.unblockProgress), findsOneWidget);
      // Both members until the answer, and neither control takes a tap.
      expect(find.text('Bea'), findsOneWidget);
      expect(_enabled(tester, 'Bea'), isFalse);
      expect(_enabled(tester, 'Caro'), isFalse);
      await tester.tap(_unblock('Caro'), warnIfMissed: false);
      await tester.tap(_unblock('Bea'), warnIfMissed: false);
      await tester.pump(const Duration(seconds: 1));
      expect(find.byType(AlertDialog), findsNothing);
      expect(server.count(_beaPath), 1);
      expect(server.count(_caroPath), 0);
      expect(app.location(tester), '/blocked');

      await tester.pump(const Duration(seconds: 16));
      await tester.pumpAndSettle();
      expect(find.text(l10n.errorTimeout), findsOneWidget);
      expect(find.bySemanticsLabel(l10n.unblockProgress), findsNothing);
      expect(find.text('Bea'), findsOneWidget);
      expect(_enabled(tester, 'Bea'), isTrue);
      expect(_enabled(tester, 'Caro'), isTrue);
      expect(server.count(_beaPath), 1);
      handle.dispose();
    });

    testWidgets('confirming twice sends one unblock', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.myBlocks, _two)
        ..once('DELETE', _beaPath, (_) => reply.future);
      await _openScreen(tester, server);

      await _choose(tester, 'Bea');
      await tester.tap(_confirm);
      await tester.pump();
      // The dialog is on its way out and the list is disabled.
      await tester.tap(_confirm, warnIfMissed: false);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      await tester.tap(_unblock('Caro'), warnIfMissed: false);
      await tester.pump(const Duration(seconds: 1));

      expect(find.byType(AlertDialog), findsNothing);
      expect(server.count(_beaPath), 1);
      expect(server.count(_caroPath), 0);

      reply.complete(noContent());
      await tester.pumpAndSettle();
      expect(find.text('Bea'), findsNothing);
      expect(find.text('Caro'), findsOneWidget);
      expect(server.count(_beaPath), 1);
    });

    testWidgets('an unblock answered after leaving is dropped', (tester) async {
      final reply = Completer<http.Response>();
      final server = _backend()
        ..once('GET', ApiPaths.myBlocks, _two)
        ..once('DELETE', _beaPath, (_) => reply.future);
      final app = await _openScreen(tester, server);

      await _choose(tester, 'Bea');
      await tester.tap(_confirm);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(_screen, findsNothing);

      reply.complete(errorResponse(500, 'internal_error'));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(app.location(tester), '/home');
      expect(server.count(_beaPath), 1);
    });

    testWidgets('the session ending with the confirmation open shows no '
        'error, and nothing is sent', (tester) async {
      final server = _backend()..once('GET', ApiPaths.myBlocks, _two);
      final app = await _openScreen(tester, server);

      await _choose(tester, 'Bea');
      await app.session.logout();
      await tester.pumpAndSettle();

      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(app.location(tester), '/login');
      expect(server.count(_beaPath), 0);
      expect(tester.takeException(), isNull);
    });

    testWidgets('an unblock refused because the session ended shows no '
        'error', (tester) async {
      final server = _backend()
        ..once('GET', ApiPaths.myBlocks, _two)
        ..once(
          'DELETE',
          _beaPath,
          (_) => errorResponse(401, 'invalid_access_token'),
        )
        ..once(
          'POST',
          ApiPaths.refresh,
          (_) => errorResponse(401, 'invalid_refresh_token'),
        );
      final app = await _openScreen(tester, server);

      await _choose(tester, 'Bea');
      await tester.tap(_confirm);
      await tester.pumpAndSettle();

      expect(app.session.status, SessionStatus.signedOut);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(_screen, findsNothing);
      expect(find.byType(FormErrorBanner), findsNothing);
      expect(app.location(tester), '/login');
      expect(server.count(_beaPath), 1);
      expect(tester.takeException(), isNull);
    });
  });
}

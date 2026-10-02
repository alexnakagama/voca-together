import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/session.dart';

void main() {
  test('starts unknown by default', () {
    final session = Session();
    addTearDown(session.dispose);

    expect(session.status, SessionStatus.unknown);
  });

  test('honors the initial status', () {
    final session = Session(initial: SessionStatus.signedIn);
    addTearDown(session.dispose);

    expect(session.status, SessionStatus.signedIn);
  });

  test('notifies once per change and not when unchanged', () {
    final session = Session();
    addTearDown(session.dispose);
    var notifications = 0;
    session.addListener(() => notifications++);

    session.markSignedOut();
    expect(session.status, SessionStatus.signedOut);
    expect(notifications, 1);

    session.markSignedOut();
    expect(notifications, 1);

    session.markSignedIn();
    expect(session.status, SessionStatus.signedIn);
    expect(notifications, 2);

    session.markSignedIn();
    expect(notifications, 2);

    session.markSignedOut();
    expect(session.status, SessionStatus.signedOut);
    expect(notifications, 3);
  });
}

import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/app.dart';
import 'package:vocatogether/config.dart';
import 'package:vocatogether/screens/login_screen.dart';
import 'package:vocatogether/session.dart';

import 'support/fakes.dart';

void main() {
  testWidgets('signed-out app opens on log in', (tester) async {
    final server = FakeServer();
    final session = SessionManager(
      store: InMemoryTokenStore(),
      authApi: authApiFor(server.client),
      clock: FakeAuthClock(),
    );
    addTearDown(session.dispose);
    await session.restore();

    await tester.pumpWidget(
      VocaTogetherApp(
        config: AppConfig(apiBaseUrl: Uri.parse('http://10.0.2.2:8080')),
        session: session,
        accountApi: accountApiFor(server.client),
        photoSource: FakePhotoSource(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(LoginScreen), findsOneWidget);
    // Title and submit button.
    expect(find.text('Log in'), findsNWidgets(2));
  });
}

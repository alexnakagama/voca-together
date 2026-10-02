import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/app.dart';
import 'package:vocatogether/config.dart';
import 'package:vocatogether/session.dart';

void main() {
  testWidgets('signed-out app opens on log in', (tester) async {
    final session = Session(initial: SessionStatus.signedOut);
    addTearDown(session.dispose);

    await tester.pumpWidget(
      VocaTogetherApp(
        config: AppConfig(apiBaseUrl: Uri.parse('http://10.0.2.2:8080')),
        session: session,
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Log in'), findsOneWidget);
  });
}

import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/config.dart';
import 'package:vocatogether/main.dart';

void main() {
  testWidgets('app shell renders', (tester) async {
    await tester.pumpWidget(
      VocaTogetherApp(
        config: AppConfig(apiBaseUrl: Uri.parse('http://10.0.2.2:8080')),
      ),
    );

    expect(find.text('VocaTogether'), findsOneWidget);
  });
}

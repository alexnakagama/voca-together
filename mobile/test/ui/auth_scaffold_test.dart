import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vocatogether/ui/widgets/app_text_field.dart';
import 'package:vocatogether/ui/widgets/auth_scaffold.dart';
import 'package:vocatogether/ui/widgets/form_error_banner.dart';
import 'package:vocatogether/ui/widgets/google_sign_in_button.dart';
import 'package:vocatogether/ui/widgets/primary_button.dart';

import 'harness.dart';

const _last = Key('last');

Widget _login() => AuthScaffold(
  title: 'Log in',
  children: [
    const FormErrorBanner(message: 'Incorrect email or password.'),
    const AppTextField.email(),
    const AppTextField.password(),
    PrimaryButton(label: 'Log in', onPressed: () {}),
    GoogleSignInButton(onPressed: () {}),
    TextButton(
      key: _last,
      onPressed: () {},
      child: const Text('Forgot password?'),
    ),
  ],
);

void main() {
  testWidgets('small screen, large text and keyboard: scrolls, no overflow', (
    tester,
  ) async {
    await pumpUi(
      tester,
      _login(),
      page: true,
      size: const Size(320, 480),
      textScale: 2,
      viewInsets: const EdgeInsets.only(bottom: 300),
    );
    expect(tester.takeException(), isNull);

    await tester.scrollUntilVisible(
      find.byKey(_last),
      100,
      scrollable: find.byType(Scrollable).first,
    );
    expect(tester.takeException(), isNull);
    expect(find.byKey(_last).hitTestable(), findsOneWidget);
  });

  testWidgets('content is centered vertically when it fits', (tester) async {
    await pumpUi(
      tester,
      const AuthScaffold(title: 'Log in', children: [Text('body')]),
      page: true,
      size: const Size(400, 900),
    );
    final top = tester.getTopLeft(find.text('Log in')).dy;
    final bottom = tester.getBottomLeft(find.text('body')).dy;
    expect((top - (900 - bottom)).abs(), lessThan(2));
  });

  testWidgets('content width is capped on wide screens', (tester) async {
    await pumpUi(tester, _login(), page: true, size: const Size(1000, 800));
    expect(
      tester.getSize(find.byType(PrimaryButton)).width,
      AuthScaffold.maxContentWidth,
    );
  });

  testWidgets('the title is a heading', (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, _login(), page: true);
    expect(
      tester.getSemantics(find.text('Log in').first),
      isSemantics(label: 'Log in', isHeader: true),
    );
    handle.dispose();
  });

  testWidgets('a focused bottom field is scrolled above the keyboard', (
    tester,
  ) async {
    final focus = FocusNode();
    addTearDown(focus.dispose);
    Widget page() => AuthScaffold(
      title: 'Log in',
      children: [
        for (var i = 0; i < 6; i++) Text('line $i'),
        AppTextField.password(focusNode: focus),
      ],
    );

    await pumpUi(tester, page(), page: true, size: const Size(360, 640));
    focus.requestFocus();
    await pumpUi(
      tester,
      page(),
      page: true,
      size: const Size(360, 640),
      viewInsets: const EdgeInsets.only(bottom: 400),
    );
    await tester.pumpAndSettle();

    final fieldBottom = tester.getBottomLeft(find.byType(TextField)).dy;
    expect(fieldBottom, lessThanOrEqualTo(640 - 400));
  });
}

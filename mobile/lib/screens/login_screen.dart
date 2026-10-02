import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../router.dart';

/// Placeholder for the login form.
class LoginScreen extends StatelessWidget {
  const LoginScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Log in')),
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextButton(
              onPressed: () => context.go(Routes.register),
              child: const Text('Create account'),
            ),
            TextButton(
              onPressed: () => context.go(Routes.forgotPassword),
              child: const Text('Forgot password?'),
            ),
          ],
        ),
      ),
    );
  }
}

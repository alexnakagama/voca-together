import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../router.dart';

/// Placeholder for the password reset request form.
class ForgotPasswordScreen extends StatelessWidget {
  const ForgotPasswordScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Reset password')),
      body: Center(
        child: TextButton(
          onPressed: () => context.go(Routes.login),
          child: const Text('Back to log in'),
        ),
      ),
    );
  }
}

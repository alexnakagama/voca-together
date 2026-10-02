import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../router.dart';

/// Placeholder for the registration form.
class RegisterScreen extends StatelessWidget {
  const RegisterScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Create account')),
      body: Center(
        child: TextButton(
          onPressed: () => context.go(Routes.login),
          child: const Text('Back to log in'),
        ),
      ),
    );
  }
}

import 'package:flutter/material.dart';

/// Shown while startup determines whether a session exists.
class SplashScreen extends StatelessWidget {
  const SplashScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return const Scaffold(
      body: Center(child: CircularProgressIndicator(semanticsLabel: 'Loading')),
    );
  }
}

import 'package:flutter/material.dart';

/// Placeholder for the signed-in home screen.
class HomeScreen extends StatelessWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Home')),
      body: const Center(child: Text('VocaTogether')),
    );
  }
}

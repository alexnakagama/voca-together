import 'package:flutter/material.dart';

import 'config.dart';

void main() {
  // Validate configuration before anything else, so a misconfigured build
  // fails at once.
  final config = AppConfig.fromEnvironment();
  runApp(VocaTogetherApp(config: config));
}

class VocaTogetherApp extends StatelessWidget {
  const VocaTogetherApp({super.key, required this.config});

  final AppConfig config;

  @override
  Widget build(BuildContext context) {
    return const MaterialApp(
      title: 'VocaTogether',
      home: Scaffold(body: Center(child: Text('VocaTogether'))),
    );
  }
}

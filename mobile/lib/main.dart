import 'package:flutter/material.dart';

import 'app.dart';
import 'config.dart';
import 'session.dart';

/// The composition root: every long-lived object is built here and passed
/// down through constructors.
void main() {
  // Validate configuration before anything else, so a misconfigured build
  // fails at once.
  final config = AppConfig.fromEnvironment();
  final session = Session();
  // There is no stored session to restore yet, so startup resolves at once.
  session.markSignedOut();
  runApp(VocaTogetherApp(config: config, session: session));
}

import 'package:flutter/material.dart';

import '../l10n/app_localizations.dart';

/// Shown while startup determines whether a session exists.
class SplashScreen extends StatelessWidget {
  const SplashScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Center(
        child: CircularProgressIndicator(
          semanticsLabel: AppLocalizations.of(context).splashLoading,
        ),
      ),
    );
  }
}

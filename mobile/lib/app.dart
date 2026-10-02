import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import 'config.dart';
import 'l10n/app_localizations.dart';
import 'router.dart';
import 'session.dart';
import 'ui/theme.dart';

/// The application root. Owns the router; [session] belongs to the caller.
class VocaTogetherApp extends StatefulWidget {
  const VocaTogetherApp({
    super.key,
    required this.config,
    required this.session,
  });

  final AppConfig config;
  final Session session;

  @override
  State<VocaTogetherApp> createState() => _VocaTogetherAppState();
}

class _VocaTogetherAppState extends State<VocaTogetherApp> {
  late GoRouter _router;

  @override
  void initState() {
    super.initState();
    _router = createRouter(widget.session);
  }

  @override
  void didUpdateWidget(VocaTogetherApp oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.session != oldWidget.session) {
      _router.dispose();
      _router = createRouter(widget.session);
    }
  }

  @override
  void dispose() {
    _router.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp.router(
      onGenerateTitle: (context) => AppLocalizations.of(context).appTitle,
      theme: AppTheme.light,
      darkTheme: AppTheme.dark,
      localizationsDelegates: AppLocalizations.localizationsDelegates,
      supportedLocales: AppLocalizations.supportedLocales,
      routerConfig: _router,
    );
  }
}

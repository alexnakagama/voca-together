import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import 'api/account_api.dart';
import 'config.dart';
import 'l10n/app_localizations.dart';
import 'media/photo_source.dart';
import 'router.dart';
import 'session.dart';
import 'ui/theme.dart';

/// The application root. Owns the router; [session], [accountApi] and
/// [photoSource] belong to the caller.
class VocaTogetherApp extends StatefulWidget {
  const VocaTogetherApp({
    super.key,
    required this.config,
    required this.session,
    required this.accountApi,
    required this.photoSource,
  });

  final AppConfig config;
  final SessionManager session;
  final AccountApi accountApi;
  final PhotoSource photoSource;

  @override
  State<VocaTogetherApp> createState() => _VocaTogetherAppState();
}

class _VocaTogetherAppState extends State<VocaTogetherApp> {
  late GoRouter _router;

  @override
  void initState() {
    super.initState();
    _router = _createRouter();
  }

  GoRouter _createRouter() =>
      createRouter(widget.session, widget.accountApi, widget.photoSource);

  @override
  void didUpdateWidget(VocaTogetherApp oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.session != oldWidget.session ||
        widget.accountApi != oldWidget.accountApi ||
        widget.photoSource != oldWidget.photoSource) {
      _router.dispose();
      _router = _createRouter();
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

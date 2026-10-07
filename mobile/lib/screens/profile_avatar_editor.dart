import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../l10n/app_localizations.dart';
import '../media/photo_source.dart';
import '../session.dart';
import '../ui/theme.dart';
import '../ui/widgets/form_error_banner.dart';
import '../ui/widgets/profile_avatar.dart';
import '../ui/widgets/secondary_button.dart';
import 'failure_presentation.dart';

/// The picture control of the profile form: the member's own picture or its
/// placeholder, a button that opens the device's photo chooser and, with a
/// picture, one that removes it (`GET`, `PUT` and `DELETE /v1/me/avatar`
/// through [SessionManager], decision 032).
///
/// A chosen photo is uploaded at once and a removal is sent at once, apart
/// from the form's Save: the picture and the text are two resources. The
/// photo is sent as the device gave it; the server decides what a picture
/// is, and the control then shows the bytes the server stored. A failure
/// keeps the picture as it was and shows its message under the control.
///
/// It loads by itself, and a picture that can't be loaded is the
/// placeholder. From the moment the chooser opens until the request has
/// answered it reports [onBusyChanged], so the form can lock itself and
/// hold back leaving, as it does for its own save.
class ProfileAvatarEditor extends StatefulWidget {
  const ProfileAvatarEditor({
    super.key,
    required this.session,
    required this.photoSource,
    required this.name,
    required this.enabled,
    required this.onBusyChanged,
  });

  final SessionManager session;
  final PhotoSource photoSource;

  /// The member's stored name, for the placeholder's initial; empty before
  /// a profile exists.
  final String name;

  /// False while the form is saving: the buttons are disabled.
  final bool enabled;

  /// Called with true when a picture action starts and false when it ends.
  final ValueChanged<bool> onBusyChanged;

  @override
  State<ProfileAvatarEditor> createState() => _ProfileAvatarEditorState();
}

/// The picture action in flight.
enum _Action { choose, remove }

class _ProfileAvatarEditorState extends State<ProfileAvatarEditor> {
  /// The picture hasn't answered yet. Nothing can be started until it has:
  /// whether there is one decides what is offered.
  bool _loading = true;

  /// The picture as the server stored it, or null with none.
  Uint8List? _image;

  _Action? _action;
  String? _error;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    Uint8List? image;
    try {
      image = await widget.session.avatar();
    } on Exception {
      // The placeholder, with the control to choose a picture: the form
      // doesn't depend on this, and an upload replaces whatever is stored.
    }
    if (!mounted) return;
    setState(() {
      _image = image;
      _loading = false;
    });
  }

  bool get _available => widget.enabled && !_loading && _action == null;

  void _start(_Action action) {
    setState(() {
      _action = action;
      _error = null;
    });
    widget.onBusyChanged(true);
  }

  void _finish({Uint8List? image, bool changed = false, String? error}) {
    setState(() {
      if (changed) _image = image;
      _action = null;
      _error = error;
    });
    widget.onBusyChanged(false);
  }

  /// Ends the action with [e]'s message, unless the session is gone: the
  /// router is then already leaving the form, which stays locked.
  void _fail(Exception e) {
    final failure = presentFailure(e, AppLocalizations.of(context));
    if (failure.kind == FailureKind.sessionEnded) return;
    _finish(error: failure.avatarError ?? failure.message);
  }

  Future<void> _choose() async {
    if (!_available) return;
    _start(_Action.choose);
    try {
      final photo = await widget.photoSource.pick();
      if (!mounted) return;
      if (photo == null) {
        // The chooser was closed without choosing.
        _finish();
        return;
      }
      // As the device gave it: the server validates and normalizes (031).
      final stored = await widget.session.saveAvatar(photo);
      if (!mounted) return;
      _finish(image: stored, changed: true);
    } on Exception catch (e) {
      if (!mounted) return;
      _fail(e);
    }
  }

  Future<void> _remove() async {
    if (!_available) return;
    setState(() => _error = null);
    final l10n = AppLocalizations.of(context);
    final remove = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        scrollable: true,
        title: Text(l10n.profileAvatarRemoveTitle),
        // The answers scroll with the text, as in the form's own dialog:
        // `actions` stay outside what scrolls.
        content: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(l10n.profileAvatarRemoveMessage),
            const SizedBox(height: Spacing.md),
            OverflowBar(
              alignment: MainAxisAlignment.end,
              overflowAlignment: OverflowBarAlignment.end,
              spacing: Spacing.sm,
              children: [
                TextButton(
                  onPressed: () => Navigator.of(context).pop(false),
                  child: Text(l10n.profileAvatarRemoveKeep),
                ),
                TextButton(
                  onPressed: () => Navigator.of(context).pop(true),
                  child: Text(l10n.profileAvatarRemoveConfirm),
                ),
              ],
            ),
          ],
        ),
      ),
    );
    if (remove != true || !mounted || !_available) return;
    _start(_Action.remove);
    try {
      await widget.session.removeAvatar();
      if (!mounted) return;
      _finish(image: null, changed: true);
    } on Exception catch (e) {
      if (!mounted) return;
      _fail(e);
    }
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final image = _image;
    final error = _error;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Center(
          child: _loading
              ? SizedBox.square(
                  dimension: ProfileAvatar.defaultSize,
                  child: Center(
                    child: CircularProgressIndicator(
                      semanticsLabel: l10n.profileAvatarLoading,
                    ),
                  ),
                )
              : ProfileAvatar(
                  name: widget.name,
                  semanticLabel: image == null
                      ? l10n.profileAvatarPlaceholderLabel
                      : l10n.profileAvatarLabel,
                  image: image,
                ),
        ),
        if (!_loading) ...[
          const SizedBox(height: Spacing.md),
          SecondaryButton(
            label: image == null
                ? l10n.profileAvatarAddButton
                : l10n.profileAvatarChangeButton,
            onPressed: _available ? () => unawaited(_choose()) : null,
            busy: _action == _Action.choose,
          ),
          if (image != null) ...[
            const SizedBox(height: Spacing.sm),
            SecondaryButton(
              label: l10n.profileAvatarRemoveButton,
              onPressed: _available ? () => unawaited(_remove()) : null,
              busy: _action == _Action.remove,
            ),
          ],
        ],
        if (error != null) ...[
          const SizedBox(height: Spacing.md),
          FormErrorBanner(message: error),
        ],
      ],
    );
  }
}

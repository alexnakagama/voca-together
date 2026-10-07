import 'dart:typed_data';

import 'package:flutter/material.dart';

/// A member's profile picture as a circle, or a placeholder with the first
/// character of their [name] when there is no picture. Not a control: it
/// takes no tap.
///
/// [image] is the encoded picture as the server returned it. Bytes that
/// can't be decoded show the placeholder, with no error. Screen readers get
/// one image described by [semanticLabel], which is passed in already
/// localized; the initial is not read on its own.
class ProfileAvatar extends StatelessWidget {
  const ProfileAvatar({
    super.key,
    required this.name,
    required this.semanticLabel,
    this.image,
    this.size = defaultSize,
  });

  static const double defaultSize = 96;

  /// The member's name, for the initial. May be empty (no profile yet): the
  /// placeholder then shows an icon.
  final String name;

  final String semanticLabel;

  /// The picture, or null for the placeholder.
  final Uint8List? image;

  /// The circle's diameter, in logical pixels.
  final double size;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final image = this.image;
    return Semantics(
      image: true,
      label: semanticLabel,
      child: ExcludeSemantics(
        child: SizedBox.square(
          dimension: size,
          child: ClipOval(
            child: ColoredBox(
              color: scheme.primaryContainer,
              child: image == null
                  ? _placeholder(scheme)
                  : Image.memory(
                      image,
                      width: size,
                      height: size,
                      fit: BoxFit.cover,
                      gaplessPlayback: true,
                      errorBuilder: (context, error, stackTrace) =>
                          _placeholder(scheme),
                    ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _placeholder(ColorScheme scheme) {
    // The first grapheme cluster, so an emoji or a letter with combining
    // marks is not cut in half.
    final initial = name.trim().characters.firstOrNull;
    return Center(
      child: initial == null
          ? Icon(
              Icons.person,
              size: size * 0.5,
              color: scheme.onPrimaryContainer,
            )
          : Padding(
              padding: EdgeInsets.all(size * 0.2),
              // The circle has a fixed size, so the initial follows it and
              // not the system's text size; a wide cluster is scaled down.
              child: FittedBox(
                fit: BoxFit.scaleDown,
                child: Text(
                  initial,
                  maxLines: 1,
                  textScaler: TextScaler.noScaling,
                  style: TextStyle(
                    fontSize: size * 0.4,
                    fontWeight: FontWeight.w500,
                    color: scheme.onPrimaryContainer,
                  ),
                ),
              ),
            ),
    );
  }
}

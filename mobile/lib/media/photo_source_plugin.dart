import 'dart:typed_data';

import 'package:image_picker/image_picker.dart';

import 'photo_source.dart';

/// [PhotoSource] over the `image_picker` plugin, and the only library that
/// imports it (decision 032).
///
/// It opens the system's own chooser for one image from the device's photos,
/// which needs no storage, media or camera permission, and never the camera.
/// It asks the plugin for a copy scaled down to fit [maxSide], so an upload
/// is a few hundred kilobytes. That is a transfer optimisation, not a rule:
/// the server still validates, crops and scales what it gets. Nothing is
/// kept here, and nothing the plugin reports is logged.
class PluginPhotoSource implements PhotoSource {
  PluginPhotoSource({ImagePicker? picker}) : _picker = picker ?? ImagePicker();

  /// The longest side, in pixels, of the copy the plugin is asked for.
  static const double maxSide = 1024;

  /// The JPEG quality of that copy.
  static const quality = 85;

  final ImagePicker _picker;

  @override
  Future<Uint8List?> pick() async {
    try {
      final file = await _picker.pickImage(
        source: ImageSource.gallery,
        maxWidth: maxSide,
        maxHeight: maxSide,
        imageQuality: quality,
      );
      if (file == null) return null;
      return await file.readAsBytes();
    } on Object {
      // A PlatformException, a file that is gone or can't be read, an Error
      // from the plugin: none of it is the caller's to interpret, and its
      // text isn't kept.
      throw const PhotoSourceException();
    }
  }
}

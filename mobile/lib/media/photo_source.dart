import 'dart:typed_data';

/// Where the app gets a photo the member chooses from their device
/// (decision 032).
///
/// `main` builds the one implementation and it is passed down by constructor
/// to the screen that offers the choice. Tests use a fake.
abstract interface class PhotoSource {
  /// Asks the member to choose one photo and returns its bytes, or null when
  /// they close the chooser without choosing.
  ///
  /// The bytes are whatever the device gave: nothing about their format, size
  /// or dimensions is checked here. The server decides what a picture is.
  ///
  /// Throws [PhotoSourceException] when a photo was chosen and can't be read,
  /// or the chooser can't be shown.
  Future<Uint8List?> pick();
}

/// The device gave the app no usable photo.
///
/// Safe to log or show: it holds nothing. Whatever the platform said (its
/// messages can name a file) is dropped where this is created.
final class PhotoSourceException implements Exception {
  const PhotoSourceException();

  @override
  String toString() => 'PhotoSourceException';
}

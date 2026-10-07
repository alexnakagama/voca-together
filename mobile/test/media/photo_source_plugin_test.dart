import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image_picker/image_picker.dart';
import 'package:vocatogether/media/photo_source.dart';
import 'package:vocatogether/media/photo_source_plugin.dart';

/// What one `pickImage` call asked the plugin for.
typedef _Pick = ({
  ImageSource source,
  double? maxWidth,
  double? maxHeight,
  int? imageQuality,
});

/// The plugin's front, scripted: each `pickImage` call takes the next
/// outcome, and an unscripted call fails the test.
class _FakePicker extends ImagePicker {
  final picks = <_Pick>[];
  final _script = <Future<XFile?> Function()>[];

  void answer(XFile? file) => _script.add(() async => file);
  void fail(Object error) => _script.add(() async => throw error);

  @override
  Future<XFile?> pickImage({
    required ImageSource source,
    double? maxWidth,
    double? maxHeight,
    int? imageQuality,
    CameraDevice preferredCameraDevice = CameraDevice.rear,
    bool requestFullMetadata = true,
  }) {
    picks.add((
      source: source,
      maxWidth: maxWidth,
      maxHeight: maxHeight,
      imageQuality: imageQuality,
    ));
    if (_script.isEmpty) throw StateError('unscripted pick');
    return _script.removeAt(0)();
  }
}

final _photo = Uint8List.fromList([0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3]);

void main() {
  late _FakePicker picker;
  late PluginPhotoSource source;

  setUp(() {
    picker = _FakePicker();
    source = PluginPhotoSource(picker: picker);
  });

  test('a chosen photo is returned as its bytes, unchanged', () async {
    picker.answer(XFile.fromData(_photo));

    expect(await source.pick(), _photo);
  });

  test('asks for one gallery image, scaled down by the plugin', () async {
    picker.answer(XFile.fromData(_photo));

    await source.pick();

    expect(picker.picks, [
      (
        source: ImageSource.gallery,
        maxWidth: 1024.0,
        maxHeight: 1024.0,
        imageQuality: 85,
      ),
    ]);
  });

  test('backing out of the chooser returns null', () async {
    picker.answer(null);

    expect(await source.pick(), isNull);
  });

  test('every call opens the chooser again: nothing is kept', () async {
    picker
      ..answer(XFile.fromData(_photo))
      ..answer(null)
      ..answer(XFile.fromData(Uint8List.fromList([9, 9])));

    expect(await source.pick(), _photo);
    expect(await source.pick(), isNull);
    expect(await source.pick(), [9, 9]);
    expect(picker.picks, hasLength(3));
  });

  test('no rule of the app is applied to the photo', () async {
    // Not an image, and empty: the server is what refuses them.
    picker
      ..answer(XFile.fromData(Uint8List.fromList('not an image'.codeUnits)))
      ..answer(XFile.fromData(Uint8List(0)));

    expect(await source.pick(), 'not an image'.codeUnits);
    expect(await source.pick(), isEmpty);
  });

  group('a photo that can not be used', () {
    const marker = 'IMG_20261006_secret.jpg';

    Future<Object> failure() async {
      try {
        await source.pick();
      } on Object catch (e) {
        return e;
      }
      return fail('expected a failure');
    }

    test('an unreadable file', () async {
      picker.answer(XFile('/no/such/dir/$marker'));

      final error = await failure();

      expect(error, isA<PhotoSourceException>());
      expect('$error', isNot(contains(marker)));
    });

    test('a platform failure, with none of its text kept', () async {
      picker.fail(
        PlatformException(code: 'invalid_image', message: 'bad $marker'),
      );

      final error = await failure();

      expect(error, isA<PhotoSourceException>());
      expect('$error', 'PhotoSourceException');
    });

    test('a second chooser while one is open', () async {
      picker.fail(PlatformException(code: 'already_active'));

      expect(await failure(), isA<PhotoSourceException>());
    });

    test('an Error from the plugin', () async {
      picker.fail(StateError('plugin not registered: $marker'));

      final error = await failure();

      expect(error, isA<PhotoSourceException>());
      expect('$error', isNot(contains(marker)));
    });

    test('the next choice works after a failure', () async {
      picker
        ..fail(PlatformException(code: 'x'))
        ..answer(XFile.fromData(_photo));

      await failure();
      expect(await source.pick(), _photo);
    });
  });
}

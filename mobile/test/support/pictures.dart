import 'dart:convert';
import 'dart:typed_data';

/// A real picture, as the avatar routes answer: a 16 by 16 JPEG, small
/// enough to decode in a widget test.
final Uint8List testPicture = base64Decode(
  '/9j/2wCEAA0JCgsKCA0LCgsODg0PEyAVExISEyccHhcgLikxMC4pLSwzOko+MzZGNywt'
  'QFdBRkxOUlNSMj5aYVpQYEpRUk8BDg4OExETJhUVJk81LTVPT09PT09PT09PT09PT09P'
  'T09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT09PT//AABEIABAAEAMBIgACEQED'
  'EQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAAB'
  'fQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkq'
  'NDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SV'
  'lpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq'
  '8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUE'
  'BAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZ'
  'GiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeI'
  'iYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl'
  '5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AMiKOrkUdEUdXIo6zqVCsLhT/9k=',
);

/// Bytes no image decoder accepts, although they start like a JPEG.
final Uint8List brokenPicture = Uint8List.fromList([
  0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, //
]);

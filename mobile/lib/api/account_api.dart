import 'api_client.dart';
import 'api_exception.dart';
import 'api_paths.dart';

/// The public account actions: the only API calls screens may make
/// directly (decision 023).
///
/// None of them carries or returns a token: each posts an email (and, for
/// register, a password) and the backend always answers 202 accepted,
/// whether or not the account exists (005). Everything token-bearing goes
/// through `SessionManager`. Nothing is retried; failures are
/// [ApiException]s.
class AccountApi {
  AccountApi(this._client);

  final ApiClient _client;

  /// Bounds each call; above the server's 10 s request deadline (018).
  static const requestTimeout = Duration(seconds: 15);

  /// `POST /v1/auth/register` → 202 for new and existing addresses alike.
  Future<void> register({required String email, required String password}) =>
      _accepted(ApiPaths.register, {'email': email, 'password': password});

  /// `POST /v1/auth/resend-verification` → 202 whatever the address.
  Future<void> resendVerification({required String email}) =>
      _accepted(ApiPaths.resendVerification, {'email': email});

  /// `POST /v1/auth/forgot-password` → 202 whatever the address.
  Future<void> forgotPassword({required String email}) =>
      _accepted(ApiPaths.forgotPassword, {'email': email});

  Future<void> _accepted(String path, Map<String, Object?> body) async {
    final r = await _client.send(
      'POST',
      path,
      json: body,
      timeout: requestTimeout,
    );
    if (r.statusCode != 202) {
      throw ApiProtocolException(
        ProtocolFailure.unexpectedStatus,
        statusCode: r.statusCode,
      );
    }
    if (r.json case {'status': 'accepted'}) return;
    throw const ApiProtocolException(
      ProtocolFailure.malformedBody,
      statusCode: 202,
    );
  }
}

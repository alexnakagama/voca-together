/// Why a member reports another (`PUT /v1/me/reports/{id}`, decision 033):
/// the five reasons the backend accepts, in the order they are offered.
enum ReportReason {
  harassment('harassment'),
  inappropriateContent('inappropriate_content'),
  spam('spam'),
  impersonation('impersonation'),
  other('other');

  const ReportReason(this.wire);

  /// The reason's identifier in the API.
  final String wire;
}

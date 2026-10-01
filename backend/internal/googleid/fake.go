package googleid

import "context"

// Fake is a Verifier for tests: no network, no keys, no clock. A token in
// Tokens verifies to its Claims; any other token is rejected with
// ReasonBadSignature. If Err is set, every call returns it instead (e.g. an
// *UnavailableError). A cancelled context returns its error first, as the
// real verifier does. Fake never logs; its errors never contain the token.
type Fake struct {
	Tokens map[string]Claims
	Err    error
}

var _ Verifier = Fake{}

func (f Fake) Verify(ctx context.Context, raw string) (Claims, error) {
	if err := ctx.Err(); err != nil {
		return Claims{}, err
	}
	if f.Err != nil {
		return Claims{}, f.Err
	}
	c, ok := f.Tokens[raw]
	if !ok {
		return Claims{}, invalid(ReasonBadSignature)
	}
	return c, nil
}

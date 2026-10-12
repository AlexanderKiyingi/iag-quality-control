package esign

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alvor-technologies/iag-platform-go/authclient"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// fakeVerifier stands in for the JWKS verifier: it accepts any token and
// returns the claims it was given, so these tests exercise only the esign
// rules layered on top.
type fakeVerifier struct{ sub, email string }

func (f fakeVerifier) VerifyBearerToken(string) (uuid.UUID, *authclient.Claims, error) {
	return uuid.Nil, &authclient.Claims{Email: f.email, RegisteredClaims: jwt.RegisteredClaims{Subject: f.sub}}, nil
}

func token(t *testing.T, payload map[string]any) string {
	t.Helper()
	b, _ := json.Marshal(payload)
	return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
}

func TestVerify(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	v := fakeVerifier{sub: "user-1", email: "ana@iag"}
	good := token(t, map[string]any{"purpose": "esign", "auth_time": now.Add(-time.Minute).Unix(), "jti": "j1"})

	signer, err := Verify(v, good, "user-1", now)
	if err != nil || signer.Email != "ana@iag" || signer.JTI != "j1" {
		t.Fatalf("good token: %+v %v", signer, err)
	}
	cases := map[string]struct {
		tok, caller string
		want        error
	}{
		"no token":               {"", "user-1", ErrMissing},
		"an ordinary session":    {token(t, map[string]any{"jti": "j2", "auth_time": now.Unix()}), "user-1", ErrNotForSign},
		"password re-entered 6m": {token(t, map[string]any{"purpose": "esign", "auth_time": now.Add(-6 * time.Minute).Unix(), "jti": "j3"}), "user-1", ErrStale},
		"someone else's token":   {good, "user-2", ErrNotYours},
		"no jti":                 {token(t, map[string]any{"purpose": "esign", "auth_time": now.Unix()}), "user-1", ErrInvalid},
	}
	for name, tc := range cases {
		if _, err := Verify(v, tc.tok, tc.caller, now); !errors.Is(err, tc.want) {
			t.Errorf("%s: want %v, got %v", name, tc.want, err)
		}
	}
}

// Package esign checks the signature tokens iag-authentication issues after a
// person re-enters their password (POST /v1/users/me/signature-token).
//
// The RSA signature, issuer, audience and expiry are checked by the platform
// verifier QC already runs for access tokens. What makes a token a signature
// token is checked here: purpose "esign", a re-authentication no older than
// MaxAge, a jti to make it single-use, and that it belongs to the person
// making the request — a signature token is not transferable.
package esign

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alvor-technologies/iag-platform-go/authclient"
	"github.com/google/uuid"
)

// Header carries the signature token on a signing request, beside the
// ordinary Authorization header that carries the session.
const Header = "X-Signature-Token"

// MaxAge bounds how long ago the password may have been re-entered.
const MaxAge = 5 * time.Minute

var (
	ErrMissing    = errors.New("this action needs an electronic signature: re-enter your password to sign")
	ErrInvalid    = errors.New("the signature token is not valid")
	ErrStale      = errors.New("the signature has expired — re-enter your password and sign again")
	ErrNotYours   = errors.New("a signature token can only be used by the person it was issued to")
	ErrNotForSign = errors.New("that token is a session, not a signature")
)

// Verifier is the part of the platform verifier esign needs.
type Verifier interface {
	VerifyBearerToken(token string) (uuid.UUID, *authclient.Claims, error)
}

// Signer is who signed, as the token proves it.
type Signer struct {
	Subject  string
	Email    string
	Name     string
	AuthTime time.Time
	JTI      string
}

type extraClaims struct {
	Purpose  string `json:"purpose"`
	AuthTime int64  `json:"auth_time"`
	JTI      string `json:"jti"`
}

// Verify checks a signature token for the caller whose session subject is
// callerSub ("" skips the ownership check — only for service-less local use).
func Verify(v Verifier, token, callerSub string, now time.Time) (Signer, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Signer{}, ErrMissing
	}
	if v == nil {
		return Signer{}, fmt.Errorf("%w: no verifier configured", ErrInvalid)
	}
	_, claims, err := v.VerifyBearerToken(token)
	if err != nil || claims == nil {
		return Signer{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	extra, err := payload(token)
	if err != nil {
		return Signer{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if extra.Purpose != "esign" {
		return Signer{}, ErrNotForSign
	}
	if extra.JTI == "" || extra.AuthTime == 0 {
		return Signer{}, fmt.Errorf("%w: missing jti or auth_time", ErrInvalid)
	}
	authTime := time.Unix(extra.AuthTime, 0)
	if now.Sub(authTime) > MaxAge || authTime.After(now.Add(time.Minute)) {
		return Signer{}, ErrStale
	}
	if callerSub != "" && claims.Subject != callerSub {
		return Signer{}, ErrNotYours
	}
	if strings.TrimSpace(claims.Email) == "" {
		return Signer{}, fmt.Errorf("%w: token names no signer", ErrInvalid)
	}
	return Signer{
		Subject:  claims.Subject,
		Email:    strings.TrimSpace(claims.Email),
		Name:     strings.TrimSpace(claims.Name),
		AuthTime: authTime,
		JTI:      extra.JTI,
	}, nil
}

// payload decodes the claims segment of a token the verifier has already
// accepted. The platform Claims struct has no field for purpose or auth_time.
func payload(token string) (extraClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return extraClaims{}, errors.New("malformed token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return extraClaims{}, err
	}
	var out extraClaims
	if err := json.Unmarshal(raw, &out); err != nil {
		return extraClaims{}, err
	}
	return out, nil
}

package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Buyer links (migration 027): one certificate, one buyer, a URL that works
// without signing in until it expires or QA revokes it.

const (
	BuyerLinkDefaultDays = 30
	BuyerLinkMaxDays     = 365
)

var (
	// ErrLinkGone is a link that was revoked or has expired.
	ErrLinkGone = errors.New("link no longer valid")
	// ErrNotShareable is a certificate that cannot be shared: not active, not
	// signed as issued, or with no template to print it.
	ErrNotShareable = errors.New("certificate cannot be shared")
)

// BuyerLink is a link as QA sees it. Token is set only in the response that
// created it; the database keeps its hash.
type BuyerLink struct {
	BusinessID     string     `json:"business_id"`
	CoaNumber      string     `json:"coa_number"`
	CustomerRef    string     `json:"customer_ref"`
	RecipientName  string     `json:"recipient_name"`
	RecipientEmail string     `json:"recipient_email"`
	ExpiresAt      time.Time  `json:"expires_at"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	RevokedBy      string     `json:"revoked_by,omitempty"`
	RevokeReason   string     `json:"revoke_reason,omitempty"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	Views          int        `json:"views"`
	LastViewedAt   *time.Time `json:"last_viewed_at,omitempty"`
	Status         string     `json:"status"` // active / expired / revoked
	Token          string     `json:"token,omitempty"`
}

// BuyerLinkInput creates a link.
type BuyerLinkInput struct {
	CustomerRef    string `json:"customer_ref"`
	RecipientName  string `json:"recipient_name"`
	RecipientEmail string `json:"recipient_email"`
	ExpiresInDays  int    `json:"expires_in_days"`
	ChangeReason   string `json:"change_reason"`
	Actor          string `json:"-"`
}

// BuyerLinkView is one opening of a link.
type BuyerLinkView struct {
	ViewedAt  time.Time `json:"viewed_at"`
	What      string    `json:"what"`
	Network   string    `json:"network"`
	UserAgent string    `json:"user_agent"`
}

// HashLinkToken is what the database keeps of a token.
func HashLinkToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newLinkToken() (string, string, error) {
	tok := make([]byte, 32)
	id := make([]byte, 6)
	if _, err := rand.Read(tok); err != nil {
		return "", "", err
	}
	if _, err := rand.Read(id); err != nil {
		return "", "", err
	}
	return base64.RawURLEncoding.EncodeToString(tok), "bl-" + hex.EncodeToString(id), nil
}

// ClampLinkDays applies the default (30) and the ceiling (365).
func ClampLinkDays(days int) int {
	switch {
	case days <= 0:
		return BuyerLinkDefaultDays
	case days > BuyerLinkMaxDays:
		return BuyerLinkMaxDays
	}
	return days
}

// CoAShareable says whether a certificate may go to a buyer: active and
// signed as issued. (The template check is the handler's: it resolves one.)
func (s *Store) CoAShareable(ctx context.Context, coaNumber string) (CoA, error) {
	coa, err := s.GetCoAByNumber(ctx, coaNumber)
	if err != nil {
		return CoA{}, err
	}
	if coa.Status != "active" {
		return coa, fmt.Errorf("%w: the certificate is %s", ErrNotShareable, coa.Status)
	}
	var signed bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM qc_signatures
		WHERE entity = 'coa' AND business_id = $1 AND meaning = 'issued')`, coa.CoaNumber).Scan(&signed); err != nil {
		return coa, err
	}
	if !signed {
		return coa, fmt.Errorf("%w: the certificate has not been signed as issued", ErrNotShareable)
	}
	return coa, nil
}

// CreateBuyerLink makes a link and returns it with its token, the only time
// the token is available.
func (s *Store) CreateBuyerLink(ctx context.Context, coaNumber string, in BuyerLinkInput) (BuyerLink, error) {
	token, id, err := newLinkToken()
	if err != nil {
		return BuyerLink{}, err
	}
	name := strings.TrimSpace(in.RecipientName)
	email := strings.ToLower(strings.TrimSpace(in.RecipientEmail))
	if name == "" && email == "" {
		return BuyerLink{}, fmt.Errorf("%w: say who the link is for (name or email)", ErrBadInput)
	}
	if len(name) > 200 || len(email) > 254 || (email != "" && !strings.Contains(email, "@")) {
		return BuyerLink{}, fmt.Errorf("%w: recipient name or email is not valid", ErrBadInput)
	}
	days := ClampLinkDays(in.ExpiresInDays)
	_, err = s.pool.Exec(ctx, `
		INSERT INTO qc_buyer_links (business_id, token_hash, coa_number, customer_ref, recipient_name, recipient_email,
			expires_at, created_by, updated_by, change_reason)
		VALUES ($1,$2,$3,$4,$5,$6, NOW() + make_interval(days => $7::int), $8, $8, $9)`,
		id, HashLinkToken(token), coaNumber, strings.TrimSpace(in.CustomerRef), name, email, days,
		in.Actor, strings.TrimSpace(in.ChangeReason))
	if err != nil {
		return BuyerLink{}, fmt.Errorf("create buyer link: %w", err)
	}
	out, err := s.GetBuyerLink(ctx, id)
	out.Token = token
	return out, err
}

const buyerLinkSelect = `
	SELECT l.business_id, l.coa_number, l.customer_ref, l.recipient_name, l.recipient_email, l.expires_at,
		l.revoked_at, l.revoked_by, CASE WHEN l.revoked_at IS NULL THEN '' ELSE l.change_reason END,
		l.created_by,
		l.created_at,
		(SELECT COUNT(*) FROM qc_buyer_link_views v WHERE v.link_id = l.business_id),
		(SELECT MAX(v.viewed_at) FROM qc_buyer_link_views v WHERE v.link_id = l.business_id)
	FROM qc_buyer_links l`

func scanBuyerLink(row pgx.Row) (BuyerLink, error) {
	var l BuyerLink
	err := row.Scan(&l.BusinessID, &l.CoaNumber, &l.CustomerRef, &l.RecipientName, &l.RecipientEmail, &l.ExpiresAt,
		&l.RevokedAt, &l.RevokedBy, &l.RevokeReason, &l.CreatedBy, &l.CreatedAt, &l.Views, &l.LastViewedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BuyerLink{}, ErrNotFound
	}
	if err != nil {
		return BuyerLink{}, err
	}
	switch {
	case l.RevokedAt != nil:
		l.Status = "revoked"
	case !l.ExpiresAt.After(time.Now()):
		l.Status = "expired"
	default:
		l.Status = "active"
	}
	return l, nil
}

// GetBuyerLink reads one link by id.
func (s *Store) GetBuyerLink(ctx context.Context, id string) (BuyerLink, error) {
	return scanBuyerLink(s.pool.QueryRow(ctx, buyerLinkSelect+` WHERE l.business_id = $1`, strings.TrimSpace(id)))
}

// ListBuyerLinks lists a certificate's links, newest first.
func (s *Store) ListBuyerLinks(ctx context.Context, coaNumber string) ([]BuyerLink, error) {
	rows, err := s.pool.Query(ctx, buyerLinkSelect+` WHERE l.coa_number = $1 ORDER BY l.created_at DESC LIMIT 200`,
		strings.TrimSpace(coaNumber))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BuyerLink{}
	for rows.Next() {
		l, err := scanBuyerLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// RevokeBuyerLink ends a link now. Revoking twice is refused, so the first
// revocation's who and why stand.
func (s *Store) RevokeBuyerLink(ctx context.Context, id, actor, reason string) (BuyerLink, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return BuyerLink{}, ErrReasonRequired
	}
	tag, err := s.pool.Exec(ctx, `UPDATE qc_buyer_links
		SET revoked_at = NOW(), revoked_by = $2, updated_by = $2, change_reason = $3, updated_at = NOW()
		WHERE business_id = $1 AND revoked_at IS NULL`, strings.TrimSpace(id), actor, reason)
	if err != nil {
		return BuyerLink{}, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.GetBuyerLink(ctx, id); err != nil {
			return BuyerLink{}, err
		}
		return BuyerLink{}, fmt.Errorf("%w: the link is already revoked", ErrConflict)
	}
	return s.GetBuyerLink(ctx, id)
}

// ResolveBuyerLink finds the link a token opens. Unknown tokens are
// ErrNotFound; revoked or expired ones ErrLinkGone, with the link.
func (s *Store) ResolveBuyerLink(ctx context.Context, token string) (BuyerLink, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 128 {
		return BuyerLink{}, ErrNotFound
	}
	l, err := scanBuyerLink(s.pool.QueryRow(ctx, buyerLinkSelect+` WHERE l.token_hash = $1`, HashLinkToken(token)))
	if err != nil {
		return BuyerLink{}, err
	}
	if l.Status != "active" {
		return l, ErrLinkGone
	}
	return l, nil
}

// RecordBuyerLinkView logs one opening.
func (s *Store) RecordBuyerLinkView(ctx context.Context, id, what, network, userAgent string) error {
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO qc_buyer_link_views (link_id, what, network, user_agent) VALUES ($1,$2,$3,$4)`,
		id, what, network, userAgent)
	return err
}

// ListBuyerLinkViews lists a link's views, newest first.
func (s *Store) ListBuyerLinkViews(ctx context.Context, id string) ([]BuyerLinkView, error) {
	rows, err := s.pool.Query(ctx, `SELECT viewed_at, what, network, user_agent FROM qc_buyer_link_views
		WHERE link_id = $1 ORDER BY viewed_at DESC LIMIT 500`, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BuyerLinkView{}
	for rows.Next() {
		var v BuyerLinkView
		if err := rows.Scan(&v.ViewedAt, &v.What, &v.Network, &v.UserAgent); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"iag-quality-control/backend/internal/esign"
)

// ErrLocked is returned when a write targets a record that has been signed
// into a final state. A signed record is changed by a new record (a new
// decision, a re-opened NC raised afresh), never by editing what was signed.
var ErrLocked = errors.New("this record has been electronically signed and is locked")

// ErrReasonRequired is returned when a change to an existing quality record
// carries no reason (Part 11 §11.10(e): the audit trail records why).
var ErrReasonRequired = errors.New("a reason for the change is required")

// ErrSignatureState is returned when a record is not in a state the
// requested signature can apply to.
var ErrSignatureState = errors.New("the record cannot be signed with that meaning in its current state")

// Signature is one electronic signature (migration 019).
type Signature struct {
	ID          int64     `json:"id"`
	Entity      string    `json:"entity"`
	BusinessID  string    `json:"business_id"`
	Meaning     string    `json:"meaning"`
	Reason      string    `json:"reason"`
	SignerSub   string    `json:"signer_sub"`
	SignerEmail string    `json:"signer_email"`
	SignerName  string    `json:"signer_name"`
	AuthTime    time.Time `json:"auth_time"`
	SignedAt    time.Time `json:"signed_at"`
	RecordHash  string    `json:"record_hash"`
}

// signable describes a register that can be signed: its table, key column,
// the meanings it accepts, and for each meaning the state change the
// signature makes (empty: the signature attests the record as it stands).
type signable struct {
	table   string
	key     string
	meaning map[string]string // meaning → UPDATE SET clause ("" = no change)
	// locks are the meanings after which the record is frozen.
	locks map[string]bool
	// guard, when set, must hold for the record before signing (SQL boolean
	// over the row aliased t).
	guard map[string]string
}

var signables = map[string]signable{
	"release-decisions": {
		table: "qc_release_decisions", key: "business_id",
		meaning: map[string]string{"approved": "status = 'approved'"},
		locks:   map[string]bool{"approved": true},
		guard:   map[string]string{"approved": "t.decision <> '' AND t.status <> 'approved'"},
	},
	"non-conformances": {
		table: "qc_non_conformances", key: "business_id",
		meaning: map[string]string{"closed": "status = 'closed'"},
		locks:   map[string]bool{"closed": true},
		guard:   map[string]string{"closed": "t.status NOT IN ('closed', 'void')"},
	},
	"capas": {
		table: "qc_capas", key: "business_id",
		meaning: map[string]string{"closed": "status = 'closed', closed_at = COALESCE(closed_at, CURRENT_DATE)"},
		locks:   map[string]bool{"closed": true},
		guard:   map[string]string{"closed": "t.status <> 'closed'"},
	},
	"specifications": {
		table: "qc_specifications", key: "business_id",
		meaning: map[string]string{"approved": ""},
	},
	"coa": {
		table: "qc_coa", key: "coa_number",
		meaning: map[string]string{"issued": ""},
	},
	// Each certification stage is approved or rejected by a signature; the
	// stage transition itself stays in ApproveCertificationStage.
	"certification-requests": {
		table: "qc_certification_requests", key: "business_id",
		meaning: map[string]string{"approved": "", "rejected": ""},
	},
}

// SignatureTokenUsed reports whether a signature token has already signed
// something. Checked before a write the signature belongs to, so a replayed
// token is refused before the record changes rather than after.
func (s *Store) SignatureTokenUsed(ctx context.Context, jti string) (bool, error) {
	var used bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM qc_signatures WHERE token_jti = $1)`, jti).Scan(&used)
	return used, err
}

// CertificationRequestForCoA names the certification request that issued a
// CoA, or "" when it was issued directly.
func (s *Store) CertificationRequestForCoA(ctx context.Context, coaNumber string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT business_id FROM qc_certification_requests
		WHERE coa_number = $1 ORDER BY id DESC LIMIT 1`, coaNumber).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// SignInput is a request to sign one record.
type SignInput struct {
	Entity     string
	BusinessID string
	Meaning    string
	Reason     string
	Signer     esign.Signer
}

/*
SignRecord records an electronic signature, applying the state change it
stands for, in one transaction.

The row is locked first, so the state checked is the state signed. The record
hash is taken after the change — it is what the signature attests — over the
row's canonical jsonb text, so any later edit to the record no longer matches
it. A signature token's jti is unique in qc_signatures: a replayed token is a
conflict, not a second signature.
*/
func (s *Store) SignRecord(ctx context.Context, in SignInput) (Signature, error) {
	def, ok := signables[strings.TrimSpace(in.Entity)]
	if !ok {
		return Signature{}, fmt.Errorf("%w: %q cannot be signed", ErrBadInput, in.Entity)
	}
	meaning := strings.TrimSpace(in.Meaning)
	set, ok := def.meaning[meaning]
	if !ok {
		return Signature{}, fmt.Errorf("%w: %s cannot be signed as %q", ErrBadInput, in.Entity, meaning)
	}
	id := strings.TrimSpace(in.BusinessID)
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return Signature{}, fmt.Errorf("%w: a signature states its reason", ErrBadInput)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Signature{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	guard := "true"
	if g := def.guard[meaning]; g != "" {
		guard = g
	}
	var ok2 bool
	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT %s FROM %s t WHERE t.%s = $1 FOR UPDATE`, guard, def.table, def.key), id).Scan(&ok2)
	if errors.Is(err, pgx.ErrNoRows) {
		return Signature{}, ErrNotFound
	}
	if err != nil {
		return Signature{}, err
	}
	if locked, err := lockedTx(ctx, tx, in.Entity, id); err != nil {
		return Signature{}, err
	} else if locked {
		return Signature{}, ErrLocked
	}
	if !ok2 {
		return Signature{}, ErrSignatureState
	}
	if set != "" {
		// updated_by and change_reason make the change log name the signer and
		// the signature's reason for the state change.
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s, updated_by = $2, change_reason = $3 WHERE %s = $1`,
			def.table, set, def.key), id, in.Signer.Email, reason); err != nil {
			return Signature{}, err
		}
	}
	var rowText string
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT to_jsonb(t)::text FROM %s t WHERE t.%s = $1`, def.table, def.key), id).Scan(&rowText); err != nil {
		return Signature{}, err
	}
	sum := sha256.Sum256([]byte(rowText))
	var out Signature
	err = tx.QueryRow(ctx, `
		INSERT INTO qc_signatures (entity, business_id, meaning, reason, signer_sub, signer_email,
		                           signer_name, auth_time, record_hash, token_jti)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+signatureCols,
		in.Entity, id, meaning, reason, in.Signer.Subject, in.Signer.Email, in.Signer.Name,
		in.Signer.AuthTime, hex.EncodeToString(sum[:]), in.Signer.JTI,
	).Scan(signatureTargets(&out)...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Signature{}, fmt.Errorf("%w: that signature token has already been used — sign again", ErrConflict)
		}
		return Signature{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Signature{}, err
	}
	return out, nil
}

const signatureCols = `id, entity, business_id, meaning, reason, signer_sub, signer_email, signer_name,
	auth_time, signed_at, record_hash`

func signatureTargets(sig *Signature) []any {
	return []any{&sig.ID, &sig.Entity, &sig.BusinessID, &sig.Meaning, &sig.Reason, &sig.SignerSub,
		&sig.SignerEmail, &sig.SignerName, &sig.AuthTime, &sig.SignedAt, &sig.RecordHash}
}

// ListSignatures lists a record's signatures, or every signature on an entity
// when businessID is empty, oldest first.
func (s *Store) ListSignatures(ctx context.Context, entity, businessID string, limit int) ([]Signature, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+signatureCols+` FROM qc_signatures
		WHERE ($1 = '' OR entity = $1) AND ($2 = '' OR business_id = $2)
		ORDER BY signed_at ASC, id ASC
		LIMIT $3`, strings.TrimSpace(entity), strings.TrimSpace(businessID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Signature{}
	for rows.Next() {
		var sig Signature
		if err := rows.Scan(signatureTargets(&sig)...); err != nil {
			return nil, err
		}
		out = append(out, sig)
	}
	return out, rows.Err()
}

// VerifySignature reports whether the record still matches what was signed.
func (s *Store) VerifySignature(ctx context.Context, sig Signature) (bool, error) {
	def, ok := signables[sig.Entity]
	if !ok {
		return false, ErrBadInput
	}
	var rowText string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT to_jsonb(t)::text FROM %s t WHERE t.%s = $1`, def.table, def.key), sig.BusinessID).Scan(&rowText)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256([]byte(rowText))
	return hex.EncodeToString(sum[:]) == sig.RecordHash, nil
}

// RecordLocked reports whether a record has been signed into a final state.
func (s *Store) RecordLocked(ctx context.Context, entity, businessID string) (bool, error) {
	def, ok := signables[entity]
	if !ok || len(def.locks) == 0 {
		return false, nil
	}
	meanings := make([]string, 0, len(def.locks))
	for m := range def.locks {
		meanings = append(meanings, m)
	}
	var locked bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM qc_signatures
		WHERE entity = $1 AND business_id = $2 AND meaning = ANY($3))`,
		entity, strings.TrimSpace(businessID), meanings).Scan(&locked)
	return locked, err
}

func lockedTx(ctx context.Context, tx pgx.Tx, entity, businessID string) (bool, error) {
	def := signables[entity]
	if len(def.locks) == 0 {
		return false, nil
	}
	meanings := make([]string, 0, len(def.locks))
	for m := range def.locks {
		meanings = append(meanings, m)
	}
	var locked bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM qc_signatures
		WHERE entity = $1 AND business_id = $2 AND meaning = ANY($3))`,
		entity, businessID, meanings).Scan(&locked)
	return locked, err
}

/*
guardWrite is the check every register write makes before touching an
existing record: refuse it when the record is signed and locked, and refuse a
change with no reason. A brand-new record needs no reason — creating it is
its own explanation.
*/
func (s *Store) guardWrite(ctx context.Context, entity, table, key, id, reason string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE %s = $1)`, table, key), id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if locked, err := s.RecordLocked(ctx, entity, id); err != nil {
		return err
	} else if locked {
		return ErrLocked
	}
	if strings.TrimSpace(reason) == "" {
		return ErrReasonRequired
	}
	return nil
}

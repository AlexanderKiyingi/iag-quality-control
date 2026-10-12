package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ControlledDocument is a document under control (migration 025).
type ControlledDocument struct {
	BusinessID         string            `json:"business_id"`
	Title              string            `json:"title"`
	DocType            string            `json:"doc_type"`
	Owner              string            `json:"owner"`
	Reviewers          []string          `json:"reviewers"`
	ReviewPeriodMonths int               `json:"review_period_months"`
	Obsolete           bool              `json:"obsolete"`
	UpdatedBy          string            `json:"updated_by"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	Effective          *DocumentVersion  `json:"effective,omitempty"`
	Latest             *DocumentVersion  `json:"latest,omitempty"`
	ReviewDue          bool              `json:"review_due"`
	Versions           []DocumentVersion `json:"versions,omitempty"`
}

// DocumentVersion is one version of a controlled document.
type DocumentVersion struct {
	DocBusinessID    string     `json:"doc_business_id"`
	Version          int        `json:"version"`
	BusinessID       string     `json:"business_id"`
	Status           string     `json:"status"`
	ChangeSummary    string     `json:"change_summary"`
	AttachmentID     string     `json:"attachment_id"`
	Filename         string     `json:"filename"`
	SubmittedBy      string     `json:"submitted_by"`
	SubmittedAt      *time.Time `json:"submitted_at,omitempty"`
	EffectiveDate    *string    `json:"effective_date,omitempty"`
	NextReviewDate   *string    `json:"next_review_date,omitempty"`
	RetiredAt        *time.Time `json:"retired_at,omitempty"`
	Acknowledgements int        `json:"acknowledgements"`
	UpdatedBy        string     `json:"updated_by"`
	CreatedAt        time.Time  `json:"created_at"`
}

// DocumentInput creates a document (with its first draft) or edits one.
type DocumentInput struct {
	BusinessID         string   `json:"business_id"`
	Title              *string  `json:"title"`
	DocType            *string  `json:"doc_type"`
	Owner              *string  `json:"owner"`
	Reviewers          []string `json:"reviewers"`
	ReviewPeriodMonths *int     `json:"review_period_months"`
	ChangeSummary      string   `json:"change_summary"`
	AttachmentID       string   `json:"attachment_id"`
	Filename           string   `json:"filename"`
	ChangeReason       string   `json:"change_reason"`
	Actor              string   `json:"-"`
}

// ErrSOPNotEffective refuses a result recorded by a method whose SOP is not
// in force.
var ErrSOPNotEffective = errors.New("this method's SOP has no effective version in date")

const docCols = `d.business_id, d.title, d.doc_type, d.owner, d.reviewers, d.review_period_months, d.obsolete,
	d.updated_by, d.created_at, d.updated_at`

const versionCols = `v.doc_business_id, v.version, v.business_id, v.status, v.change_summary, v.attachment_id,
	v.filename, v.submitted_by, v.submitted_at, v.effective_date::text, v.next_review_date::text, v.retired_at,
	(SELECT COUNT(*) FROM qc_document_acknowledgements a WHERE a.doc_business_id = v.doc_business_id AND a.version = v.version)::int,
	v.updated_by, v.created_at`

func scanDoc(row interface{ Scan(...any) error }) (ControlledDocument, error) {
	var d ControlledDocument
	var reviewers []byte
	if err := row.Scan(&d.BusinessID, &d.Title, &d.DocType, &d.Owner, &reviewers, &d.ReviewPeriodMonths, &d.Obsolete,
		&d.UpdatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return ControlledDocument{}, err
	}
	d.Reviewers = []string{}
	_ = json.Unmarshal(reviewers, &d.Reviewers)
	return d, nil
}

func scanVersion(row interface{ Scan(...any) error }) (DocumentVersion, error) {
	var v DocumentVersion
	err := row.Scan(&v.DocBusinessID, &v.Version, &v.BusinessID, &v.Status, &v.ChangeSummary, &v.AttachmentID,
		&v.Filename, &v.SubmittedBy, &v.SubmittedAt, &v.EffectiveDate, &v.NextReviewDate, &v.RetiredAt,
		&v.Acknowledgements, &v.UpdatedBy, &v.CreatedAt)
	return v, err
}

func (s *Store) documentVersions(ctx context.Context, doc string) ([]DocumentVersion, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+versionCols+` FROM qc_document_versions v
		WHERE v.doc_business_id = $1 ORDER BY v.version DESC`, doc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocumentVersion{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func fillDocument(d *ControlledDocument, versions []DocumentVersion) {
	today := time.Now().UTC().Format("2006-01-02")
	for i := range versions {
		v := &versions[i]
		if d.Latest == nil {
			d.Latest = v
		}
		if v.Status == "effective" {
			d.Effective = v
			if v.NextReviewDate != nil && *v.NextReviewDate <= today {
				d.ReviewDue = true
			}
		}
	}
}

// CreateDocument registers a document and its first draft in one go.
func (s *Store) CreateDocument(ctx context.Context, in DocumentInput) (ControlledDocument, error) {
	id := strings.ToUpper(strings.TrimSpace(in.BusinessID))
	if id == "" || in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		return ControlledDocument{}, fmt.Errorf("%w: a document has a number and a title", ErrBadInput)
	}
	docType, owner, period := "sop", strings.TrimSpace(in.Actor), 24
	if in.DocType != nil && strings.TrimSpace(*in.DocType) != "" {
		docType = strings.TrimSpace(*in.DocType)
	}
	if in.Owner != nil && strings.TrimSpace(*in.Owner) != "" {
		owner = strings.TrimSpace(*in.Owner)
	}
	if in.ReviewPeriodMonths != nil {
		period = *in.ReviewPeriodMonths
	}
	reviewers, _ := json.Marshal(cleanList(in.Reviewers))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ControlledDocument{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO qc_controlled_documents (business_id, title, doc_type, owner, reviewers, review_period_months, updated_by)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7)`, id, strings.TrimSpace(*in.Title), docType, owner, reviewers, period,
		strings.TrimSpace(in.Actor)); err != nil {
		return ControlledDocument{}, docErr(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO qc_document_versions (doc_business_id, version, change_summary, attachment_id, filename, updated_by)
		VALUES ($1, 1, $2, $3, $4, $5)`, id, firstNonEmpty(in.ChangeSummary, "First issue"),
		strings.TrimSpace(in.AttachmentID), strings.TrimSpace(in.Filename), strings.TrimSpace(in.Actor)); err != nil {
		return ControlledDocument{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ControlledDocument{}, err
	}
	return s.GetDocument(ctx, id)
}

func docErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "duplicate key"):
		return fmt.Errorf("%w: that document number is taken", ErrConflict)
	case strings.Contains(msg, "doc_type"):
		return fmt.Errorf("%w: type is sop, method, form, spec_sheet, policy or work_instruction", ErrBadInput)
	case strings.Contains(msg, "review_period_months"):
		return fmt.Errorf("%w: the review period is 1 – 120 months", ErrBadInput)
	}
	return err
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func cleanList(in []string) []string {
	out := []string{}
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (s *Store) GetDocument(ctx context.Context, id string) (ControlledDocument, error) {
	d, err := scanDoc(s.pool.QueryRow(ctx, `SELECT `+docCols+` FROM qc_controlled_documents d WHERE d.business_id = $1`,
		strings.ToUpper(strings.TrimSpace(id))))
	if errors.Is(err, pgx.ErrNoRows) {
		return ControlledDocument{}, ErrNotFound
	}
	if err != nil {
		return ControlledDocument{}, err
	}
	versions, err := s.documentVersions(ctx, d.BusinessID)
	if err != nil {
		return ControlledDocument{}, err
	}
	d.Versions = versions
	fillDocument(&d, d.Versions)
	return d, nil
}

// ListDocuments lists documents with their effective and latest versions.
// filter: "" all, "effective", "in_review", "review_due", "obsolete".
func (s *Store) ListDocuments(ctx context.Context, docType, filter string) ([]ControlledDocument, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+docCols+` FROM qc_controlled_documents d
		WHERE ($1 = '' OR d.doc_type = $1) ORDER BY d.business_id`, strings.TrimSpace(docType))
	if err != nil {
		return nil, err
	}
	var docs []ControlledDocument
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		docs = append(docs, d)
	}
	rows.Close()
	out := []ControlledDocument{}
	for _, d := range docs {
		versions, err := s.documentVersions(ctx, d.BusinessID)
		if err != nil {
			return nil, err
		}
		fillDocument(&d, versions)
		keep := true
		switch filter {
		case "effective":
			keep = d.Effective != nil
		case "in_review":
			keep = d.Latest != nil && d.Latest.Status == "in_review"
		case "review_due":
			keep = d.ReviewDue
		case "obsolete":
			keep = d.Obsolete
		}
		if keep {
			out = append(out, d)
		}
	}
	return out, nil
}

// UpdateDocument edits a document's header; every edit says why.
func (s *Store) UpdateDocument(ctx context.Context, id string, in DocumentInput) (ControlledDocument, error) {
	id = strings.ToUpper(strings.TrimSpace(id))
	if strings.TrimSpace(in.ChangeReason) == "" {
		return ControlledDocument{}, ErrReasonRequired
	}
	var reviewers []byte
	if in.Reviewers != nil {
		reviewers, _ = json.Marshal(cleanList(in.Reviewers))
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE qc_controlled_documents SET
			title = COALESCE(NULLIF($2, ''), title), doc_type = COALESCE(NULLIF($3, ''), doc_type),
			owner = COALESCE($4, owner), reviewers = COALESCE($5::jsonb, reviewers),
			review_period_months = COALESCE($6, review_period_months),
			updated_by = $7, change_reason = $8, updated_at = NOW()
		WHERE business_id = $1`, id, val(in.Title), val(in.DocType), trimOptional(in.Owner), reviewers,
		in.ReviewPeriodMonths, strings.TrimSpace(in.Actor), strings.TrimSpace(in.ChangeReason))
	if err != nil {
		return ControlledDocument{}, docErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ControlledDocument{}, ErrNotFound
	}
	return s.GetDocument(ctx, id)
}

func val(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

/*
NewDocumentVersion opens the next draft. One revision at a time: while a
draft, a version in review or an approved-but-not-yet-effective version
exists, that is the one to work on.
*/
func (s *Store) NewDocumentVersion(ctx context.Context, doc string, in DocumentInput) (DocumentVersion, error) {
	doc = strings.ToUpper(strings.TrimSpace(doc))
	d, err := s.GetDocument(ctx, doc)
	if err != nil {
		return DocumentVersion{}, err
	}
	if d.Obsolete {
		return DocumentVersion{}, fmt.Errorf("%w: the document is obsolete", ErrConflict)
	}
	for _, v := range d.Versions {
		if v.Status == "draft" || v.Status == "in_review" || v.Status == "approved" {
			return DocumentVersion{}, fmt.Errorf("%w: version %d is still %s", ErrConflict, v.Version, v.Status)
		}
	}
	if strings.TrimSpace(in.ChangeSummary) == "" {
		return DocumentVersion{}, fmt.Errorf("%w: say what changes in this version", ErrBadInput)
	}
	next := 1
	if len(d.Versions) > 0 {
		next = d.Versions[0].Version + 1
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO qc_document_versions (doc_business_id, version, change_summary, attachment_id, filename, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6)`, doc, next, strings.TrimSpace(in.ChangeSummary),
		strings.TrimSpace(in.AttachmentID), strings.TrimSpace(in.Filename), strings.TrimSpace(in.Actor)); err != nil {
		return DocumentVersion{}, err
	}
	return s.getVersion(ctx, doc, next)
}

func (s *Store) getVersion(ctx context.Context, doc string, version int) (DocumentVersion, error) {
	v, err := scanVersion(s.pool.QueryRow(ctx, `SELECT `+versionCols+` FROM qc_document_versions v
		WHERE v.doc_business_id = $1 AND v.version = $2`, strings.ToUpper(strings.TrimSpace(doc)), version))
	if errors.Is(err, pgx.ErrNoRows) {
		return DocumentVersion{}, ErrNotFound
	}
	return v, err
}

// UpdateDraft changes a draft's file or summary; nothing past draft is edited.
func (s *Store) UpdateDraft(ctx context.Context, doc string, version int, in DocumentInput) (DocumentVersion, error) {
	v, err := s.getVersion(ctx, doc, version)
	if err != nil {
		return DocumentVersion{}, err
	}
	if v.Status != "draft" {
		return DocumentVersion{}, fmt.Errorf("%w: version %d is %s; only a draft is edited", ErrConflict, version, v.Status)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE qc_document_versions SET
			change_summary = COALESCE(NULLIF($3, ''), change_summary),
			attachment_id = COALESCE(NULLIF($4, ''), attachment_id),
			filename = COALESCE(NULLIF($5, ''), filename),
			updated_by = $6, updated_at = NOW()
		WHERE doc_business_id = $1 AND version = $2`, v.DocBusinessID, version, strings.TrimSpace(in.ChangeSummary),
		strings.TrimSpace(in.AttachmentID), strings.TrimSpace(in.Filename), strings.TrimSpace(in.Actor)); err != nil {
		return DocumentVersion{}, err
	}
	return s.getVersion(ctx, doc, version)
}

// SubmitForReview sends a draft for approval; it must carry its file.
func (s *Store) SubmitForReview(ctx context.Context, doc string, version int, actor string) (DocumentVersion, error) {
	v, err := s.getVersion(ctx, doc, version)
	if err != nil {
		return DocumentVersion{}, err
	}
	if v.Status != "draft" {
		return DocumentVersion{}, fmt.Errorf("%w: version %d is %s, not a draft", ErrConflict, version, v.Status)
	}
	if v.AttachmentID == "" {
		return DocumentVersion{}, fmt.Errorf("%w: attach the document before sending it for review", ErrBadInput)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE qc_document_versions SET status = 'in_review', submitted_by = $3, submitted_at = NOW(),
			updated_by = $3, updated_at = NOW()
		WHERE doc_business_id = $1 AND version = $2`, v.DocBusinessID, version, strings.TrimSpace(actor)); err != nil {
		return DocumentVersion{}, err
	}
	return s.getVersion(ctx, doc, version)
}

/*
MakeEffective puts an approved version in force on a date (today when
empty), superseding the one before it in the same transaction — so a document
is never without, or with two, effective versions — and sets the next review
date from the document's review period.
*/
func (s *Store) MakeEffective(ctx context.Context, doc string, version int, effectiveDate, actor string) (DocumentVersion, error) {
	doc = strings.ToUpper(strings.TrimSpace(doc))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DocumentVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	var period int
	err = tx.QueryRow(ctx, `
		SELECT v.status, d.review_period_months FROM qc_document_versions v
		JOIN qc_controlled_documents d ON d.business_id = v.doc_business_id
		WHERE v.doc_business_id = $1 AND v.version = $2 FOR UPDATE OF v`, doc, version).Scan(&status, &period)
	if errors.Is(err, pgx.ErrNoRows) {
		return DocumentVersion{}, ErrNotFound
	}
	if err != nil {
		return DocumentVersion{}, err
	}
	if status != "approved" {
		return DocumentVersion{}, fmt.Errorf("%w: version %d is %s; it is made effective once approved", ErrConflict, version, status)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE qc_document_versions SET status = 'superseded', retired_at = NOW(), updated_by = $2, updated_at = NOW()
		WHERE doc_business_id = $1 AND status = 'effective'`, doc, strings.TrimSpace(actor)); err != nil {
		return DocumentVersion{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE qc_document_versions SET status = 'effective',
			effective_date = COALESCE(NULLIF($3, '')::date, CURRENT_DATE),
			next_review_date = (COALESCE(NULLIF($3, '')::date, CURRENT_DATE) + make_interval(months => $4))::date,
			updated_by = $5, updated_at = NOW()
		WHERE doc_business_id = $1 AND version = $2`, doc, version, strings.TrimSpace(effectiveDate), period,
		strings.TrimSpace(actor)); err != nil {
		return DocumentVersion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DocumentVersion{}, err
	}
	return s.getVersion(ctx, doc, version)
}

// ObsoleteDocument withdraws a document: its effective version is retired and
// nothing can be revised again. Every withdrawal says why.
func (s *Store) ObsoleteDocument(ctx context.Context, doc, reason, actor string) (ControlledDocument, error) {
	doc = strings.ToUpper(strings.TrimSpace(doc))
	if strings.TrimSpace(reason) == "" {
		return ControlledDocument{}, ErrReasonRequired
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ControlledDocument{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE qc_controlled_documents SET obsolete = true, updated_by = $2, change_reason = $3, updated_at = NOW()
		WHERE business_id = $1 AND NOT obsolete`, doc, strings.TrimSpace(actor), strings.TrimSpace(reason))
	if err != nil {
		return ControlledDocument{}, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.GetDocument(ctx, doc); err != nil {
			return ControlledDocument{}, err
		}
		return ControlledDocument{}, fmt.Errorf("%w: already obsolete", ErrConflict)
	}
	if _, err := tx.Exec(ctx, `UPDATE qc_document_versions SET status = 'obsolete', retired_at = NOW(), updated_by = $2,
		change_reason = $3, updated_at = NOW()
		WHERE doc_business_id = $1 AND status IN ('effective', 'draft', 'in_review', 'approved')`,
		doc, strings.TrimSpace(actor), strings.TrimSpace(reason)); err != nil {
		return ControlledDocument{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ControlledDocument{}, err
	}
	return s.GetDocument(ctx, doc)
}

// Acknowledgement is one person's "read and understood".
type Acknowledgement struct {
	DocBusinessID  string    `json:"doc_business_id"`
	Version        int       `json:"version"`
	UserSub        string    `json:"user_sub"`
	UserEmail      string    `json:"user_email"`
	UserName       string    `json:"user_name"`
	AcknowledgedAt time.Time `json:"acknowledged_at"`
}

// Acknowledge records that someone read and understood the effective version.
// Acknowledging twice is harmless.
func (s *Store) Acknowledge(ctx context.Context, doc string, version int, sub, email, name string) (Acknowledgement, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return Acknowledgement{}, fmt.Errorf("%w: an acknowledgement names who read it", ErrBadInput)
	}
	v, err := s.getVersion(ctx, doc, version)
	if err != nil {
		return Acknowledgement{}, err
	}
	if v.Status != "effective" {
		return Acknowledgement{}, fmt.Errorf("%w: only the effective version is acknowledged", ErrConflict)
	}
	var a Acknowledgement
	err = s.pool.QueryRow(ctx, `
		INSERT INTO qc_document_acknowledgements (doc_business_id, version, user_sub, user_email, user_name)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (doc_business_id, version, user_email) DO UPDATE SET user_name = EXCLUDED.user_name
		RETURNING doc_business_id, version, user_sub, user_email, user_name, acknowledged_at`,
		v.DocBusinessID, version, strings.TrimSpace(sub), email, strings.TrimSpace(name)).
		Scan(&a.DocBusinessID, &a.Version, &a.UserSub, &a.UserEmail, &a.UserName, &a.AcknowledgedAt)
	return a, err
}

// ListAcknowledgements lists acknowledgements of a document, or by a person
// (for training records), newest first.
func (s *Store) ListAcknowledgements(ctx context.Context, doc, email string) ([]Acknowledgement, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT doc_business_id, version, user_sub, user_email, user_name, acknowledged_at
		FROM qc_document_acknowledgements
		WHERE ($1 = '' OR doc_business_id = $1) AND ($2 = '' OR user_email = $2)
		ORDER BY acknowledged_at DESC LIMIT 1000`, strings.ToUpper(strings.TrimSpace(doc)), strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Acknowledgement{}
	for rows.Next() {
		var a Acknowledgement
		if err := rows.Scan(&a.DocBusinessID, &a.Version, &a.UserSub, &a.UserEmail, &a.UserName, &a.AcknowledgedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

/*
checkMethodSOP refuses a result recorded by a method whose SOP is not in
force: no effective version, or one past its review date. A method with no
SOP named, or an id that is not a registered method, is not checked — the
methods register predates document control.
*/
func (s *Store) checkMethodSOP(ctx context.Context, methodID string) error {
	methodID = strings.TrimSpace(methodID)
	if methodID == "" {
		return nil
	}
	var sop string
	err := s.pool.QueryRow(ctx, `SELECT sop_ref FROM qc_lab_methods WHERE business_id = $1`, methodID).Scan(&sop)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && strings.TrimSpace(sop) == "") {
		return nil
	}
	if err != nil {
		return err
	}
	var inForce bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM qc_document_versions WHERE doc_business_id = $1 AND status = 'effective'
		  AND (next_review_date IS NULL OR next_review_date >= CURRENT_DATE))`, strings.ToUpper(strings.TrimSpace(sop))).Scan(&inForce); err != nil {
		return err
	}
	if !inForce {
		return fmt.Errorf("%w: method %s runs by %s, which has no effective version in its review date", ErrSOPNotEffective, methodID, sop)
	}
	return nil
}

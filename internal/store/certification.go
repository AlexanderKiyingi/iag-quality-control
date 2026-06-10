package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	CertStageQCReview    = "qc_review"
	CertStageOpsApproval = "ops_approval"
	CertStageCEOSignoff  = "ceo_signoff"
	CertStageCoAIssued   = "coa_issued"
	CertStageExportReady = "export_ready"

	certStageOrder = []string{
		CertStageQCReview,
		CertStageOpsApproval,
		CertStageCEOSignoff,
		CertStageCoAIssued,
		CertStageExportReady,
	}
)

type CertificationRequest struct {
	BusinessID      string     `json:"business_id"`
	BatchBusinessID string     `json:"batch_business_id"`
	LotBusinessID   string     `json:"lot_business_id"`
	CoaNumber       string     `json:"coa_number"`
	DocumentRef     string     `json:"document_ref"`
	Status          string     `json:"status"`
	Stage           string     `json:"stage"`
	RequestedBy     string     `json:"requested_by"`
	RequestedAt     time.Time  `json:"requested_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	Notes           string     `json:"notes"`
	Approvals       []CertificationApproval `json:"approvals,omitempty"`
}

type CertificationApproval struct {
	RequestBusinessID string     `json:"request_business_id"`
	Stage             string     `json:"stage"`
	Approver          string     `json:"approver"`
	Decision          string     `json:"decision"`
	Notes             string     `json:"notes"`
	DecidedAt         *time.Time `json:"decided_at,omitempty"`
}

type CreateCertificationInput struct {
	BatchBusinessID string
	LotBusinessID   string
	CoaNumber       string
	DocumentRef     string
	RequestedBy     string
	Notes           string
}

type ApproveCertificationInput struct {
	RequestBusinessID string
	Approver          string
	Decision          string
	Notes             string
}

func (s *Store) CreateCertificationRequest(ctx context.Context, in CreateCertificationInput) (CertificationRequest, error) {
	bid := strings.TrimSpace(in.BatchBusinessID)
	if bid == "" {
		return CertificationRequest{}, ErrBadInput
	}
	summary, err := s.GetBatchLabSummary(ctx, bid)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return CertificationRequest{}, fmt.Errorf("%w: batch lab summary required before certification", ErrBadInput)
		}
		return CertificationRequest{}, err
	}
	if summary.Moisture == nil || summary.CupScore == nil {
		return CertificationRequest{}, fmt.Errorf("%w: moisture and cup score required", ErrBadInput)
	}

	businessID, err := nextBusinessID(ctx, s.pool, "qc_certification_requests", "CERT")
	if err != nil {
		return CertificationRequest{}, err
	}

	var out CertificationRequest
	err = s.pool.QueryRow(ctx, `
		INSERT INTO qc_certification_requests (
			business_id, batch_business_id, lot_business_id, coa_number, document_ref,
			status, stage, requested_by, notes
		) VALUES ($1,$2,$3,$4,$5,'pending',$6,$7,$8)
		RETURNING business_id, batch_business_id, lot_business_id, coa_number, document_ref,
		          status, stage, requested_by, requested_at, completed_at, notes`,
		businessID, bid, strings.TrimSpace(in.LotBusinessID), strings.TrimSpace(in.CoaNumber),
		strings.TrimSpace(in.DocumentRef), CertStageQCReview, strings.TrimSpace(in.RequestedBy),
		strings.TrimSpace(in.Notes),
	).Scan(
		&out.BusinessID, &out.BatchBusinessID, &out.LotBusinessID, &out.CoaNumber, &out.DocumentRef,
		&out.Status, &out.Stage, &out.RequestedBy, &out.RequestedAt, &out.CompletedAt, &out.Notes,
	)
	if err != nil {
		return CertificationRequest{}, fmt.Errorf("create certification: %w", err)
	}
	return out, nil
}

func (s *Store) ListPendingCertifications(ctx context.Context, limit int) ([]CertificationRequest, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT business_id, batch_business_id, lot_business_id, coa_number, document_ref,
		       status, stage, requested_by, requested_at, completed_at, notes
		FROM qc_certification_requests
		WHERE status = 'pending'
		ORDER BY requested_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCertRequests(rows)
}

func (s *Store) GetCertificationRequest(ctx context.Context, businessID string) (CertificationRequest, error) {
	var out CertificationRequest
	err := s.pool.QueryRow(ctx, `
		SELECT business_id, batch_business_id, lot_business_id, coa_number, document_ref,
		       status, stage, requested_by, requested_at, completed_at, notes
		FROM qc_certification_requests WHERE business_id = $1`, businessID,
	).Scan(
		&out.BusinessID, &out.BatchBusinessID, &out.LotBusinessID, &out.CoaNumber, &out.DocumentRef,
		&out.Status, &out.Stage, &out.RequestedBy, &out.RequestedAt, &out.CompletedAt, &out.Notes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CertificationRequest{}, ErrNotFound
	}
	if err != nil {
		return CertificationRequest{}, err
	}
	approvals, err := s.listApprovals(ctx, businessID)
	if err != nil {
		return CertificationRequest{}, err
	}
	out.Approvals = approvals
	return out, nil
}

func (s *Store) ApproveCertificationStage(ctx context.Context, in ApproveCertificationInput) (CertificationRequest, CoA, bool, error) {
	req, err := s.GetCertificationRequest(ctx, in.RequestBusinessID)
	if err != nil {
		return CertificationRequest{}, CoA{}, false, err
	}
	if req.Status != "pending" {
		return CertificationRequest{}, CoA{}, false, ErrConflict
	}
	decision := strings.ToLower(strings.TrimSpace(in.Decision))
	if decision != "approved" && decision != "rejected" {
		return CertificationRequest{}, CoA{}, false, ErrBadInput
	}

	now := time.Now().UTC()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO qc_certification_approvals (request_business_id, stage, approver, decision, notes, decided_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		req.BusinessID, req.Stage, strings.TrimSpace(in.Approver), decision, strings.TrimSpace(in.Notes), now,
	)
	if err != nil {
		return CertificationRequest{}, CoA{}, false, err
	}

	if decision == "rejected" {
		_, err = s.pool.Exec(ctx, `
			UPDATE qc_certification_requests SET status = 'rejected', completed_at = $2 WHERE business_id = $1`,
			req.BusinessID, now)
		if err != nil {
			return CertificationRequest{}, CoA{}, false, err
		}
		updated, err := s.GetCertificationRequest(ctx, req.BusinessID)
		return updated, CoA{}, false, err
	}

	nextStage := nextCertStage(req.Stage)
	var coa CoA
	issuedCoA := false

	if req.Stage == CertStageCEOSignoff {
		if strings.TrimSpace(req.LotBusinessID) == "" || strings.TrimSpace(req.CoaNumber) == "" {
			return CertificationRequest{}, CoA{}, false, fmt.Errorf("%w: lot_business_id and coa_number required before CEO sign-off", ErrBadInput)
		}
		coa, err = s.CreateCoA(ctx, CreateCoAInput{
			LotBusinessID:   req.LotBusinessID,
			BatchBusinessID: req.BatchBusinessID,
			CoaNumber:       req.CoaNumber,
			DocumentRef:     req.DocumentRef,
			IssuedBy:        in.Approver,
		})
		if err != nil {
			return CertificationRequest{}, CoA{}, false, err
		}
		issuedCoA = true
	}

	status := "pending"
	var completedAt *time.Time
	if nextStage == CertStageExportReady {
		status = "approved"
		completedAt = &now
	}

	_, err = s.pool.Exec(ctx, `
		UPDATE qc_certification_requests
		SET stage = $2, status = $3, completed_at = COALESCE($4, completed_at)
		WHERE business_id = $1`,
		req.BusinessID, nextStage, status, completedAt,
	)
	if err != nil {
		return CertificationRequest{}, CoA{}, false, err
	}
	updated, err := s.GetCertificationRequest(ctx, req.BusinessID)
	return updated, coa, issuedCoA, err
}

func nextCertStage(current string) string {
	for i, st := range certStageOrder {
		if st == current && i+1 < len(certStageOrder) {
			return certStageOrder[i+1]
		}
	}
	return CertStageExportReady
}

func (s *Store) listApprovals(ctx context.Context, requestID string) ([]CertificationApproval, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT request_business_id, stage, approver, decision, notes, decided_at
		FROM qc_certification_approvals
		WHERE request_business_id = $1 ORDER BY decided_at ASC`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CertificationApproval
	for rows.Next() {
		var item CertificationApproval
		if err := rows.Scan(&item.RequestBusinessID, &item.Stage, &item.Approver, &item.Decision, &item.Notes, &item.DecidedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanCertRequests(rows pgx.Rows) ([]CertificationRequest, error) {
	defer rows.Close()
	var out []CertificationRequest
	for rows.Next() {
		var item CertificationRequest
		if err := rows.Scan(
			&item.BusinessID, &item.BatchBusinessID, &item.LotBusinessID, &item.CoaNumber, &item.DocumentRef,
			&item.Status, &item.Stage, &item.RequestedBy, &item.RequestedAt, &item.CompletedAt, &item.Notes,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

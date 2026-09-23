package store

import (
	"context"
	"errors"
	"testing"
)

/*
Every upsert must refuse an invalid request before it touches the pool.

These run against a Store with a nil pool, so anything that reaches SQL panics
rather than failing — which is the point. Validating first saves a round trip on
a request that was never going to succeed, and it is what makes the guards
testable at all, since there is no database in CI.

The COALESCE guards themselves cannot be proved here for the same reason. They
are verified by hand against a live service: POST the full object, POST again
with only the business id and one field, then GET and diff.
*/

func TestUpsertsValidateBeforeTouchingThePool(t *testing.T) {
	s := &Store{}
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"instrument without a name", func() error {
			_, err := s.UpsertInstrument(ctx, UpsertInstrumentInput{BusinessID: "INS-26-0001"})
			return err
		}},
		{"instrument with a whitespace name", func() error {
			_, err := s.UpsertInstrument(ctx, UpsertInstrumentInput{BusinessID: "INS-26-0001", Name: "   "})
			return err
		}},
		{"lab method without a name", func() error {
			_, err := s.UpsertLabMethod(ctx, UpsertLabMethodInput{BusinessID: "LM-26-0001"})
			return err
		}},
		{"lab request without a product", func() error {
			_, err := s.UpsertLabRequest(ctx, UpsertLabRequestInput{BusinessID: "LR-26-0001"})
			return err
		}},
		{"CAPA without a title", func() error {
			_, err := s.UpsertCAPA(ctx, UpsertCAPAInput{BusinessID: "CAPA-26-0001"})
			return err
		}},
		{"external audit without a type", func() error {
			_, err := s.UpsertExternalAudit(ctx, UpsertExternalAuditInput{
				BusinessID: "AUD-26-0001", AuditDate: "2026-09-23",
			})
			return err
		}},
		{"external audit without a date", func() error {
			_, err := s.UpsertExternalAudit(ctx, UpsertExternalAuditInput{
				BusinessID: "AUD-26-0001", AuditType: "surveillance",
			})
			return err
		}},
		{"technician without an id", func() error {
			_, err := s.UpsertTechnician(ctx, UpsertTechnicianInput{Name: "A. Tester"})
			return err
		}},
		{"technician without a name", func() error {
			_, err := s.UpsertTechnician(ctx, UpsertTechnicianInput{BusinessID: "TEC-1"})
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrBadInput) {
				t.Fatalf("got %v, want ErrBadInput", err)
			}
		})
	}
}

// A blank id is minted, which needs the pool — so these cases must reach SQL
// rather than being refused. Guarding against a well-meant "require an id too"
// that would break every create.
func TestUpsertsStillMintABlankBusinessID(t *testing.T) {
	s := &Store{}
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		call func()
	}{
		{"instrument", func() { _, _ = s.UpsertInstrument(ctx, UpsertInstrumentInput{Name: "Moisture meter"}) }},
		{"lab method", func() { _, _ = s.UpsertLabMethod(ctx, UpsertLabMethodInput{Name: "Moisture by oven"}) }},
		{"lab request", func() { _, _ = s.UpsertLabRequest(ctx, UpsertLabRequestInput{Product: "Arabica AA"}) }},
		{"CAPA", func() { _, _ = s.UpsertCAPA(ctx, UpsertCAPAInput{Title: "Re-train the intake team"}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected the nil pool to be reached (a blank id is minted); it was not")
				}
			}()
			tc.call()
		})
	}
}

func TestOptionalHelpers(t *testing.T) {
	if got := trimOptional(nil); got != nil {
		t.Fatalf("trimOptional(nil) = %v, want nil — absent must stay absent", got)
	}
	if got := trimOptional(ptr("  spaced  ")); got == nil || *got != "spaced" {
		t.Fatalf("trimOptional did not trim: %v", got)
	}
	if got := trimOptional(ptr("   ")); got == nil || *got != "" {
		t.Fatalf("trimOptional(blank) must stay non-nil so the column can be cleared: %v", got)
	}
	if got := blankToNil(ptr("   ")); got != nil {
		t.Fatalf("blankToNil(blank) = %v, want nil", got)
	}
	if got := blankToNil(ptr("open")); got == nil || *got != "open" {
		t.Fatalf("blankToNil dropped a real value: %v", got)
	}
}

func ptr(s string) *string { return &s }

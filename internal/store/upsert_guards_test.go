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

// Migration 011 resources, same contract.
func TestLabModuleWritesValidateBeforeTouchingThePool(t *testing.T) {
	s := &Store{}
	ctx := context.Background()

	t.Run("calibration without an instrument", func(t *testing.T) {
		_, err := s.CreateInstrumentCalibration(ctx, CreateCalibrationInput{BusinessID: "CAL-26-0001"})
		if !errors.Is(err, ErrBadInput) {
			t.Fatalf("got %v, want ErrBadInput", err)
		}
	})
	t.Run("calibration named only by instrument_name is accepted", func(t *testing.T) {
		// An instrument the register has never heard of is still a calibration
		// worth recording, so this must reach SQL rather than be refused.
		defer func() {
			if recover() == nil {
				t.Fatal("expected the nil pool to be reached; the input was refused instead")
			}
		}()
		_, _ = s.CreateInstrumentCalibration(ctx, CreateCalibrationInput{
			BusinessID: "CAL-26-0001", InstrumentName: "Borrowed moisture meter",
		})
	})
	t.Run("stability study without a product", func(t *testing.T) {
		_, err := s.UpsertStabilityStudy(ctx, UpsertStabilityStudyInput{BusinessID: "STB-26-0001"})
		if !errors.Is(err, ErrBadInput) {
			t.Fatalf("got %v, want ErrBadInput", err)
		}
	})
}

func TestCalibrationVocabulary(t *testing.T) {
	for in, want := range map[string]string{
		"Pass": "pass", "Adjust": "adjust", "Fail": "fail", "FAILED": "fail", "": "",
	} {
		if got := normalizeCalResult(in); got != want {
			t.Errorf("normalizeCalResult(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"Current": "current", "Due": "due", "Overdue": "overdue",
		"Out of service": "out_of_service", "out-of-service": "out_of_service", "": "",
	} {
		if got := normalizeCalStatus(in); got != want {
			t.Errorf("normalizeCalStatus(%q) = %q, want %q", in, got, want)
		}
	}
	// An unfamiliar word is kept, not guessed at — a wrong verdict on a
	// calibration certificate is worse than an unfamiliar one.
	if got := normalizeCalResult("Conditional"); got != "Conditional" {
		t.Errorf("normalizeCalResult dropped an unknown verdict: %q", got)
	}
}

// Migration 012 resources.
func TestQARegisterWritesValidateBeforeTouchingThePool(t *testing.T) {
	s := &Store{}
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"non-conformance without a title", func() error {
			_, err := s.UpsertNonConformance(ctx, UpsertNonConformanceInput{BusinessID: "NCR-26-0001"})
			return err
		}},
		{"in-process check without a parameter", func() error {
			_, err := s.UpsertInProcessCheck(ctx, UpsertInProcessCheckInput{BusinessID: "IPC-26-0001"})
			return err
		}},
		{"release decision without a batch", func() error {
			_, _, err := s.UpsertReleaseDecision(ctx, UpsertReleaseDecisionInput{BusinessID: "REL-26-0001"})
			return err
		}},
		{"hold event without a batch", func() error {
			_, err := s.CreateHoldEvent(ctx, CreateHoldEventInput{BusinessID: "HLD-26-0001", Event: "Released"})
			return err
		}},
		{"hold event without an event", func() error {
			_, err := s.CreateHoldEvent(ctx, CreateHoldEventInput{BusinessID: "HLD-26-0001", BatchRef: "BATCH-1"})
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

/*
The release-decision vocabulary decides what other services do.

iag-traceability gates QR publish on qc.* events and iag-warehouse consumes them,
so a decision mapped to the wrong event type moves real stock. An unrecognised
decision must map to no event — silence is recoverable, a batch wrongly announced
as released is not.
*/
func TestReleaseDecisionVocabularyAndEvents(t *testing.T) {
	for in, want := range map[string]string{
		"Released": "released", "release": "released",
		"Conditional release": "conditional", "conditional": "conditional",
		"Hold": "hold", "On hold": "hold",
		"Reject": "reject", "Rejected": "reject",
		"Rework": "rework",
		"":       "",
	} {
		if got := NormalizeReleaseDecision(in); got != want {
			t.Errorf("NormalizeReleaseDecision(%q) = %q, want %q", in, got, want)
		}
	}

	for in, want := range map[string]string{
		"Released":            "qc.batch.released",
		"Conditional release": "qc.batch.released",
		"Hold":                "qc.batch.held",
		"Reject":              "qc.batch.held",
		"Rework":              "qc.batch.held",
		"":                    "",
		"Pending review":      "", // unknown: announce nothing
	} {
		if got := ReleaseEventType(in); got != want {
			t.Errorf("ReleaseEventType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHoldEventVocabulary(t *testing.T) {
	for in, want := range map[string]string{
		"Placed on hold": "placed_on_hold", "hold": "placed_on_hold",
		"Released": "released", "Rejected": "rejected", "Reworked": "reworked", "": "",
	} {
		if got := normalizeHoldEvent(in); got != want {
			t.Errorf("normalizeHoldEvent(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"Active hold": "active_hold", "Cleared": "cleared", "Scrapped": "scrapped", "": "",
	} {
		if got := normalizeHoldStatus(in); got != want {
			t.Errorf("normalizeHoldStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

// Migration 013.
func TestLabMeasurementValidation(t *testing.T) {
	s := &Store{}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		in   CreateLabMeasurementInput
	}{
		{"no sample", CreateLabMeasurementInput{Parameter: "caffeine"}},
		{"no parameter", CreateLabMeasurementInput{SampleBusinessID: "SMP-26-0001"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.CreateLabMeasurement(ctx, tc.in); !errors.Is(err, ErrBadInput) {
				t.Fatalf("got %v, want ErrBadInput", err)
			}
		})
	}
}

/*
The rollup map decides what keeps feeding SPC, the dashboard and the CoA PDF.

A parameter wrongly mapped writes a caffeine reading into the moisture column of
a batch summary that the CoA is printed from; a known parameter wrongly left
unmapped silently stops feeding four consumers. Both are quiet, so both are
pinned here.
*/
func TestMeasurementRollupField(t *testing.T) {
	for in, want := range map[string]string{
		"moisture": "moisture", "Moisture %": "moisture", "moisture_pct": "moisture",
		"water activity": "water_activity", "aw": "water_activity",
		"cup score": "cup_score", "SCA score": "cup_score", "Total score": "cup_score",
		"defects": "defects", "Defect count": "defects",
		// Everything else is a measurement and nothing more — that is the point
		// of the table.
		"caffeine": "", "chlorogenic acid": "", "pH": "", "": "",
	} {
		if got := MeasurementRollupField(in); got != want {
			t.Errorf("MeasurementRollupField(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseMeasurementValue(t *testing.T) {
	if got := parseMeasurementValue("11.5"); got == nil || *got != 11.5 {
		t.Errorf("plain number not parsed: %v", got)
	}
	if got := parseMeasurementValue("11.5%"); got == nil || *got != 11.5 {
		t.Errorf("trailing percent not handled: %v", got)
	}
	// Real results are often not numbers. They must still be recordable, with
	// value_text keeping what was typed.
	for _, in := range []string{"<0.1", "trace", "pass", "", "  "} {
		if got := parseMeasurementValue(in); got != nil {
			t.Errorf("parseMeasurementValue(%q) = %v, want nil", in, *got)
		}
	}
}

// Migration 015.
func TestIncomingInspectionRequiresASourceLot(t *testing.T) {
	s := &Store{}
	if _, err := s.UpsertIncomingInspection(context.Background(),
		UpsertIncomingInspectionInput{BusinessID: "IIN-26-0001"}); !errors.Is(err, ErrBadInput) {
		t.Fatalf("got %v, want ErrBadInput", err)
	}
}

/*
The delete allow-list is the whole safety property, so it is pinned.

A LIMS whose results can be removed is not evidence of anything. Only planning
data that has not been acted on may go; everything that forms the audit trail
must be refused before any SQL runs.
*/
func TestOnlyPlanningRegistersAreDeletable(t *testing.T) {
	s := &Store{}
	ctx := context.Background()

	for _, table := range []string{
		"qc_samples", "qc_physical_tests", "qc_chemical_tests", "qc_cupping_sessions",
		"qc_lab_measurements", "qc_instrument_calibrations", "qc_coa",
		"qc_certification_requests", "qc_custody_logs", "qc_hold_events",
		"qc_release_decisions", "qc_capas", "qc_non_conformances",
		"qc_compliance_logs", "qc_external_audits", "qc_instruments", "qc_technicians",
	} {
		if err := s.DeleteRegisterRow(ctx, table, "X-1"); !errors.Is(err, ErrBadInput) {
			t.Errorf("%s is deletable — it is part of the quality record and must not be", table)
		}
	}

	// And the four that are, refuse a blank id before touching the pool.
	for table := range deletableWhileStatus {
		if err := s.DeleteRegisterRow(ctx, table, "  "); !errors.Is(err, ErrBadInput) {
			t.Errorf("%s accepted a blank id: %v", table, err)
		}
	}
}

func TestContainsFold(t *testing.T) {
	if !containsFold([]string{"draft"}, " Draft ") {
		t.Error("status comparison should ignore case and padding")
	}
	if containsFold([]string{"draft"}, "approved") {
		t.Error("approved must not match draft")
	}
}

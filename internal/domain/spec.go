package domain

import "strings"

// Verdicts the service computes against a specification (migration 016).
const (
	VerdictPass       = "pass"
	VerdictFail       = "fail"
	VerdictNoSpec     = "no_spec"
	VerdictNotNumeric = "not_numeric"
)

// Spec actions, mirroring iag-production's prod_ccp_limits.action_on_fail.
const (
	ActionNone = "none"
	ActionFlag = "flag"
	ActionHold = "hold"
)

// WithinBand reports whether v lies inside [lsl, usl]. A nil limit is
// one-sided: no lower or no upper bound. Both bounds are inclusive, matching
// how the limits are written on the forms ("<= 12.5%").
func WithinBand(v float64, lsl, usl *float64) bool {
	if lsl != nil && v < *lsl {
		return false
	}
	if usl != nil && v > *usl {
		return false
	}
	return true
}

// ActionRank orders actions by consequence, so the strongest failing action
// across several judged parameters decides what happens to the record.
func ActionRank(action string) int {
	switch action {
	case ActionHold:
		return 2
	case ActionFlag:
		return 1
	default:
		return 0
	}
}

// CombineVerdicts folds per-parameter verdicts into the record's verdict: any
// fail fails it, otherwise any pass passes it, otherwise a numeric problem is
// reported over a missing spec. An empty list is "" — nothing was evaluated.
func CombineVerdicts(verdicts []string) string {
	seen := map[string]bool{}
	for _, v := range verdicts {
		seen[v] = true
	}
	switch {
	case seen[VerdictFail]:
		return VerdictFail
	case seen[VerdictPass]:
		return VerdictPass
	case seen[VerdictNotNumeric]:
		return VerdictNotNumeric
	case seen[VerdictNoSpec]:
		return VerdictNoSpec
	default:
		return ""
	}
}

// ResultStance classifies a user-entered result word against the verdict
// vocabulary: +1 agrees with pass, -1 agrees with fail, 0 takes no position.
//
// The three registers each have their own words — measurements say pass/fail,
// incoming inspections accept/quarantine/reject, in-process checks
// in_control/out_of_control — and all of them have a neutral "pending".
func ResultStance(result string) int {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "pass", "passed", "accept", "accepted", "in_control", "in control", "ok":
		return 1
	case "fail", "failed", "reject", "rejected", "quarantine", "out_of_control", "out of control", "oos":
		return -1
	default:
		return 0
	}
}

// ContradictsVerdict reports whether a user's result takes the opposite
// position to a definitive verdict. A neutral result, or a verdict that is not
// pass/fail, never contradicts.
func ContradictsVerdict(result, verdict string) bool {
	stance := ResultStance(result)
	switch verdict {
	case VerdictPass:
		return stance < 0
	case VerdictFail:
		return stance > 0
	default:
		return false
	}
}

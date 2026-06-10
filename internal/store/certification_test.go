package store

import "testing"

func TestNextCertStage(t *testing.T) {
	cases := map[string]string{
		CertStageQCReview:    CertStageOpsApproval,
		CertStageOpsApproval: CertStageCEOSignoff,
		CertStageCEOSignoff:  CertStageCoAIssued,
		CertStageCoAIssued:   CertStageExportReady,
	}
	for cur, want := range cases {
		if got := nextCertStage(cur); got != want {
			t.Fatalf("nextCertStage(%q) = %q, want %q", cur, got, want)
		}
	}
}

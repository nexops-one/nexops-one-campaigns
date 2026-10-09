// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
)

func TestTallyCountsRejectedAndExpired(t *testing.T) {
	rs := []engine.ControlResult{
		{Status: engine.StatusReady}, {Status: engine.StatusMonitoring}, {Status: engine.StatusRejected},
		{Status: engine.StatusExpired}, {Status: engine.StatusInReview}, {Status: engine.StatusNotAssessed},
	}
	tl := engine.TallyOf(rs, catalog.Scoring{CountsAsReady: []string{"ready", "monitoring"}, Assumptions: "a"})
	if tl.InScope != 6 || tl.Assessable != 5 || tl.Rejected != 1 || tl.Expired != 1 || tl.ScoreNumerator != 2 || tl.ScorePct != 40 || tl.CoveragePct != 83 {
		t.Fatalf("tally = %+v", tl)
	}
	sum := engine.SumTallies(tl, tl)
	if sum.InScope != 12 || sum.Rejected != 2 || sum.ScorePct != 40 || sum.Assumptions != "" {
		t.Fatalf("sum = %+v", sum)
	}
}

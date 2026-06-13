package statesmantest_test

import (
	"testing"

	"github.com/andrioid/statesman/statesmantest"
)

// TestScenarios runs the curated txtar behavioral corpus: each fixture is a
// machine.json plus a send/advance/expect script driven on virtual time. New
// machines are added as data, not Go code.
func TestScenarios(t *testing.T) {
	t.Parallel()
	statesmantest.RunScenarios(t, "testdata/scenarios")
}

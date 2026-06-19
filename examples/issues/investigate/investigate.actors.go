package investigate

import (
	"context"
	"time"

	"github.com/andrioid/statesman/examples/issues/agent"
)

// AnalyseInput / AnalyseResult are the analyse promise adapter's I/O.
type AnalyseInput struct {
	Number int
	Title  string
	Body   string
}

type AnalyseResult struct{ Findings string }

const analysePrompt = `Investigate GitHub issue #{{.Number}} in this repository using read-only tools.

Title: {{.Title}}

Body:
{{.Body}}

Produce concise root-cause findings (a few sentences). Do not modify any files.`

type analyseVars struct {
	Number int
	Title  string
	Body   string
}

// AnalyseCode runs the code-analysis agent for the issue (the "LLM agent from
// shell"). ctx bounds the subprocess so the AnalyseTimeout edge can kill it.
func AnalyseCode(ctx context.Context, in AnalyseInput) (AnalyseResult, error) {
	out, err := agent.Invoke(ctx, "analyse", analysePrompt, analyseVars{in.Number, in.Title, in.Body})
	if err != nil {
		return AnalyseResult{}, err
	}
	return AnalyseResult{Findings: out}, nil
}

// AnalyseTimeout bounds a single analysis run (delays.go convention).
const AnalyseTimeout = 5 * time.Minute

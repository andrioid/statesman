package investigate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// AnalyseInput / AnalyseResult are the analyse promise adapter's I/O.
type AnalyseInput struct{ Number int }

type AnalyseResult struct{ Findings string }

// AnalyseCode execs the code-analysis agent for the issue (the "LLM agent from
// shell"). It uses CommandContext so the AnalyseTimeout `after` edge — which exits
// the state and cancels ctx — actually kills the process. Set ISSUES_AGENT to your
// agent CLI; it is called as `$ISSUES_AGENT analyse <number>` and its stdout is the
// findings.
func AnalyseCode(ctx context.Context, in AnalyseInput) (AnalyseResult, error) {
	agent := os.Getenv("ISSUES_AGENT")
	if agent == "" {
		return AnalyseResult{}, errors.New("investigate: set ISSUES_AGENT to your code-analysis agent command")
	}
	out, err := exec.CommandContext(ctx, agent, "analyse", strconv.Itoa(in.Number)).Output()
	if err != nil {
		return AnalyseResult{}, fmt.Errorf("analyse agent: %w", err)
	}
	return AnalyseResult{Findings: strings.TrimSpace(string(out))}, nil
}

// AnalyseTimeout bounds a single analysis run (delays.go convention).
const AnalyseTimeout = 5 * time.Minute

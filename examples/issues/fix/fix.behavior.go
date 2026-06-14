package fix

// Impl is the fix sub-machine's behavior.
type Impl struct{}

// ApplyEditInput / RunTestsInput map the seeded context to each agent's input.
func (Impl) ApplyEditInput(ctx Context) EditInput {
	return EditInput{Number: ctx.Number, Findings: ctx.Findings}
}

func (Impl) RunTestsInput(ctx Context) TestInput {
	return TestInput{Number: ctx.Number}
}

// SavePatchOnEditDone records the agent's patch into context.
func (Impl) SavePatchOnEditDone(ctx Context, evt EditDone) ActionResult {
	f := ctx.ContextFields
	f.Patch = evt.Output.Patch
	return Assign{Fields: f}
}

// TestsPassedOnTestDone ends the loop when the test run succeeded.
func (Impl) TestsPassedOnTestDone(ctx Context, evt TestDone) bool {
	return evt.Output.Passed
}

// CanRetryOnTestDone re-edits while the attempt budget allows.
func (Impl) CanRetryOnTestDone(ctx Context, evt TestDone) bool {
	return ctx.Attempt+1 < ctx.MaxAttempts
}

// NextAttemptOnTestDone bumps the edit attempt before re-editing.
func (Impl) NextAttemptOnTestDone(ctx Context, evt TestDone) ActionResult {
	f := ctx.ContextFields
	f.Attempt++
	return Assign{Fields: f}
}

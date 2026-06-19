package investigate

// Impl is the investigate sub-machine's behavior.
type Impl struct{}

// AnalyseCodeInput maps the parent-seeded context to the analyse agent's input.
func (Impl) AnalyseCodeInput(ctx Context) AnalyseInput {
	return AnalyseInput{Number: ctx.Number, Title: ctx.Title, Body: ctx.Body}
}

// SaveFindingsOnAnalyseDone records the agent's findings into the terminal
// context, which the parent reads from the invoke's done payload.
func (Impl) SaveFindingsOnAnalyseDone(ctx Context, evt AnalyseDone) ActionResult {
	f := ctx.ContextFields
	f.Findings = evt.Output.Findings
	return Assign{Fields: f}
}

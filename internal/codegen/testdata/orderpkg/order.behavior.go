package orderpkg

// orderImpl is the user-implemented behavior satisfying the generated
// Implementations interface.
type orderImpl struct{}

func (orderImpl) ValidateFormOnSubmit(ctx Context, evt Submit) ActionResult {
	return SendToInventory{Command: WatchSKUs{SKUs: evt.Form.SKUs}}
}

func (orderImpl) IncrementRetriesOnChargeError(ctx Context, evt ChargeError) ActionResult {
	return incRetries(ctx)
}

func (orderImpl) IncrementRetriesOnProcessingCharging(ctx Context) ActionResult {
	return incRetries(ctx)
}

func incRetries(ctx Context) ActionResult {
	return Assign{Fields: ContextFields{UserID: ctx.UserID, Amount: ctx.Amount, Retries: ctx.Retries + 1}}
}

// HasRetriesLeft is the context-only fallback for the reused hasRetriesLeft guard
// (wired on both error.invoke.charge and the charging timeout).
func (orderImpl) HasRetriesLeft(ctx Context) bool {
	return ctx.Retries < 3
}

// HasRetriesLeftOnChargeError overrides the fallback for the error.invoke.charge
// callsite, exercising per-callsite precedence (it can read evt).
func (orderImpl) HasRetriesLeftOnChargeError(ctx Context, evt ChargeError) bool {
	return ctx.Retries < 3
}

func (orderImpl) ChargeCardInput(ctx Context) ChargeInput {
	return ChargeInput{UserID: ctx.UserID, Amount: ctx.Amount}
}

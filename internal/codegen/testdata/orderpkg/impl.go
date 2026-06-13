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

func (orderImpl) HasRetriesLeftOnChargeError(ctx Context, evt ChargeError) bool {
	return ctx.Retries < 3
}

func (orderImpl) HasRetriesLeftOnProcessingCharging(ctx Context) bool {
	return ctx.Retries < 3
}

func (orderImpl) ChargeCardInput(ctx Context) ChargeInput {
	return ChargeInput{UserID: ctx.UserID, Amount: ctx.Amount}
}

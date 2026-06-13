package codegen

import "testing"

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"SUBMIT":              "Submit",
		"PAYMENT_FAILED":      "PaymentFailed",
		"validateForm":        "ValidateForm",
		"payment-charge-card": "PaymentChargeCard",
		"hasRetriesLeft":      "HasRetriesLeft",
		"WATCH_SKUS":          "WatchSkus",
	}
	for in, want := range cases {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEventGoName(t *testing.T) {
	cases := map[string]string{
		"SUBMIT":                "Submit",
		"done.invoke.charge":    "ChargeDone",
		"error.invoke.charge":   "ChargeError",
		"done.state.processing": "ProcessingDone",
	}
	for in, want := range cases {
		got, err := EventGoName(in)
		if err != nil {
			t.Errorf("EventGoName(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("EventGoName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGoIdentHardFails(t *testing.T) {
	for _, bad := range []string{"2fa_required", "123", ""} {
		if _, err := GoIdent(bad); err == nil {
			t.Errorf("GoIdent(%q) should fail (not a valid identifier)", bad)
		}
	}
}

func TestCheckCollisions(t *testing.T) {
	if err := CheckCollisions([]string{"SUBMIT", "PAYMENT_FAILED", "RETRY"}); err != nil {
		t.Errorf("distinct names should not collide: %v", err)
	}
	// SUBMIT and submit both normalize to Submit.
	if err := CheckCollisions([]string{"SUBMIT", "submit"}); err == nil {
		t.Error("SUBMIT/submit should collide")
	}
	// payment_failed and paymentFailed both normalize to PaymentFailed.
	if err := CheckCollisions([]string{"payment_failed", "paymentFailed"}); err == nil {
		t.Error("payment_failed/paymentFailed should collide")
	}
}

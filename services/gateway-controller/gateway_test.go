package gatewayctl

import "testing"

func TestNilLimiterDefaultsToDenyAll(t *testing.T) {
	controller := New("gw-1", "eu-standard", nil, nil)
	if controller.limiter.Allow(42) {
		t.Fatal("missing limiter must deny admission")
	}
	if _, err := controller.Admit(nil); err != ErrVerifierUnavailable {
		t.Fatalf("missing verifier error = %v, want ErrVerifierUnavailable", err)
	}
}

func TestEpochBudgetCannotResetByAlternatingEpochs(t *testing.T) {
	budget := NewEpochBudget(2)
	if !budget.Allow(42) || !budget.Allow(42) || budget.Allow(42) {
		t.Fatal("epoch 42 did not enforce its budget")
	}
	if !budget.Allow(41) || !budget.Allow(41) || budget.Allow(41) {
		t.Fatal("epoch 41 did not enforce its independent budget")
	}
	if budget.Allow(42) {
		t.Fatal("switching epochs reset the epoch 42 budget")
	}
	if !budget.Allow(43) {
		t.Fatal("new epoch should receive a fresh budget")
	}
	if budget.Allow(41) {
		t.Fatal("epochs older than the current verification window must be denied")
	}
}

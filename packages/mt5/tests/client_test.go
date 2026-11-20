package mt5_test

import (
	"errors"
	"testing"

	"portexa/mt5"
)

func TestInvalidConfigurationStopsBeforeDial(t *testing.T) {
	_, err := mt5.Fetch(t.Context(), mt5.Config{
		IPAddress: "broker.example", Port: 2085, Server: "Synthetic-MT5",
		Login: 1, Password: "synthetic-pw",
	})
	if !errors.Is(err, mt5.ErrConfig) {
		t.Fatalf("invalid address error = %v", err)
	}
}

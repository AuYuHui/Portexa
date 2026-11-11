package protocol_test

import (
	"bytes"
	. "portexa/mt4/internal/protocol"
	"testing"
)

func TestLZODeclaredLengthAndInputBoundary(t *testing.T) {
	compressed := []byte{22, 'A', 'B', 'C', 'D', 'E', 17, 0, 0}
	got, err := DecodeLZO(compressed, 5, 65535)
	if err != nil || !bytes.Equal(got, []byte("ABCDE")) {
		t.Fatalf("literal-only LZO fixture failed: %v", err)
	}
	for _, value := range []struct {
		input    []byte
		expected int
	}{
		{compressed, 4}, {compressed, 6}, {compressed[:len(compressed)-1], 5},
		{append(bytes.Clone(compressed), 0), 5}, {compressed, 65536},
	} {
		if _, err := DecodeLZO(value.input, value.expected, 65535); err == nil {
			t.Fatal("LZO corruption or declared-length mismatch was accepted")
		}
	}
}

func TestServerConfigurationStructureAndBroker(t *testing.T) {
	value := make([]byte, 196+2*160)
	offset := 4 + 2*160
	copy(value[offset:], "FixtureBroker-Live9")
	got, err := ValidateServerConfiguration(value, "fixturebroker-Live1")
	if err != nil || got.AccessCount != 2 {
		t.Fatalf("valid synthetic broker configuration failed: %v", err)
	}
	if _, err := ValidateServerConfiguration(value, "DifferentBroker-Live9"); err == nil {
		t.Fatal("different broker configuration was accepted")
	}
	if _, err := ValidateServerConfiguration(value[:len(value)-1], "FixtureBroker-Live9"); err == nil {
		t.Fatal("truncated configuration was accepted")
	}
	clear(value[offset : offset+64])
	if _, err := ValidateServerConfiguration(value, "FixtureBroker-Live9"); err == nil {
		t.Fatal("empty name was treated as broker verification")
	}
}

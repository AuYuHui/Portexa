package protocol_test

import (
	"bytes"
	"errors"
	"testing"

	. "portexa/mt4/internal/protocol"
)

func TestReadOnlyCommandGuardRejectsEverythingOutsideQueryShapes(t *testing.T) {
	for command := 0; command < 256; command++ {
		if command == 0x10 || command == 0x21 {
			continue
		}
		state := StreamState{Counter: 9, Previous: 7}
		before := state
		var output bytes.Buffer
		err := WriteCommand(&output, &state, []byte{byte(command)})
		if !errors.Is(err, ErrReadOnlyCommand) || output.Len() != 0 || state != before {
			t.Fatalf("unapproved command %02x changed the stream or reached the writer", command)
		}
	}
	for _, command := range [][]byte{nil, {0x10, 0}, {0x08}, {0x22, 0}, {0x1e, 0}, append(AccountRequest(1470), 0)} {
		var output bytes.Buffer
		if err := WriteCommand(&output, &StreamState{}, command); !errors.Is(err, ErrReadOnlyCommand) || output.Len() != 0 {
			t.Fatal("malformed query escaped the read-only guard")
		}
	}
	for _, command := range [][]byte{{0x10}, {0x21}, append([]byte{0x08}, make([]byte, 16)...), append([]byte{0x0a}, make([]byte, 16)...), AccountRequest(1492)} {
		var output bytes.Buffer
		if err := WriteCommand(&output, &StreamState{}, command); err != nil || output.Len() != len(command) {
			t.Fatalf("read-only query rejected: %v", err)
		}
	}
}

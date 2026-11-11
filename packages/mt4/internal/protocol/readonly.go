package protocol

import (
	"bytes"
	"encoding/binary"
)

// Only these exact query shapes may reach the authenticated stream. This guard
// runs before encryption or writing, so a rejected command cannot advance it.
func validateReadOnlyCommand(command []byte) error {
	if len(command) == 0 {
		return ErrReadOnlyCommand
	}
	switch command[0] {
	case 0x10, 0x21:
		if len(command) == 1 {
			return nil
		}
	case 0x08, 0x0a:
		if len(command) == 17 && bytes.Equal(command[1:], make([]byte, 16)) {
			return nil
		}
	case 0x22:
		if len(command) == 9 {
			_, err := HistoryRequest(binary.LittleEndian.Uint32(command[1:]), binary.LittleEndian.Uint32(command[5:]))
			if err == nil {
				return nil
			}
		}
	case 0x1e:
		// Account requests are generated locally from the fixed read-only profile.
		if bytes.Equal(command, AccountRequest(ClientBuild)) {
			return nil
		}
	}
	return ErrReadOnlyCommand
}

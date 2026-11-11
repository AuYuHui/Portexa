package protocol

import (
	"encoding/binary"
	"errors"
)

// EvaluateFeatures implements the bounded arithmetic instruction set carried
// in server feature records. It executes no host code and has no jump opcodes.
func EvaluateFeatures(program []byte) (uint64, error) {
	if len(program)%8 != 0 || len(program) > 4095 {
		return 0, errors.New("invalid feature program size")
	}
	var accumulator uint64
	for offset := 0; offset+24 <= len(program); offset += 24 {
		instruction := binary.LittleEndian.Uint64(program[offset:])
		opcode := (instruction >> 21) & 255
		if opcode == 0xd8 {
			return accumulator, nil
		}
		a := binary.LittleEndian.Uint64(program[offset+8:])
		b := binary.LittleEndian.Uint64(program[offset+16:])
		if ((a >> 21) & 255) == 0xf5 {
			a = accumulator
		}
		switch opcode {
		case 0xab:
			accumulator = a + b
		case 0xa9:
			accumulator = a - b
		case 0x54:
			accumulator = a & b
		case 0x70:
			accumulator = a | b
		case 0x91:
			accumulator = a ^ b
		case 0xb1:
			accumulator = a << (b % 24)
		case 0xc8:
			accumulator = a >> (b % 24)
		}
	}
	return accumulator, nil
}

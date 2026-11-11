package protocol_test

import (
	"encoding/binary"
	"math"
	. "portexa/mt4/internal/protocol"
	"testing"
)

func featureProgram(words ...uint64) []byte {
	var out []byte
	for _, word := range words {
		out = binary.LittleEndian.AppendUint64(out, word)
	}
	return out
}

func TestFeatureOpcodesAndWordMask(t *testing.T) {
	known := map[uint64]uint64{
		0x54: 17, 0x70: 29, 0x91: 12, 0xa9: math.MaxUint64 - 11,
		0xab: 46, 0xb1: 544, 0xc8: 0,
	}
	for opcode := uint64(0); opcode < 256; opcode++ {
		instruction := (opcode << 21) | 0x8000000000000055
		got, err := EvaluateFeatures(featureProgram(instruction, 17, 29))
		if err != nil || got != known[opcode] {
			t.Fatalf("synthetic feature opcode %x differs: %v", opcode, err)
		}
	}
}

func TestFeaturesReferenceHaltAndIgnoredTail(t *testing.T) {
	ref := uint64(0xf5 << 21)
	program := featureProgram(
		0xab<<21, 17, 29,
		0x91<<21, ref, 255,
		0x54<<21, ref, 240,
		0x70<<21, ref, 3,
		0xa9<<21, ref, 5,
		0xb1<<21, ref, 25,
		0xc8<<21, ref, 24,
		0xd8<<21, 0, 0,
		0xab<<21, 1, 2,
	)
	got, err := EvaluateFeatures(program)
	if err != nil || got != 412 {
		t.Fatalf("synthetic feature sequence differs: %d %v", got, err)
	}
	// B is literal even when it carries the reference marker.
	got, err = EvaluateFeatures(featureProgram(0xab<<21, 17, 29, 0xab<<21, ref, ref))
	if err != nil || got != 46+ref {
		t.Fatal("B must remain a literal")
	}
	got, err = EvaluateFeatures(featureProgram(0xab<<21, 17, 29, 0xd8<<21, 0))
	if err != nil || got != 46 {
		t.Fatal("incomplete trailing instruction must be ignored")
	}
	got, err = EvaluateFeatures(featureProgram(0xab<<21, math.MaxUint64, 1))
	if err != nil || got != 0 {
		t.Fatal("feature arithmetic must wrap at 64 bits")
	}
	if _, err := EvaluateFeatures([]byte{1}); err == nil {
		t.Fatal("unaligned feature bytes must be rejected")
	}
}

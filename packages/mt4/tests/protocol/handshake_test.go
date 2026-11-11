package protocol_test

import (
	"bytes"
	"encoding/hex"
	"io"
	. "portexa/mt4/internal/protocol"
	"testing"
)

// All values in this file are synthetic. No captured account material is used.
func unhex(t *testing.T, value string) []byte {
	t.Helper()
	out, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInitialPacketGolden(t *testing.T) {
	want := unhex(t, "0047f776a8793785fe3db9c69ba360f96993db1a90c9d5558338747c")
	if got := InitialPacket(123456, "fixture-only!", 0); !bytes.Equal(got, want) {
		t.Fatalf("synthetic initial packet differs: %x", got)
	}
}

func TestRawDigestNativeSyntheticVectors(t *testing.T) {
	// Expected values were obtained by offline CPU execution of the original
	// arithmetic routine on these synthetic inputs, then checked independently.
	for _, fixture := range []struct {
		length int
		want   string
	}{
		{1, "64c93ae7e873d96e60864d2e0a816e60"},
		{16, "6a3e0707df1c73cde9cab1cd507e35e1"},
		{64, "377af56cf86d7878ba432343e986e6a7"},
		{97, "a6f1522df29388fe80f72383ed7c313c"},
	} {
		value := make([]byte, fixture.length)
		for i := range value {
			value[i] = byte(i*17 + 31)
		}
		got := RawDigest16(value)
		if !bytes.Equal(got[:], unhex(t, fixture.want)) {
			t.Fatalf("digest differs for %d synthetic bytes", fixture.length)
		}
	}
}

func TestChallengeOperatorsSyntheticVectors(t *testing.T) {
	var block [64]byte
	for i := range block {
		block[i] = byte(i*7 + 3)
	}
	for _, fixture := range []struct {
		field uint32
		want  string
	}{
		{1, "79484847e03c45876a77d2d9877dc490"},
		{0x01020304, "3c1643b22f791596779bb7ae4fb85795"},
		{0x12345678, "a417ebd242bcb037ee18472fff09f344"},
		{0xaabbccdd, "c3256f62d3609aae3acf5214da5bf033"},
		{0x100, "1b21a22d84e40da0c137effc5f3370a4"},
	} {
		got, err := ChallengeKey(123456, PasswordDigest("fixture-only!"), fixture.field, block)
		if err != nil || !bytes.Equal(got[:], unhex(t, fixture.want)) {
			t.Fatalf("challenge differs for synthetic field %x: %v", fixture.field, err)
		}
	}
	if _, err := ChallengeKey(123456, PasswordDigest("fixture-only!"), 0, block); err == nil {
		t.Fatal("legacy challenge must be explicitly rejected")
	}
}

func TestRandomKnownSequence(t *testing.T) {
	r := LegacyRandom(1)
	for _, want := range []uint32{41, 18467, 6334, 26500} {
		if got := r.Next(); got != want {
			t.Fatalf("random sequence differs: got %d want %d", got, want)
		}
	}
}

func TestRecordCBCAndTailGolden(t *testing.T) {
	var key [16]byte
	for i := range key {
		key[i] = byte(i)
	}
	value := make([]byte, 40)
	for i := range value {
		value[i] = byte(i)
	}
	wire, err := PackRecord(Record{Kind: 9, Mode: 1, Data: value}, 0x123, key)
	if err != nil {
		t.Fatal(err)
	}
	want := unhex(t, "28292225282d10160a940bb5416ef045f1c39458c653ea5a3cf456b4ca488aa383c79c98b34797cb2000200020002000")
	if !bytes.Equal(wire, want) {
		t.Fatal("synthetic CBC/tail packet differs from independent vector")
	}
	got, err := UnpackInner(want, key)
	if err != nil || len(got) != 1 || !bytes.Equal(got[0].Data, value) {
		t.Fatalf("cannot decode independent Record vector: %v", err)
	}
	if _, err := UnpackInner(want[:len(want)-1], key); err == nil {
		t.Fatal("truncated Record must not be accepted")
	}
}

func TestStreamStateAcrossChunksAndKeyChange(t *testing.T) {
	var first, second [16]byte
	for i := range first {
		first[i], second[i] = byte(i*7+3), byte(i*11+5)
	}
	value := make([]byte, 501)
	for i := range value {
		value[i] = byte(i*13 + 5)
	}
	wire := bytes.Clone(value)
	tx := StreamState{Key: first}
	tx.Encode(wire[:71])
	tx.Key = second
	tx.Encode(wire[71:])
	rx := StreamState{Key: first}
	for i := range wire {
		if i == 71 {
			rx.Key = second
		}
		rx.Decode(wire[i : i+1])
	}
	if !bytes.Equal(wire, value) || rx.Counter != 501 || rx.Previous != value[500] {
		t.Fatal("outer stream state was lost across reads or key transition")
	}
}

type oneByteReader struct{ io.Reader }

func (r oneByteReader) Read(value []byte) (int, error) {
	if len(value) > 1 {
		value = value[:1]
	}
	return r.Reader.Read(value)
}

func TestInitialResponseIndependentSyntheticWire(t *testing.T) {
	// Built with independent Python framing/AES, not with this Go reader.
	wire := unhex(t, "004714291ffa26eb14561c277f01d2ab76e382e7e5d57f4602b6fc275f01f2ab16a382e7e5355f4602d61ce7bf01d26b36a382a7a555bf4602f63ce7df01326b166382a7a575dfc6e179013049098ffc90be7c7f0f8e50b9bbd9dd880a6afda7142a1962c9384015d2e40c0c26ec8ec2")
	got, err := ReadInitial(oneByteReader{bytes.NewReader(wire)}, 123456, "fixture-only!")
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerBuild != 1470 || len(got.Records) != 1 || got.Records[0].Kind != 9 || len(got.Records[0].Data) != 24 {
		t.Fatal("independent response fields differ")
	}
	if got.Rx.Counter != uint32(len(wire)-1) || got.Rx.Key != got.Key {
		t.Fatal("receive state was not retained when switching to the challenge key")
	}
	for _, length := range []int{0, 1, 71, 80, len(wire) - 1} {
		if _, err := ReadInitial(bytes.NewReader(wire[:length]), 123456, "fixture-only!"); err == nil {
			t.Fatalf("truncation at %d bytes was accepted", length)
		}
	}
	wrongStatus := bytes.Clone(wire)
	wrongStatus[0] = 4
	if _, err := ReadInitial(bytes.NewReader(wrongStatus), 123456, "fixture-only!"); err == nil {
		t.Fatal("nonzero server status was accepted")
	}
}

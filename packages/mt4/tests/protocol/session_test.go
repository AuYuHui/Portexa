package protocol_test

import (
	"bytes"
	"encoding/binary"
	"math"
	. "portexa/mt4/internal/protocol"
	"testing"
)

func encryptedFixture(value []byte) ([]byte, StreamState) {
	key := [16]byte{1, 3, 5, 7, 9, 11, 13, 15, 2, 4, 6, 8, 10, 12, 14, 16}
	tx := StreamState{Key: key}
	encoded := bytes.Clone(value)
	tx.Encode(encoded)
	return encoded, StreamState{Key: key}
}

func TestAuxiliaryUnsupportedConsumesOnlyStatus(t *testing.T) {
	encoded, state := encryptedFixture([]byte{1, 0x7b})
	reader := bytes.NewReader(encoded)
	result, err := ReadAuxiliary(reader, &state)
	if err != nil || result.Status != 1 || result.Count != 0 || reader.Len() != 1 {
		t.Fatalf("unsupported optional response consumed a later response: %v", err)
	}
	next, err := state.Read(reader, 1)
	if err != nil || next[0] != 0x7b {
		t.Fatal("cipher feedback was lost across the optional response")
	}
}

func TestAuxiliaryRejectsInvalidCountsAndTruncation(t *testing.T) {
	for _, count := range []uint32{0, 1025, 1} {
		value := binary.LittleEndian.AppendUint32([]byte{0}, count)
		encoded, state := encryptedFixture(value)
		if _, err := ReadAuxiliary(bytes.NewReader(encoded), &state); err == nil {
			t.Fatalf("invalid count or truncated table was accepted: %d", count)
		}
	}
}

func TestAccountMinimumStructureAndFiniteMoney(t *testing.T) {
	data := make([]byte, AccountBaseSize+2*TradeRecordSize)
	binary.LittleEndian.PutUint32(data[0x44:], 100)
	binary.LittleEndian.PutUint64(data[0x48:], math.Float64bits(1234.5))
	binary.LittleEndian.PutUint64(data[0x50:], math.Float64bits(-0.25))
	binary.LittleEndian.PutUint32(data[0x35d8:], 1)
	copy(data[0x34f4:], "USD")
	for i := 0; i < 2; i++ {
		binary.LittleEndian.PutUint32(data[AccountBaseSize+i*TradeRecordSize:], uint32(i+1))
		binary.LittleEndian.PutUint32(data[AccountBaseSize+i*TradeRecordSize+0x18:], 6)
	}
	got, err := ParseAccount(data)
	if err != nil || got.Balance != 1234.5 || got.Credit != -0.25 || got.Leverage != 100 || got.InvestorIndicator != 1 || got.TradeRecords != 2 || got.TrailingBytes != 0 {
		t.Fatalf("synthetic account fields did not match the external layout: %v", err)
	}
	if _, err := ParseAccount(data[:AccountBaseSize-1]); err == nil {
		t.Fatal("short account structure was accepted")
	}
	for _, money := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		binary.LittleEndian.PutUint64(data[0x48:], math.Float64bits(money))
		if _, err := ParseAccount(data); err == nil {
			t.Fatal("nonfinite balance was accepted")
		}
	}
}

func TestHistoryEmptyAndUnchangedResponses(t *testing.T) {
	for _, plaintext := range [][]byte{{1}, {0, 0, 0, 0, 0}} {
		encoded, state := encryptedFixture(plaintext)
		reader := bytes.NewReader(encoded)
		result, err := ReadHistory(reader, &state)
		if err != nil || result.Count != 0 || reader.Len() != 0 {
			t.Fatalf("valid empty history response failed: %v", err)
		}
	}
	if _, err := HistoryRequest(200, 100); err == nil {
		t.Fatal("reversed history interval was accepted")
	}
	if _, err := HistoryRequest(100, math.MaxUint32); err == nil {
		t.Fatal("unsupported history timestamp was accepted")
	}
}

func TestAccountRejectsMissingCurrencyAndDuplicateOrderTickets(t *testing.T) {
	data := make([]byte, AccountBaseSize+2*TradeRecordSize)
	for i := 0; i < 2; i++ {
		binary.LittleEndian.PutUint32(data[AccountBaseSize+i*TradeRecordSize:], 1)
		binary.LittleEndian.PutUint32(data[AccountBaseSize+i*TradeRecordSize+0x18:], 6)
	}
	copy(data[0x34f4:], "USD")
	if _, err := ParseAccount(data); err == nil {
		t.Fatal("duplicate account order ticket was accepted")
	}
	binary.LittleEndian.PutUint32(data[AccountBaseSize+TradeRecordSize:], 2)
	if _, err := ParseAccount(data); err != nil {
		t.Fatal(err)
	}
	clear(data[0x34f4:0x3500])
	if _, err := ParseAccount(data); err == nil {
		t.Fatal("missing currency in an unknown account layout was accepted")
	}
}

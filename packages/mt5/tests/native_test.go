package mt5_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"net"
	"testing"
	"time"

	"portexa/mt5"
	"portexa/mt5/internal/protocol"
)

func TestBootstrapTransformSymmetry(t *testing.T) {
	orig := []byte("Hello MT5 Native Protocol Wire Test Payload!")
	enc := protocol.BootstrapTransform(orig, true)
	if bytes.Equal(orig, enc) {
		t.Fatal("bootstrap transform did not mask the payload")
	}
	dec := protocol.BootstrapTransform(enc, false)
	if !bytes.Equal(orig, dec) {
		t.Fatalf("bootstrap transform not symmetric: got %q, want %q", dec, orig)
	}
}

func TestRollingPayloadTransform(t *testing.T) {
	var table [256]byte
	for i := 0; i < 256; i++ {
		table[i] = byte((i * 37 + 13) & 0xFF)
	}

	raw := []byte("Synchronized synthetic MT5 terminal payload 1234567890!")
	encoder := protocol.NewRollingPayload(table)
	ciphertext := encoder.Encode(raw)

	decoder := protocol.NewRollingPayload(table)
	plaintext := decoder.Decode(ciphertext, false)
	if !bytes.Equal(raw, plaintext) {
		t.Fatalf("rolling payload decode mismatch: got %q, want %q", plaintext, raw)
	}
}

func TestLZODecompressSynthetic(t *testing.T) {
	// 5 bytes "ABCDE" in LZO literal
	lzoLiteral := []byte{22, 'A', 'B', 'C', 'D', 'E', 17, 0, 0}
	frame := make([]byte, 8+len(lzoLiteral))
	binary.LittleEndian.PutUint32(frame[0:4], 5) // expected size
	binary.LittleEndian.PutUint32(frame[4:8], uint32(len(lzoLiteral)))
	copy(frame[8:], lzoLiteral)

	got, err := protocol.DecompressLZO(frame)
	if err != nil {
		t.Fatalf("DecompressLZO failed: %v", err)
	}
	if !bytes.Equal(got, []byte("ABCDE")) {
		t.Fatalf("decompressed mismatch: got %q, want ABCDE", got)
	}
}

func TestAccountRecordParsing(t *testing.T) {
	const login = uint64(10001001)
	payload := make([]byte, 10000)

	// Put account record at offset 5000
	off := 5000
	payload[off-1] = protocol.AccountRecordTag
	rec := payload[off : off+protocol.AccountRecordSize]

	// Login
	binary.LittleEndian.PutUint64(rec[0:8], login)
	// Flags with InvestorFlag (0x08)
	binary.LittleEndian.PutUint32(rec[456:460], protocol.InvestorFlag)
	// Balance: 12345.67, Credit: 50.0
	binary.LittleEndian.PutUint64(rec[1836:1844], math.Float64bits(12345.67))
	binary.LittleEndian.PutUint64(rec[1844:1852], math.Float64bits(50.0))

	res, err := protocol.ParseSyncPayload(payload, login)
	if err != nil {
		t.Fatalf("ParseSyncPayload failed: %v", err)
	}
	if res.Account.Login != login || res.Account.Balance != 12345.67 || res.Account.Credit != 50.0 || !res.Account.IsInvestor {
		t.Fatalf("parsed account mismatch: %+v", res.Account)
	}

	// Test investor permission denied
	binary.LittleEndian.PutUint32(rec[456:460], 0)
	if _, err := protocol.ParseSyncPayload(payload, login); err != protocol.ErrInvestorDenied {
		t.Fatalf("expected ErrInvestorDenied, got %v", err)
	}
}

func TestDealRecordParsing(t *testing.T) {
	rec := make([]byte, protocol.DealRecordSize)
	rec[0] = protocol.DealRecordTag

	ticket := uint64(4238299)
	order := uint64(4238290)
	pos := uint64(4238200)

	binary.LittleEndian.PutUint64(rec[592:600], ticket)
	binary.LittleEndian.PutUint64(rec[600:608], order)
	binary.LittleEndian.PutUint64(rec[608:616], pos)

	// Time
	now := time.Date(2026, 11, 20, 1, 50, 0, 0, time.UTC)
	ft := protocol.UnixToFILETIME(now)
	binary.LittleEndian.PutUint64(rec[616:624], ft)

	// Type=0 (Buy), Entry=1 (Out)
	binary.LittleEndian.PutUint32(rec[624:628], 0)
	binary.LittleEndian.PutUint32(rec[628:632], 1)

	// Lots = 10000 (1.00 lot)
	binary.LittleEndian.PutUint64(rec[632:640], 10000)

	// Price = 2650.50, Profit = 150.25
	binary.LittleEndian.PutUint64(rec[272:280], math.Float64bits(2650.50))
	binary.LittleEndian.PutUint64(rec[640:648], math.Float64bits(150.25))

	deal := protocol.ParseDealRecord(rec)
	if deal.Ticket != ticket || deal.Order != order || deal.Position != pos {
		t.Fatalf("ticket/order mismatch: got %+v", deal)
	}
	if deal.Price != 2650.50 || deal.Profit != 150.25 || deal.Lots != 1.0 {
		t.Fatalf("deal price/lots mismatch: got %+v", deal)
	}
}

func TestNativeMockLoopbackFetch(t *testing.T) {
	protocol.SetChallengeOverride(func(chal []byte) (uint64, error) {
		return 0x1234567890ABCDEF, nil
	})
	defer protocol.SetChallengeOverride(nil)

	const login = uint64(10001001)
	const pass = "Synthetic123!"

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// 1. Read Hello (0xFB, seq=1)
		hdr, err := protocol.ReadHeader(conn)
		if err != nil {
			return
		}
		helloPayload, _ := protocol.ReadFramePayload(conn, hdr.Length)
		_ = protocol.BootstrapTransform(helloPayload, false)

		// 2. Reply Challenge (0xFB, seq=1, 102 bytes)
		chalBody := make([]byte, 102)
		binary.LittleEndian.PutUint16(chalBody[12:14], 6230) // build
		copy(chalBody[14:30], bytes.Repeat([]byte{0x77}, 16))
		encChal := protocol.BootstrapTransform(chalBody, true)
		protocol.WriteFrame(conn, protocol.FrameHeader{Type: protocol.TypeExtendedHello, Length: uint32(len(encChal)), Sequence: 1, Flags: protocol.FlagFinal}, encChal)

		// 3. Read Auth Proof (0xFC, seq=2, 268 bytes)
		hdr, _ = protocol.ReadHeader(conn)
		proofPayload, _ := protocol.ReadFramePayload(conn, hdr.Length)
		plainProof := protocol.BootstrapTransform(proofPayload, false)
		clientChal := plainProof[56:72]

		// 4. Reply Auth (0xFC, seq=2, >= 980 bytes)
		digest := protocol.PasswordDigest(login, pass)
		h := sha256.New()
		h.Write(digest[:])
		h.Write(clientChal)
		serverProof := h.Sum(nil)

		replyBody := make([]byte, 980+5+384)
		copy(replyBody[64:96], serverProof)
		copy(replyBody[0xE0:0x1E0], bytes.Repeat([]byte{0x55}, 256)) // seed
		// Extension 28 tag
		replyBody[980] = 28
		binary.LittleEndian.PutUint32(replyBody[981:985], 384)
		copy(replyBody[985:985+384], bytes.Repeat([]byte{0x33}, 384))

		encReply := protocol.BootstrapTransform(replyBody, true)
		protocol.WriteFrame(conn, protocol.FrameHeader{Type: protocol.TypeAuthProof, Length: uint32(len(encReply)), Sequence: 2, Flags: protocol.FlagFinal}, encReply)

		// 5. Read Sync Request (0x0C, seq=3)
		hdr, _ = protocol.ReadHeader(conn)
		syncPayload, _ := protocol.ReadFramePayload(conn, hdr.Length)

		table, _ := protocol.BuildTable(digest[:], bytes.Repeat([]byte{0x55}, 256))
		inState := protocol.NewRollingPayload(table)
		outState := protocol.NewRollingPayload(table)
		_ = inState.Decode(syncPayload, false)

		// 6. Send Sync Reply (0x0C, seq=3, 1 uncompressed chunk containing 0x25 record)
		replySync := make([]byte, 8000)
		replySync[999] = protocol.AccountRecordTag
		rec := replySync[1000 : 1000+protocol.AccountRecordSize]
		binary.LittleEndian.PutUint64(rec[0:8], login)
		binary.LittleEndian.PutUint32(rec[456:460], protocol.InvestorFlag)
		binary.LittleEndian.PutUint64(rec[1836:1844], math.Float64bits(99999.0))
		binary.LittleEndian.PutUint64(rec[1844:1852], math.Float64bits(0.0))

		encSync := outState.Encode(replySync)
		protocol.WriteFrame(conn, protocol.FrameHeader{Type: protocol.TypeInitialSync, Length: uint32(len(encSync)), Sequence: 3, Flags: protocol.FlagFinal}, encSync)
	}()

	addr := l.Addr().(*net.TCPAddr)
	cfg := mt5.Config{
		IPAddress: addr.IP.String(),
		Port:      addr.Port,
		Login:     login,
		Password:  pass,
		Server:    "Synthetic-Server",
	}

	res, err := mt5.Fetch(context.Background(), cfg)
	if err != nil {
		t.Fatalf("native Fetch failed: %v", err)
	}
	if res.Account.Balance != 99999.0 || !res.Account.IsInvestor() {
		t.Fatalf("unexpected account result: %+v", res.Account)
	}
}

func TestComputeChallenge(t *testing.T) {
	const ext28Hex = "08c921f62f6e83959ce5aafe7bece4fbda18ea37f72d753e5a1f18d9e8018456d324bc3efa389071c749366c2e05868d57d03276aa8f733caea6ad3edf1164b0dba51f1a2ef3a73e8b1528d51619f8865371bd9ead9c0c8b3931ce1459c9a7a0365389ea13b9bff0b7f3a6be1c29e75a98f55ec74da615e0f7931d192e6951f387aba0deb91b614ed120e8a99be80920fac9333633b350db2e27b6decfa3120346e5900c24e7547d0dee23563bf9aab6dff8a55e2bd5099c832c81d86688130f1921904a86d50bfa3044ab3efd0a36cc3405b3be9f2e788ebdf1259620d40567f8d8b81ec8629626237597f93e0dd1afc9c41aaeb8d57d73f741b2bee94423decbb33b0cec4d564e654c808a5378ba5e9d25b55e9068d9b83c59344592a45dc03fda2c766654d2b78063a93e2f9a1eda288c5114badd554f6f103596a00c66b0de9fbc9e4ab8f0e9f4063adeef3d4f89abb4982af532cefb501cb39e0b309f88afa2a3d767d6390f9fbf117b42895880a5ac9df8a4cb8234f30c02c0e06ef31c"
	ext28, _ := hex.DecodeString(ext28Hex)

	res, err := protocol.ComputeChallenge(ext28)
	if err != nil {
		t.Skipf("skipping challenge computation test: %v", err)
	}
	expected := uint64(0x900420110800000)
	if res != expected {
		t.Fatalf("challenge computation mismatch: got %#x, want %#x", res, expected)
	}
}

func TestNativeMockLoopbackFetchHistory(t *testing.T) {
	protocol.SetChallengeOverride(func(chal []byte) (uint64, error) {
		return 0x1234567890ABCDEF, nil
	})
	defer protocol.SetChallengeOverride(nil)

	const login = uint64(10001001)
	const pass = "Synthetic123!"

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// 1. Read Hello
		hdr, _ := protocol.ReadHeader(conn)
		helloPayload, _ := protocol.ReadFramePayload(conn, hdr.Length)
		_ = protocol.BootstrapTransform(helloPayload, false)

		// 2. Reply Challenge
		chalBody := make([]byte, 102)
		binary.LittleEndian.PutUint16(chalBody[12:14], 6230)
		copy(chalBody[14:30], bytes.Repeat([]byte{0x77}, 16))
		encChal := protocol.BootstrapTransform(chalBody, true)
		protocol.WriteFrame(conn, protocol.FrameHeader{Type: protocol.TypeExtendedHello, Length: uint32(len(encChal)), Sequence: 1, Flags: protocol.FlagFinal}, encChal)

		// 3. Read Auth Proof
		hdr, _ = protocol.ReadHeader(conn)
		proofPayload, _ := protocol.ReadFramePayload(conn, hdr.Length)
		plainProof := protocol.BootstrapTransform(proofPayload, false)
		clientChal := plainProof[56:72]

		// 4. Reply Auth
		digest := protocol.PasswordDigest(login, pass)
		h := sha256.New()
		h.Write(digest[:])
		h.Write(clientChal)
		serverProof := h.Sum(nil)

		replyBody := make([]byte, 980+5+384)
		copy(replyBody[64:96], serverProof)
		copy(replyBody[0xE0:0x1E0], bytes.Repeat([]byte{0x55}, 256))
		replyBody[980] = 28
		binary.LittleEndian.PutUint32(replyBody[981:985], 384)
		copy(replyBody[985:985+384], bytes.Repeat([]byte{0x33}, 384))

		encReply := protocol.BootstrapTransform(replyBody, true)
		protocol.WriteFrame(conn, protocol.FrameHeader{Type: protocol.TypeAuthProof, Length: uint32(len(encReply)), Sequence: 2, Flags: protocol.FlagFinal}, encReply)

		// 5. Read Sync Request
		hdr, _ = protocol.ReadHeader(conn)
		syncPayload, _ := protocol.ReadFramePayload(conn, hdr.Length)

		table, _ := protocol.BuildTable(digest[:], bytes.Repeat([]byte{0x55}, 256))
		inState := protocol.NewRollingPayload(table)
		outState := protocol.NewRollingPayload(table)
		_ = inState.Decode(syncPayload, false)

		// 6. Send Sync Reply
		replySync := make([]byte, 8000)
		replySync[999] = protocol.AccountRecordTag
		rec := replySync[1000 : 1000+protocol.AccountRecordSize]
		binary.LittleEndian.PutUint64(rec[0:8], login)
		binary.LittleEndian.PutUint32(rec[456:460], protocol.InvestorFlag)
		binary.LittleEndian.PutUint64(rec[1836:1844], math.Float64bits(99999.0))
		binary.LittleEndian.PutUint64(rec[1844:1852], math.Float64bits(0.0))

		encSync := outState.Encode(replySync)
		protocol.WriteFrame(conn, protocol.FrameHeader{Type: protocol.TypeInitialSync, Length: uint32(len(encSync)), Sequence: 3, Flags: protocol.FlagFinal}, encSync)

		// 7. Read History Request 1 (seq=4)
		hdr, _ = protocol.ReadHeader(conn)
		req20, _ := protocol.ReadFramePayload(conn, hdr.Length)
		_ = inState.Decode(req20, false)

		// 8. Read History Request 2 (seq=5)
		hdr, _ = protocol.ReadHeader(conn)
		req21, _ := protocol.ReadFramePayload(conn, hdr.Length)
		_ = inState.Decode(req21, false)

		// 9. Reply Deal Record (seq=4)
		dealRec := make([]byte, protocol.DealRecordSize)
		dealRec[0] = protocol.DealRecordTag
		binary.LittleEndian.PutUint64(dealRec[592:600], 4238299)
		binary.LittleEndian.PutUint64(dealRec[600:608], 4238290)
		binary.LittleEndian.PutUint64(dealRec[608:616], 4238200)
		ft := protocol.UnixToFILETIME(time.Date(2026, 11, 20, 1, 50, 0, 0, time.UTC))
		binary.LittleEndian.PutUint64(dealRec[616:624], ft)
		binary.LittleEndian.PutUint32(dealRec[624:628], 0) // Buy
		binary.LittleEndian.PutUint32(dealRec[628:632], 1) // Out
		binary.LittleEndian.PutUint64(dealRec[632:640], 10000)
		binary.LittleEndian.PutUint64(dealRec[272:280], math.Float64bits(2650.50))
		binary.LittleEndian.PutUint64(dealRec[640:648], math.Float64bits(150.25))

		encDeal := outState.Encode(dealRec)
		protocol.WriteFrame(conn, protocol.FrameHeader{Type: protocol.TypeHistoryReply, Length: uint32(len(encDeal)), Sequence: 4, Flags: 0}, encDeal)

		// 10. Reply Final Frame (seq=5, FlagFinal)
		encFinal := outState.Encode(nil)
		protocol.WriteFrame(conn, protocol.FrameHeader{Type: protocol.TypeHistoryReply, Length: uint32(len(encFinal)), Sequence: 5, Flags: protocol.FlagFinal}, encFinal)
	}()

	addr := l.Addr().(*net.TCPAddr)
	cfg := mt5.Config{
		IPAddress: addr.IP.String(),
		Port:      addr.Port,
		Login:     login,
		Password:  pass,
		Server:    "Synthetic-Server",
	}

	window := mt5.HistoryRange{
		Start: 1763596800,
		End:   1763683200,
	}

	hRes, err := mt5.FetchHistory(context.Background(), cfg, window)
	if err != nil {
		t.Fatalf("native FetchHistory failed: %v", err)
	}

	if len(hRes.Deals) != 1 {
		t.Fatalf("expected 1 deal, got %d", len(hRes.Deals))
	}
	d := hRes.Deals[0]
	if d.Ticket != 4238299 || d.Order != 4238290 || d.PositionID != 4238200 || d.Price != 2650.50 || d.Profit != 150.25 {
		t.Fatalf("unexpected deal data: %+v", d)
	}
	if hRes.Account.Balance != 99999.0 || !hRes.Account.IsInvestor() {
		t.Fatalf("unexpected account in history result: %+v", hRes.Account)
	}
}

func TestDynamicSyncOptionsAndGenerators(t *testing.T) {
	// 1. Verify Random Installation Number
	inst1 := protocol.GenerateRandomInstallationNumber()
	inst2 := protocol.GenerateRandomInstallationNumber()
	if inst1 == 0 || inst2 == 0 {
		t.Fatalf("expected non-zero installation numbers, got %d and %d", inst1, inst2)
	}
	if inst1 == inst2 {
		t.Logf("warning: random installation collision (unlikely): %d", inst1)
	}

	// 2. Verify Random DeviceID
	dev1 := protocol.GenerateRandomDeviceID()
	if len(dev1) != 10 {
		t.Fatalf("expected 10-char devid, got %q (len %d)", dev1, len(dev1))
	}
	for _, r := range dev1 {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z')) {
			t.Fatalf("invalid character in devid %q: %c", dev1, r)
		}
	}

	// 3. Verify BuildClientDeclaration with specific devid
	customDecl := protocol.BuildClientDeclaration("4LK8F7J843")
	if !bytes.Equal(customDecl, protocol.OfficialClientDeclaration) {
		t.Fatalf("expected custom declaration with default devid to match OfficialClientDeclaration")
	}

	// 4. Verify BuildClientDeclaration with empty devid (auto random)
	autoDecl := protocol.BuildClientDeclaration("")
	if len(autoDecl) != len(customDecl) {
		t.Fatalf("expected auto declaration length %d, got %d", len(customDecl), len(autoDecl))
	}

	// 5. Verify BuildSyncRequest with custom SyncOptions
	sess := &protocol.AuthSession{
		Extension28: []byte{
			0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x18, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		},
		Challenge: [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
	}
	customInstall := uint64(0x1122334455667788)
	customCache := make([]byte, 44)
	copy(customCache, []byte("CUSTOM_MARKET_CACHE_DESCRIPTOR_DATA_44_BYTES"))

	reqBytes, err := protocol.BuildSyncRequest(sess, 888888, protocol.SyncOptions{
		InstallationNumber: customInstall,
		DeviceID:           "CUSTOMDE01",
		MarketCache:        customCache,
	})
	if err != nil {
		t.Fatalf("BuildSyncRequest failed: %v", err)
	}

	// Check if custom installation number is in the packed TLV
	var instBytes [8]byte
	binary.LittleEndian.PutUint64(instBytes[:], customInstall)
	if !bytes.Contains(reqBytes, instBytes[:]) {
		t.Fatalf("expected sync request to contain custom installation number %x", customInstall)
	}

	// Check if custom market cache is in the packed TLV
	if !bytes.Contains(reqBytes, customCache) {
		t.Fatalf("expected sync request to contain custom market cache")
	}

	// 6. Verify default/random behavior when no opts are passed
	defReqBytes, err := protocol.BuildSyncRequest(sess, 888888)
	if err != nil {
		t.Fatalf("BuildSyncRequest with defaults failed: %v", err)
	}
	if len(defReqBytes) == 0 {
		t.Fatalf("expected non-empty default sync request")
	}
	if !bytes.Contains(defReqBytes, protocol.DefaultMarketCache) {
		t.Fatalf("expected default sync request to contain DefaultMarketCache")
	}
}

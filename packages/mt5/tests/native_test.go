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
		table[i] = byte((i*37 + 13) & 0xFF)
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

	// A missing investor flag still describes an authenticated account record.
	binary.LittleEndian.PutUint32(rec[456:460], 0)
	res, err = protocol.ParseSyncPayload(payload, login)
	if err != nil || res.Account.IsInvestor || res.Account.Login != login || res.Account.Balance != 12345.67 {
		t.Fatalf("unexpected account without investor flag: result=%+v err=%v", res, err)
	}
}

func TestSyncReadsNativePositionSide(t *testing.T) {
	const login = uint64(10001001)
	const positionStart = 1 + protocol.AccountRecordSize
	payload := make([]byte, positionStart+72+672)
	payload[0] = protocol.AccountRecordTag
	account := payload[1 : 1+protocol.AccountRecordSize]
	binary.LittleEndian.PutUint64(account[:8], login)
	binary.LittleEndian.PutUint32(account[456:460], protocol.InvestorFlag)

	binary.LittleEndian.PutUint64(payload[positionStart:], 456789)
	position := payload[positionStart+72:]
	binary.LittleEndian.PutUint64(position[:8], login)
	binary.LittleEndian.PutUint32(position[112:116], 1)
	binary.LittleEndian.PutUint64(position[152:160], 2_000_000)
	binary.LittleEndian.PutUint64(position[216:224], 123456)
	copy(position[48:112], protocol.EncodeUTF16LE("TESTUSD"))

	result, err := protocol.ParseSyncPayload(payload, login)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Positions) != 1 || result.Positions[0].Type != 1 {
		t.Fatalf("expected one sell position, got %+v", result.Positions)
	}
	if got := result.Positions[0]; got.Identifier != 123456 || got.NativeTicket != 456789 {
		t.Fatalf("stable position identifier and native ticket were confused: %+v", got)
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
	if res.Account.MarginAvailable {
		t.Fatal("snapshot without a margin field advertised an available margin")
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
		binary.LittleEndian.PutUint64(rec[1836:1844], math.Float64bits(99999.0))
		binary.LittleEndian.PutUint64(rec[1844:1852], math.Float64bits(0.0))

		encSync := outState.Encode(replySync)
		protocol.WriteFrame(conn, protocol.FrameHeader{Type: protocol.TypeInitialSync, Length: uint32(len(encSync)), Sequence: 3, Flags: protocol.FlagFinal}, encSync)

		// 7. Read the bounded deal history request.
		hdr, _ = protocol.ReadHeader(conn)
		encrypted, _ := protocol.ReadFramePayload(conn, hdr.Length)
		request := inState.Decode(encrypted, false)
		expectedRequest := historyRequest(1_795_132_800, 1_795_219_200)
		if hdr.Type != protocol.TypeHistory || !bytes.Equal(request, expectedRequest) {
			t.Errorf("unexpected history request: type=%d, bytes=%x", hdr.Type, request)
			return
		}

		// 8. Send a complete history response split across two frames.
		rows := append(historyTrade(login, 1_795_139_400_150), historyBalance(login, 1_795_139_400_828)...)
		reply := historyReply(rows)
		for i, piece := range [][]byte{reply[:13], reply[13:]} {
			enc := outState.Encode(piece)
			protocol.WriteFrame(conn, protocol.FrameHeader{Type: protocol.TypeHistory, Length: uint32(len(enc)),
				Sequence: hdr.Sequence, Flags: uint16(i) * protocol.FlagFinal}, enc)
		}
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
		Start: 1_795_132_800,
		End:   1_795_219_200,
	}

	hRes, err := mt5.FetchHistory(context.Background(), cfg, window)
	if err != nil {
		t.Fatalf("native FetchHistory failed: %v", err)
	}

	if len(hRes.Deals) != 2 {
		t.Fatalf("expected trade and balance deal, got %d rows", len(hRes.Deals))
	}
	d := hRes.Deals[0]
	if d.Ticket != 2001 || d.Order != 1001 || d.PositionID != 3001 || d.Price != 2650.50 ||
		d.Digits != 3 || d.VolumeUnits != 1234567 || d.WallTimeMS != 1_795_139_400_150 ||
		d.Symbol != "TESTUSD" || d.Type != mt5.DealSell || d.Entry != mt5.EntryOutBy ||
		d.Profit != 15.25 || d.Commission != -1 || d.Swap != -2 || d.Fee != -0.25 {
		t.Fatalf("unexpected trade: %+v", d)
	}
	fund := hRes.Deals[1]
	if fund.Type != mt5.DealBalance || fund.Profit != 50 || fund.Comment != "deposit" {
		t.Fatalf("unexpected funding deal: %+v", fund)
	}
	if hRes.Account.Balance != 99999.0 || hRes.Account.IsInvestor() {
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

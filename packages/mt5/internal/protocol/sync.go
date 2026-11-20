package protocol

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf16"
)

const (
	TypeInitialSync = 12 // 0x0C

	AccountRecordTag  = 0x25
	AccountRecordSize = 2996
	InvestorFlag      = 0x08

	DefaultDeviceID = "4LK8F7J843"
)

// SyncOptions specifies optional parameters for account synchronization.
// Unspecified or zero values default to secure random values or verified baselines.
type SyncOptions struct {
	InstallationNumber uint64 // Client installation ID (randomly generated if 0)
	DeviceID           string // 10-char alphanumeric device ID (randomly generated if empty)
	ClientDeclaration  []byte // Raw UTF-16LE client declaration (built from DeviceID if nil)
	MarketCache        []byte // Baseline market catalog descriptor (defaults to DefaultMarketCache if nil)
}

// EncodeUTF16LE converts a string to little-endian UTF-16 byte slice.
func EncodeUTF16LE(s string) []byte {
	u16 := utf16.Encode([]rune(s))
	buf := make([]byte, len(u16)*2)
	for i, v := range u16 {
		binary.LittleEndian.PutUint16(buf[i*2:(i+1)*2], v)
	}
	return buf
}

// GenerateRandomInstallationNumber generates a non-zero 64-bit random installation ID.
func GenerateRandomInstallationNumber() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0x4664982c15aaab4d
	}
	v := binary.LittleEndian.Uint64(b[:])
	if v == 0 {
		return 0x4664982c15aaab4d
	}
	return v
}

// GenerateRandomDeviceID generates a 10-character alphanumeric hardware identifier.
func GenerateRandomDeviceID() string {
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "4LK8F7J843"
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b[:])
}

// BuildClientDeclaration formats standard MT5 client declaration with dynamic devid in UTF-16LE.
func BuildClientDeclaration(devID string) []byte {
	if devID == "" {
		devID = DefaultDeviceID
	}
	s := fmt.Sprintf("package=9PN8D6Q4E6.net.metaquotes.MetaTrader5Terminal\tos_ver=iPadOS 27.2\tdevid=%s\tfile=MetaTrader5Terminal\tversion=6300\t\x00", devID)
	return EncodeUTF16LE(s)
}

var (
	// DefaultMarketCache represents the verified 44-byte baseline market catalog cache descriptor.
	// It signals to the MT5 server that basic symbol dictionaries are cached locally,
	// avoiding multi-megabyte full catalog downloads during read-only synchronization.
	DefaultMarketCache = []byte{
		0xe6, 0xa3, 0x45, 0x67, 0xae, 0x74, 0xdd, 0x01, 0x30, 0x00, 0x00, 0x00, 0x08, 0xf8, 0x37, 0x42,
		0x0d, 0xce, 0xc7, 0x1b, 0x01, 0x64, 0xcb, 0x4e, 0x2a, 0x57, 0x0d, 0x04, 0x25, 0x9d, 0xdc, 0x84,
		0x90, 0xa5, 0x45, 0xed, 0xf0, 0xbb, 0xd2, 0xfd, 0x2e, 0x95, 0x99, 0x94,
	}

	// Backward compatibility aliases
	OfficialMarketCache               = DefaultMarketCache
	OfficialInstallationNumber uint64 = 0x4664982c15aaab4d
	OfficialClientDeclaration         = BuildClientDeclaration("4LK8F7J843")
)

type AccountSnapshot struct {
	Login      uint64
	Balance    float64
	Credit     float64
	Leverage   uint32
	Currency   string
	IsInvestor bool
	Name       string
}

type PositionSnapshot struct {
	Ticket       uint64
	Symbol       string
	Type         int // 0 buy, 1 sell
	Lots         float64
	OpenPrice    float64
	CurrentPrice float64
	SL           float64
	TP           float64
	Profit       float64
	Swap         float64
	OpenTime     uint32
	Comment      string
}

type OrderSnapshot struct {
	Ticket   uint64
	Symbol   string
	Type     int
	Lots     float64
	Price    float64
	SL       float64
	TP       float64
	OpenTime uint32
	Comment  string
}

type SyncResult struct {
	Account   AccountSnapshot
	Positions []PositionSnapshot
	Orders    []OrderSnapshot
	RawData   []byte
}

func BuildSyncRequest(session *AuthSession, login uint64, opts ...SyncOptions) ([]byte, error) {
	if len(session.Extension28) == 0 {
		return nil, errors.New("missing extension 28 in session")
	}

	extRes, err := ComputeChallenge(session.Extension28)
	if err != nil {
		return nil, fmt.Errorf("failed computing extension 28 confirmation: %w", err)
	}

	chalPrefix := binary.LittleEndian.Uint64(session.Challenge[:8])
	confirmation := extRes ^ chalPrefix ^ login ^ uint64(ClientBuild)

	emptyMD5 := md5.Sum(nil)
	var tag28 [28]byte
	copy(tag28[12:], emptyMD5[:])

	var opt SyncOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	installNum := opt.InstallationNumber
	if installNum == 0 {
		installNum = GenerateRandomInstallationNumber()
	}

	clientDecl := opt.ClientDeclaration
	if len(clientDecl) == 0 {
		clientDecl = BuildClientDeclaration(opt.DeviceID)
	}

	marketCache := opt.MarketCache
	if len(marketCache) == 0 {
		marketCache = DefaultMarketCache
	}

	fields := []struct {
		tag uint8
		val []byte
	}{
		{0x22, packUint64(0x3FC)},
		{0x6C, packUint64(installNum)},
		{0x16, []byte{0x01, 0, 0, 0, 0, 0, 0, 0, 0}},
		{0x18, make([]byte, 8)},
		{0x84, nil},
		{0x7F, clientDecl},
		{0x07, marketCache},
		{0x28, tag28[:]},
		{0x11, make([]byte, 16)},
		{0x58, packUint64(confirmation)},
	}

	var buf bytes.Buffer
	for _, f := range fields {
		var hdr [5]byte
		hdr[0] = f.tag
		binary.LittleEndian.PutUint32(hdr[1:5], uint32(len(f.val)))
		buf.Write(hdr[:])
		if len(f.val) > 0 {
			buf.Write(f.val)
		}
	}
	return buf.Bytes(), nil
}

func PerformSync(r io.Reader, w io.Writer, session *AuthSession, login uint64, opts ...SyncOptions) (*SyncResult, error) {
	reqBody, err := BuildSyncRequest(session, login, opts...)
	if err != nil {
		return nil, err
	}

	outState := session.OutState
	if outState == nil {
		outState = NewRollingPayload(session.Table)
	}
	inState := session.InState
	if inState == nil {
		inState = NewRollingPayload(session.Table)
	}

	encBody := outState.Encode(reqBody)
	if err := WriteFrame(w, FrameHeader{Type: TypeInitialSync, Length: uint32(len(encBody)), Sequence: 3, Flags: FlagFinal}, encBody); err != nil {
		return nil, err
	}

	var chunks [][]byte
	var totalBytes int

	for {
		hdr, err := ReadHeader(r)
		if err != nil {
			return nil, err
		}
		rawPayload, err := ReadFramePayload(r, hdr.Length)
		if err != nil {
			return nil, err
		}

		compressed := (hdr.Flags & FlagCompressed) != 0
		decPayload := inState.Decode(rawPayload, compressed)

		if hdr.Type != TypeInitialSync {
			continue
		}

		piece := decPayload
		if compressed {
			piece, err = DecompressLZO(decPayload)
			if err != nil {
				return nil, fmt.Errorf("decompression failed: %w", err)
			}
		}

		chunks = append(chunks, piece)
		totalBytes += len(piece)
		if totalBytes > MaxFrameSize {
			return nil, ErrFrameTooLarge
		}

		if (hdr.Flags & FlagFinal) != 0 {
			break
		}
	}

	full := bytes.Join(chunks, nil)
	return ParseSyncPayload(full, login)
}

func ParseSyncPayload(payload []byte, login uint64) (*SyncResult, error) {
	var loginBytes [8]byte
	binary.LittleEndian.PutUint64(loginBytes[:], login)

	off := bytes.Index(payload, loginBytes[:])
	if off < 1 || off+AccountRecordSize > len(payload) {
		return nil, errors.New("mt5: account record not found in synchronization payload")
	}

	if payload[off-1] != AccountRecordTag {
		return nil, errors.New("mt5: invalid account section tag")
	}

	record := payload[off : off+AccountRecordSize]

	flags := binary.LittleEndian.Uint32(record[456:460])
	if (flags & InvestorFlag) == 0 {
		return nil, ErrInvestorDenied
	}

	balance := math.Float64frombits(binary.LittleEndian.Uint64(record[1836:1844]))
	credit := math.Float64frombits(binary.LittleEndian.Uint64(record[1844:1852]))
	var leverage uint32
	if value := int32(binary.LittleEndian.Uint32(record[2020:2024])); value > 0 {
		leverage = uint32(value)
	}

	name := decodeUTF16String(record[24:152])
	currency := decodeUTF16String(record[336:368])
	if currency == "" {
		currency = "USD"
	}

	res := &SyncResult{
		Account: AccountSnapshot{
			Login:      login,
			Balance:    balance,
			Credit:     credit,
			Leverage:   leverage,
			Currency:   currency,
			IsInvestor: true,
			Name:       name,
		},
		RawData: payload,
	}

	// Parse positions (Tag 0x20) and orders (Tag 0x1F) if present
	parseEmbeddedRecords(payload, res)
	return res, nil
}

func parseEmbeddedRecords(payload []byte, res *SyncResult) {
	loginBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(loginBytes, res.Account.Login)

	seenTickets := make(map[uint64]bool)

	// 1. Scan for standard MT5 native 672-byte position/order records
	const rec672 = 672
	for i := 0; i+rec672 <= len(payload); i++ {
		if bytes.Equal(payload[i:i+8], loginBytes) {
			rec := payload[i : i+rec672]
			symbol := decodeUTF16String(rec[48:112])
			ticket := binary.LittleEndian.Uint64(rec[216:224])
			if ticket == 0 {
				ticket = binary.LittleEndian.Uint64(rec[208:216])
			}
			if len(symbol) >= 2 && ticket > 0 && !seenTickets[ticket] {
				seenTickets[ticket] = true
				openTime := binary.LittleEndian.Uint32(rec[32:36])
				side := int(binary.LittleEndian.Uint32(rec[16:20]))
				openPrice := math.Float64frombits(binary.LittleEndian.Uint64(rec[120:128]))
				currPrice := math.Float64frombits(binary.LittleEndian.Uint64(rec[128:136]))
				sl := math.Float64frombits(binary.LittleEndian.Uint64(rec[136:144]))
				tp := math.Float64frombits(binary.LittleEndian.Uint64(rec[144:152]))
				volumeUnits := binary.LittleEndian.Uint64(rec[152:160])
				lots := float64(volumeUnits) / 100_000_000.0
				if lots <= 0 {
					lots = math.Float64frombits(binary.LittleEndian.Uint64(rec[288:296]))
				}
				profit := math.Float64frombits(binary.LittleEndian.Uint64(rec[160:168]))
				comment := decodeUTF16String(rec[224:])

				if side <= 1 {
					res.Positions = append(res.Positions, PositionSnapshot{
						Ticket:       ticket,
						Symbol:       symbol,
						Type:         side,
						Lots:         lots,
						OpenPrice:    openPrice,
						CurrentPrice: currPrice,
						SL:           sl,
						TP:           tp,
						Profit:       profit,
						OpenTime:     openTime,
						Comment:      comment,
					})
				} else if side <= 7 {
					res.Orders = append(res.Orders, OrderSnapshot{
						Ticket:   ticket,
						Symbol:   symbol,
						Type:     side,
						Lots:     lots,
						Price:    openPrice,
						SL:       sl,
						TP:       tp,
						OpenTime: openTime,
						Comment:  comment,
					})
				}
				i += rec672 - 1
			}
		}
	}

	// 2. Fallback to 793-byte tagged records if no 672-byte records found
	if len(res.Positions) == 0 && len(res.Orders) == 0 {
		recSize := 793
		for i := 0; i+recSize <= len(payload); i++ {
			tag := payload[i]
			if (tag == 0x1F || tag == 0x20) && i+5 <= len(payload) {
				count := binary.LittleEndian.Uint32(payload[i+1 : i+5])
				if count == 1 && i+recSize <= len(payload) {
					rec := payload[i : i+recSize]
					symbol := decodeUTF16String(rec[240:272])
					if len(symbol) >= 2 {
						ticket := binary.LittleEndian.Uint64(rec[4:12])
						if !seenTickets[ticket] {
							seenTickets[ticket] = true
							if tag == 0x20 {
								res.Positions = append(res.Positions, PositionSnapshot{
									Ticket: ticket,
									Symbol: symbol,
								})
							} else {
								res.Orders = append(res.Orders, OrderSnapshot{
									Ticket: ticket,
									Symbol: symbol,
								})
							}
							i += recSize - 1
						}
					}
				}
			}
		}
	}
}

func packUint64(v uint64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	return buf[:]
}

func decodeUTF16String(b []byte) string {
	var words []uint16
	for i := 0; i+2 <= len(b); i += 2 {
		w := binary.LittleEndian.Uint16(b[i : i+2])
		if w == 0 {
			break
		}
		words = append(words, w)
	}
	return string(utf16.Decode(words))
}

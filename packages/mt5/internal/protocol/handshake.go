package protocol

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	TypeExtendedHello = 251 // 0xFB
	TypeAuthProof     = 252 // 0xFC

	ClientBuild   uint16 = 6300
	ClientProto   uint16 = 20813
	ClientMode    uint8  = 4
	MinimumBuild  uint16 = 5916
)

type AuthSession struct {
	ServerBuild uint16
	Challenge   [16]byte
	ServerProof [32]byte
	TableSeed   [256]byte
	Extension28 []byte
	Digest      [32]byte
	Table       [256]byte
	ClientChal  [16]byte
	OutState    *RollingPayload
	InState     *RollingPayload
}

func HandshakeAndAuthenticate(r io.Reader, w io.Writer, login uint64, password string) (*AuthSession, error) {
	// 1. Build & Send Extended Hello (128 bytes)
	helloBody := make([]byte, 128)
	var nonce [1]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	// Sign-extend the first byte to 64-bit integer
	binary.LittleEndian.PutUint64(helloBody[0:8], uint64(int64(int8(nonce[0]))))
	helloBody[8] = ClientMode
	binary.LittleEndian.PutUint16(helloBody[10:12], ClientBuild)
	binary.LittleEndian.PutUint16(helloBody[12:14], ClientProto)
	binary.LittleEndian.PutUint64(helloBody[16:24], login)

	// Instance seed
	var instSeed [15]byte
	if _, err := rand.Read(instSeed[:]); err != nil {
		return nil, err
	}
	var instSum byte
	for _, b := range instSeed {
		instSum += b
	}
	helloBody[32] = instSum
	copy(helloBody[33:48], instSeed[:])

	// Feature tail bytes observed in official terminal
	copy(helloBody[48:], []byte{
		0x00, 0x00, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x88, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08, 0x88, 0xc4, 0x04, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xa8, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf4, 0x00, 0x2f, 0xc1, 0x3c, 0x46, 0x12, 0x17,
		0xb0, 0x1f, 0x5a, 0x96, 0x7b, 0x00, 0x00, 0x00, 0x34, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	})

	encHello := BootstrapTransform(helloBody, true)
	if err := WriteFrame(w, FrameHeader{Type: TypeExtendedHello, Length: uint32(len(encHello)), Sequence: 1, Flags: FlagFinal}, encHello); err != nil {
		return nil, err
	}

	// 2. Read Server Challenge (102 bytes)
	hdr, err := ReadHeader(r)
	if err != nil {
		return nil, err
	}
	if hdr.Type != TypeExtendedHello || hdr.Sequence != 1 {
		return nil, ErrProtocolMismatch
	}
	chalPayload, err := ReadFramePayload(r, hdr.Length)
	if err != nil {
		return nil, err
	}
	plainChal := BootstrapTransform(chalPayload, false)
	if len(plainChal) < 30 {
		return nil, ErrTruncatedFrame
	}
	serverBuild := binary.LittleEndian.Uint16(plainChal[12:14])
	if serverBuild < MinimumBuild {
		return nil, fmt.Errorf("unsupported server build %d", serverBuild)
	}
	var serverChal [16]byte
	copy(serverChal[:], plainChal[14:30])

	// 3. Build & Send Auth Proof (268 bytes)
	digest := PasswordDigest(login, password)
	legacyDigest := LegacyAuthDigest(login, password)

	legacyProof, err := LegacyProof(legacyDigest, serverChal[:])
	if err != nil {
		return nil, err
	}

	hSha := sha256.New()
	hSha.Write(digest[:])
	hSha.Write(serverChal[:])
	var shaProof [32]byte
	copy(shaProof[:], hSha.Sum(nil))

	var clientChal [16]byte
	if _, err := rand.Read(clientChal[:]); err != nil {
		return nil, err
	}

	proofBody := make([]byte, 268)
	if _, err := rand.Read(proofBody); err != nil {
		return nil, err
	}
	copy(proofBody[8:24], legacyProof[:])
	copy(proofBody[24:56], shaProof[:])
	copy(proofBody[56:72], clientChal[:])
	// Clear reserved regions
	copy(proofBody[0x50:0x60], make([]byte, 16))
	copy(proofBody[0x68:0x70], make([]byte, 8)) // resume state
	copy(proofBody[0x80:0x84], make([]byte, 4))

	encProof := BootstrapTransform(proofBody, true)
	if err := WriteFrame(w, FrameHeader{Type: TypeAuthProof, Length: uint32(len(encProof)), Sequence: 2, Flags: FlagFinal}, encProof); err != nil {
		return nil, err
	}

	// 4. Read Server Auth Reply (>= 980 bytes)
	hdr, err = ReadHeader(r)
	if err != nil {
		return nil, err
	}
	if hdr.Type != TypeAuthProof || hdr.Sequence != 2 {
		return nil, ErrProtocolMismatch
	}
	replyPayload, err := ReadFramePayload(r, hdr.Length)
	if err != nil {
		return nil, err
	}
	plainReply := BootstrapTransform(replyPayload, false)
	if len(plainReply) < 980 {
		return nil, ErrTruncatedFrame
	}

	// Verify server proof: SHA-256(digest + client_challenge)
	hExpected := sha256.New()
	hExpected.Write(digest[:])
	hExpected.Write(clientChal[:])
	expectedProof := hExpected.Sum(nil)
	actualProof := plainReply[64:96]
	if !bytes.Equal(expectedProof, actualProof) {
		return nil, ErrAuthFailed
	}

	var tableSeed [256]byte
	copy(tableSeed[:], plainReply[0xE0:0x1E0])
	table, err := BuildTable(digest[:], tableSeed[:])
	if err != nil {
		return nil, err
	}

	// Extract Extension 28
	var ext28 []byte
	extOffset := 980
	for extOffset+5 <= len(plainReply) {
		tag := plainReply[extOffset]
		length := int(binary.LittleEndian.Uint32(plainReply[extOffset+1 : extOffset+5]))
		extOffset += 5
		if extOffset+length > len(plainReply) {
			break
		}
		if tag == 28 {
			ext28 = make([]byte, length)
			copy(ext28, plainReply[extOffset:extOffset+length])
			break
		}
		extOffset += length
	}

	session := &AuthSession{
		ServerBuild: serverBuild,
		Challenge:   serverChal,
		TableSeed:   tableSeed,
		Extension28: ext28,
		Digest:      digest,
		Table:       table,
		ClientChal:  clientChal,
		OutState:    NewRollingPayload(table),
		InState:     NewRollingPayload(table),
	}
	copy(session.ServerProof[:], actualProof)
	return session, nil
}

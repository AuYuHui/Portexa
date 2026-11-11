package protocol

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
)

func DefaultDeviceInfo() ([]byte, error) {
	value := make([]byte, 68)
	if _, err := io.ReadFull(rand.Reader, value[1:16]); err != nil {
		return nil, err
	}
	for _, v := range value[1:16] {
		value[0] += v
	}
	// Protocol profile of the analyzed mobile build. Optional message ID,
	// campaign, and MetaQuotes user number remain absent (zero).
	imageSize := binary.LittleEndian.AppendUint32(nil, 2384)
	copy(value[20:24], FeedbackEncode(imageSize, InitialTable))
	copy(value[24:28], FeedbackEncode(make([]byte, 4), InitialTable))
	return value, nil
}

func AuthOuterKey(login uint32, field uint32, key [16]byte) ([16]byte, LegacyRandom) {
	random := LegacyRandom(login + field)
	var nonce [64]byte
	for i := range nonce {
		nonce[i] = byte(random.Next())
	}
	value := append([]byte{10}, []byte("MTComplex\x00")...)
	value = binary.LittleEndian.AppendUint16(value, ClientBuild)
	value = append(value, key[:]...)
	value = append(value, nonce[:]...)
	value = binary.LittleEndian.AppendUint32(value, login)
	return md5.Sum(value), random
}

func AuthenticationPacket(response InitialResponse, login uint32, device []byte) ([]byte, [16]byte, error) {
	var sessionKey [16]byte
	if len(device) != 68 {
		return nil, sessionKey, errors.New("mobile device profile must contain 68 bytes")
	}
	var checksum byte
	for _, v := range device[1:16] {
		checksum += v
	}
	if checksum != device[0] {
		return nil, sessionKey, errors.New("invalid installation identifier checksum")
	}
	var token uint64
	foundProgram := false
	for _, r := range response.Records {
		if r.Kind == 9 {
			var err error
			token, err = EvaluateFeatures(r.Data)
			if err != nil {
				return nil, sessionKey, err
			}
			foundProgram = true
		}
	}
	if !foundProgram {
		return nil, sessionKey, errors.New("server feature program is absent")
	}
	mix := (uint64(login) << 32) | uint64(int64(int32(response.Field)))
	token ^= mix
	outerKey, random := AuthOuterKey(login, response.Field, response.Key)
	working := outerKey
	count := 5 + int(random.Next()%4)
	kinds := []uint16{0, 1, 10, 3, 4, 3, 4, 3, 4}
	kinds = kinds[:count]
	for range 128 {
		a, b := int(random.Next()%uint32(count)), int(random.Next()%uint32(count))
		kinds[a], kinds[b] = kinds[b], kinds[a]
	}
	var inner []byte
	for _, kind := range kinds {
		var data []byte
		var mode, headerRandom uint16
		switch kind {
		case 0:
			data, mode = append([]byte{}, device...), 1
			headerRandom = uint16(random.Next())
		case 1:
			message := append([]byte{}, device[:16]...)
			message = binary.LittleEndian.AppendUint32(message, login)
			message = append(message, response.Key[:]...)
			message = append(message, working[:]...)
			first := sha1.Sum(message)
			copy(sessionKey[:], FeedbackEncode(FeedbackEncode(working[:], first[:]), device[:16]))
			message = binary.LittleEndian.AppendUint32(nil, login)
			message = append(message, response.Key[:]...)
			message = binary.LittleEndian.AppendUint32(message, response.Field)
			proof := sha1.Sum(message)
			data = append([]byte{}, proof[:]...)
			headerRandom = uint16(random.Next())
		case 10:
			data = binary.LittleEndian.AppendUint64(nil, token)
			headerRandom = uint16(random.Next())
		case 3, 4:
			data = make([]byte, 8+int(random.Next()%32))
			headerRandom = uint16(random.Next())
			for i := range data {
				data[i] = byte(random.Next())
			}
		}
		wire, err := PackRecord(Record{Kind: kind, Mode: mode, Data: data}, headerRandom, working)
		if err != nil {
			return nil, sessionKey, err
		}
		inner = append(inner, wire...)
		if kind == 3 {
			copy(working[:], FeedbackEncode(working[:], data))
		}
	}
	outer, err := PackRecord(Record{Kind: 2, Mode: 1, Data: inner}, uint16(random.Next()), outerKey)
	if err != nil {
		return nil, sessionKey, err
	}
	return append([]byte{0x18}, outer...), sessionKey, nil
}

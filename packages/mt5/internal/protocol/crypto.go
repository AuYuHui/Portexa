package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf16"

	"github.com/anchore/go-lzo"
)

var bootstrapMask = [16]byte{
	0x41, 0xb6, 0x7f, 0x58, 0x38, 0x0c, 0xf0, 0x2d,
	0x7b, 0x39, 0x08, 0xfe, 0x21, 0xbb, 0x41, 0x58,
}

func BootstrapTransform(data []byte, outbound bool) []byte {
	res := make([]byte, len(data))
	var previous byte
	for i, val := range data {
		m := val ^ byte(int(previous+bootstrapMask[i%16])&0xFF)
		res[i] = m
		if outbound {
			previous = m
		} else {
			previous = val
		}
	}
	return res
}

func PasswordDigest(login uint64, password string) [32]byte {
	var prefix [8]byte
	binary.LittleEndian.PutUint64(prefix[:], login)

	u16 := utf16.Encode([]rune(password + "MQ"))
	contextBytes := make([]byte, len(u16)*2)
	for i, v := range u16 {
		binary.LittleEndian.PutUint16(contextBytes[i*2:(i+1)*2], v)
	}

	h := sha256.New()
	h.Write(prefix[:])
	h.Write(contextBytes)
	var res [32]byte
	copy(res[:], h.Sum(nil))
	return res
}

func LegacyAuthDigest(login uint64, password string) [16]byte {
	var prefix [8]byte
	binary.LittleEndian.PutUint64(prefix[:], login)

	u16 := utf16.Encode([]rune(password))
	passBytes := make([]byte, len(u16)*2)
	for i, v := range u16 {
		binary.LittleEndian.PutUint16(passBytes[i*2:(i+1)*2], v)
	}
	if len(passBytes) > 32 {
		passBytes = passBytes[:32]
	}

	mqBytes := []byte{'M', 0, 'Q', 0}
	h := md5.New()
	h.Write(prefix[:])
	h.Write(passBytes)
	h.Write(mqBytes)
	var res [16]byte
	copy(res[:], h.Sum(nil))
	return res
}

var md5Rounds = func() [64]uint32 {
	var rounds [64]uint32
	for i := 1; i <= 64; i++ {
		rounds[i-1] = uint32(math.Abs(math.Sin(float64(i))) * 4294967296.0)
	}
	return rounds
}()

var md5Shifts = [4][4]uint{
	{7, 12, 17, 22},
	{5, 9, 14, 20},
	{4, 11, 16, 23},
	{6, 10, 15, 21},
}

func LegacyProof(initialDigest [16]byte, challenge []byte) ([16]byte, error) {
	if len(challenge) != 16 {
		return [16]byte{}, errors.New("expected 16-byte challenge")
	}
	a := binary.LittleEndian.Uint32(initialDigest[0:4])
	b := binary.LittleEndian.Uint32(initialDigest[4:8])
	c := binary.LittleEndian.Uint32(initialDigest[8:12])
	d := binary.LittleEndian.Uint32(initialDigest[12:16])
	origA, origB, origC, origD := a, b, c, d

	var block [64]byte
	copy(block[0:16], challenge)
	block[16] = 0x80
	binary.LittleEndian.PutUint64(block[56:64], uint64(len(challenge)*8))

	var words [16]uint32
	for i := 0; i < 16; i++ {
		words[i] = binary.LittleEndian.Uint32(block[i*4 : (i+1)*4])
	}

	for step := 0; step < 64; step++ {
		group := step / 16
		var mix, index uint32
		switch group {
		case 0:
			mix = (b & c) | (^b & d)
			index = uint32(step)
		case 1:
			mix = (d & b) | (^d & c)
			index = uint32(5*step + 1) % 16
		case 2:
			mix = b ^ c ^ d
			index = uint32(3*step + 5) % 16
		default:
			mix = c ^ (b | ^d)
			index = uint32(7*step) % 16
		}
		shift := md5Shifts[group][step%4]
		val := a + mix + md5Rounds[step] + words[index]
		rotated := (val << shift) | (val >> (32 - shift))
		a, b, c, d = d, b+rotated, b, c
	}

	var res [16]byte
	binary.LittleEndian.PutUint32(res[0:4], origA+a)
	binary.LittleEndian.PutUint32(res[4:8], origB+b)
	binary.LittleEndian.PutUint32(res[8:12], origC+c)
	binary.LittleEndian.PutUint32(res[12:16], origD+d)
	return res, nil
}

func BuildTable(key []byte, seed []byte) ([256]byte, error) {
	if len(seed) != 256 {
		return [256]byte{}, errors.New("expected 256-byte seed")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return [256]byte{}, err
	}
	iv := make([]byte, aes.BlockSize)
	mode := cipher.NewCBCEncrypter(block, iv)
	var table [256]byte
	mode.CryptBlocks(table[:], seed)
	return table, nil
}

type RollingPayload struct {
	table    [256]byte
	offset   int
	previous byte
}

func NewRollingPayload(table [256]byte) *RollingPayload {
	return &RollingPayload{table: table}
}

func (r *RollingPayload) Encode(data []byte) []byte {
	res := make([]byte, len(data))
	prev := r.previous
	for i, val := range data {
		t := r.table[(r.offset+i)&0xFF]
		res[i] = val ^ byte((int(t) + int(prev)) & 0xFF)
		prev = val
	}
	r.advance(len(data), prev)
	return res
}

func (r *RollingPayload) Decode(data []byte, compressed bool) []byte {
	size := len(data)
	res := make([]byte, size)
	copy(res, data)

	split := size
	if compressed && size >= 2 {
		split = (size + 1) / 2
	}

	var backward byte
	for i := size - 1; i >= split; i-- {
		t := r.table[(r.offset+i)&0xFF]
		backward = byte((int(backward^data[i]) - int(t)) & 0xFF)
		res[i-1] = backward
	}

	prev := r.previous
	for i := 0; i < split; i++ {
		t := r.table[(r.offset+i)&0xFF]
		prev = byte((int(t) + int(prev)) & 0xFF) ^ data[i]
		res[i] = prev
	}

	if split < size {
		res[size-1] = 0
		if prev == backward {
			prev = 0
		} else {
			for i := split; i < size; i++ {
				t := r.table[(r.offset+i)&0xFF]
				prevBackward := backward
				backward = res[i]
				term1 := byte((int(t) + int(prevBackward)) & 0xFF)
				term2 := byte((int(t) + int(prev)) & 0xFF)
				prev = term1 ^ term2 ^ backward
				res[i] = prev
			}
		}
	}

	r.advance(size, prev)
	return res
}

func (r *RollingPayload) advance(size int, prev byte) {
	r.offset = (r.offset + size) & 0xFF
	r.previous = prev
}

func DecompressLZO(data []byte) ([]byte, error) {
	if len(data) < 8 {
		return nil, errors.New("compressed frame truncated")
	}
	expandedSize := int(binary.LittleEndian.Uint32(data[0:4]))
	compressedSize := int(binary.LittleEndian.Uint32(data[4:8]))
	if expandedSize <= 0 || expandedSize > MaxFrameSize || compressedSize != len(data)-8 {
		return nil, errors.New("invalid compressed lengths")
	}
	out := make([]byte, expandedSize)
	n, err := lzo.Decompress(data[8:], out)
	if err != nil || n != expandedSize {
		return nil, errors.New("LZO decompression failed")
	}
	return out, nil
}

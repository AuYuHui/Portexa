package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

var ErrPassword = errors.New("密码至少需要16个字符，且不超过1024个字符")
var ErrUnauthorized = errors.New("登录已失效或密码错误")

func ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if !utf8.ValidString(password) || length < 16 || length > 1024 || strings.ContainsRune(password, 0) {
		return ErrPassword
	}
	return nil
}

func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	const memory, passes, lanes = 64 * 1024, 3, 1
	key := argon2.IDKey([]byte(password), salt, passes, memory, lanes, 32)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, memory, passes, lanes,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(password, encoded string) bool {
	if len(password) > 4096 {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory, passes uint32
	var lanes uint8
	if count, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &lanes); err != nil || count != 3 {
		return false
	}
	if memory < 19*1024 || memory > 128*1024 || passes < 1 || passes > 5 || lanes < 1 || lanes > 4 {
		return false
	}
	salt, saltErr := base64.RawStdEncoding.DecodeString(parts[4])
	want, keyErr := base64.RawStdEncoding.DecodeString(parts[5])
	if saltErr != nil || keyErr != nil || len(salt) != 16 || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, passes, memory, lanes, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

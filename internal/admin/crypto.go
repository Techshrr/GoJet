package admin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	passwordAlgorithm    = "pbkdf2-sha256"
	passwordIterations   = 600000
	passwordSaltBytes    = 16
	passwordDerivedBytes = 32
	passwordMaxBytes     = 1024
)

func hashOpaque(value string) [32]byte { return sha256.Sum256([]byte(value)) }

func newOpaque(prefix string, bytes int) (string, error) {
	if prefix == "" || bytes < 16 || bytes > 64 {
		return "", ErrInvalid
	}
	raw := make([]byte, bytes)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func validPassword(password string) bool {
	return password != "" && len(password) <= passwordMaxBytes && utf8.ValidString(password)
}

func hashPassword(password string) (string, error) {
	if !validPassword(password) {
		return "", ErrInvalid
	}
	salt := make([]byte, passwordSaltBytes)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}
	derived := pbkdf2SHA256([]byte(password), salt, passwordIterations, passwordDerivedBytes)
	return strings.Join([]string{passwordAlgorithm, strconv.Itoa(passwordIterations), base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(derived)}, "$"), nil
}

func verifyPassword(encoded, password string) bool {
	if !validPassword(password) {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != passwordAlgorithm {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 100000 || iterations > 1000000 {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(expected) != passwordDerivedBytes {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	hashLength := sha256.Size
	blocks := (keyLength + hashLength - 1) / hashLength
	derived := make([]byte, 0, blocks*hashLength)
	var blockIndex [4]byte
	for block := 1; block <= blocks; block++ {
		binary.BigEndian.PutUint32(blockIndex[:], uint32(block))
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		_, _ = mac.Write(blockIndex[:])
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for iteration := 1; iteration < iterations; iteration++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for i := range t {
				t[i] ^= u[i]
			}
		}
		derived = append(derived, t...)
	}
	return derived[:keyLength]
}

type SecretCipher struct {
	keyID string
	key   [32]byte
}

func NewSecretCipher(keyID string, key []byte) (*SecretCipher, error) {
	keyID = strings.TrimSpace(keyID)
	if keyID == "" || len(keyID) > 64 || strings.ContainsAny(keyID, " \t\r\n") || len(key) != 32 {
		return nil, ErrInvalid
	}
	var fixed [32]byte
	copy(fixed[:], key)
	return &SecretCipher{keyID: keyID, key: fixed}, nil
}
func (c *SecretCipher) KeyID() string {
	if c == nil {
		return ""
	}
	return c.keyID
}
func (c *SecretCipher) Encrypt(plaintext, purpose string) ([]byte, error) {
	if c == nil || plaintext == "" || strings.TrimSpace(purpose) == "" {
		return nil, ErrInvalid
	}
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), []byte(purpose))
	return append(nonce, sealed...), nil
}
func (c *SecretCipher) Decrypt(ciphertext []byte, keyID, purpose string) (string, error) {
	if c == nil || keyID != c.keyID || strings.TrimSpace(purpose) == "" {
		return "", ErrForbidden
	}
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ciphertext) <= gcm.NonceSize() {
		return "", ErrForbidden
	}
	plain, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], []byte(purpose))
	if err != nil {
		return "", fmt.Errorf("admin secret authentication failed: %w", ErrForbidden)
	}
	return string(plain), nil
}

func NewTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}
func VerifyTOTP(secret, code string, now time.Time) bool {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || len(raw) < 16 {
		return false
	}
	counter := now.UTC().Unix() / 30
	for drift := -1; drift <= 1; drift++ {
		if subtle.ConstantTimeCompare([]byte(totpCode(raw, uint64(counter+int64(drift)))), []byte(code)) == 1 {
			return true
		}
	}
	return false
}
func totpCode(secret []byte, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	binaryCode := (uint32(sum[offset])&0x7f)<<24 | (uint32(sum[offset+1])&0xff)<<16 | (uint32(sum[offset+2])&0xff)<<8 | (uint32(sum[offset+3]) & 0xff)
	return fmt.Sprintf("%06d", binaryCode%1000000)
}

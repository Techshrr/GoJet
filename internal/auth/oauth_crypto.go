package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"strings"
)

type OAuthCrypto struct {
	keyID string
	key   [32]byte
}

func NewOAuthCrypto(keyID string, key []byte) (*OAuthCrypto, error) {
	keyID = strings.TrimSpace(keyID)
	if keyID == "" || len(keyID) > 64 || strings.ContainsAny(keyID, " \t\r\n") || len(key) != 32 {
		return nil, ErrInvalid
	}
	var fixed [32]byte
	copy(fixed[:], key)
	return &OAuthCrypto{keyID: keyID, key: fixed}, nil
}

func (c *OAuthCrypto) KeyID() string {
	if c == nil {
		return ""
	}
	return c.keyID
}

func (c *OAuthCrypto) Encrypt(plaintext, purpose string) ([]byte, error) {
	if c == nil || c.keyID == "" || len(plaintext) == 0 || len(plaintext) > 4096 || strings.TrimSpace(purpose) == "" {
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
	out := make([]byte, 0, 1+len(nonce)+len(sealed))
	out = append(out, byte(len(nonce)))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

func (c *OAuthCrypto) Decrypt(ciphertext []byte, keyID, purpose string) (string, error) {
	if c == nil || keyID != c.keyID || len(ciphertext) < 2 || strings.TrimSpace(purpose) == "" {
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
	nonceSize := int(ciphertext[0])
	if nonceSize != gcm.NonceSize() || len(ciphertext) <= 1+nonceSize {
		return "", ErrForbidden
	}
	nonce := ciphertext[1 : 1+nonceSize]
	sealed := ciphertext[1+nonceSize:]
	plain, err := gcm.Open(nil, nonce, sealed, []byte(purpose))
	if err != nil {
		return "", fmt.Errorf("oauth ciphertext authentication failed: %w", ErrForbidden)
	}
	return string(plain), nil
}

package repository

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// AESEncryptor implements SecretEncryptor using AES-256-GCM
type AESEncryptor struct {
	keys [][]byte
}

const maxLegacyEncryptionKeys = 4

// NewAESEncryptor creates a new AES encryptor
func NewAESEncryptor(cfg *config.Config) (service.SecretEncryptor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("totp encryption config is required")
	}
	if len(cfg.Totp.LegacyEncryptionKeys) > maxLegacyEncryptionKeys {
		return nil, fmt.Errorf("totp legacy encryption keys exceed limit %d", maxLegacyEncryptionKeys)
	}
	key, err := decodeAES256Key(cfg.Totp.EncryptionKey, "totp encryption key")
	if err != nil {
		return nil, err
	}
	keys := make([][]byte, 0, 1+len(cfg.Totp.LegacyEncryptionKeys))
	keys = append(keys, key)
	seen := map[string]struct{}{hex.EncodeToString(key): {}}
	for index, candidate := range cfg.Totp.LegacyEncryptionKeys {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		legacy, decodeErr := decodeAES256Key(candidate, fmt.Sprintf("totp legacy encryption key %d", index+1))
		if decodeErr != nil {
			return nil, decodeErr
		}
		fingerprint := hex.EncodeToString(legacy)
		if _, exists := seen[fingerprint]; exists {
			continue
		}
		seen[fingerprint] = struct{}{}
		keys = append(keys, legacy)
	}
	return &AESEncryptor{keys: keys}, nil
}

func decodeAES256Key(value, label string) ([]byte, error) {
	key, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", label, err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s must be 32 bytes (64 hex chars), got %d bytes", label, len(key))
	}
	return key, nil
}

// Encrypt encrypts plaintext using AES-256-GCM
// Output format: base64(nonce + ciphertext + tag)
func (e *AESEncryptor) Encrypt(plaintext string) (string, error) {
	if e == nil || len(e.keys) == 0 {
		return "", fmt.Errorf("encryption key is unavailable")
	}
	block, err := aes.NewCipher(e.keys[0])
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	// Generate a random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	// Encrypt the plaintext
	// Seal appends the ciphertext and tag to the nonce
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Encode as base64
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext using AES-256-GCM
func (e *AESEncryptor) Decrypt(ciphertext string) (string, error) {
	// Decode from base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	if e == nil || len(e.keys) == 0 {
		return "", fmt.Errorf("decrypt: encryption key is unavailable")
	}
	for _, key := range e.keys {
		block, cipherErr := aes.NewCipher(key)
		if cipherErr != nil {
			continue
		}
		gcm, gcmErr := cipher.NewGCM(block)
		if gcmErr != nil {
			continue
		}
		nonceSize := gcm.NonceSize()
		if len(data) < nonceSize {
			return "", fmt.Errorf("ciphertext too short")
		}
		nonce, ciphertextData := data[:nonceSize], data[nonceSize:]
		plaintext, openErr := gcm.Open(nil, nonce, ciphertextData, nil)
		if openErr == nil {
			return string(plaintext), nil
		}
	}
	return "", fmt.Errorf("decrypt: authentication failed")
}

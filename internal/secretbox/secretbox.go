// Package secretbox encrypts small secrets, such as refresh tokens, for storage.
// It uses AES-256-GCM from the Go standard library.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

// KeySize is the required key length in bytes.
const KeySize = 32

// ErrOpen means the ciphertext could not be opened: wrong key, wrong binding or damaged data.
var ErrOpen = errors.New("secretbox: cannot open")

// ParseKey decodes a base64 key. Errors never include the key text.
func ParseKey(s string) ([]byte, error) {
	if s == "" {
		return nil, errors.New("key is empty")
	}
	k, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New("key is not valid base64")
	}
	if len(k) != KeySize {
		return nil, errors.New("key must decode to exactly 32 bytes")
	}
	return k, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, errors.New("secretbox: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plaintext. binding is authenticated but not stored: Open succeeds
// only with the same binding, so a ciphertext cannot be moved to another record.
// The result is nonce followed by ciphertext.
func Seal(key, binding, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, binding), nil
}

// Open decrypts the output of Seal.
func Open(key, binding, sealed []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return nil, ErrOpen
	}
	nonce, ct := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, binding)
	if err != nil {
		return nil, ErrOpen
	}
	return pt, nil
}

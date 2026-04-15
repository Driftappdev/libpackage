package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
)

func EncryptGCM(key, plaintext []byte) (nonce, ciphertext []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return nonce, gcm.Seal(nil, nonce, plaintext, nil), nil
}

func DecryptGCM(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// Encrypt encrypts plaintext with AES-GCM and returns nonce+ciphertext in base64.
func Encrypt(key, plaintext []byte) (string, error) {
	nonce, ciphertext, err := EncryptGCM(key, plaintext)
	if err != nil {
		return "", err
	}
	joined := append(append([]byte{}, nonce...), ciphertext...)
	return base64.RawStdEncoding.EncodeToString(joined), nil
}

// Decrypt decodes base64(nonce+ciphertext) and decrypts with AES-GCM.
func Decrypt(key []byte, encoded string) ([]byte, error) {
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return nil, io.ErrUnexpectedEOF
	}
	return DecryptGCM(key, raw[:nonceSize], raw[nonceSize:])
}

package aes

import (
	cryptoAES "crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

type AES struct {
	aead cipher.AEAD
}

func New(key string) (*AES, error) {
	block, err := cryptoAES.NewCipher([]byte(key))
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AES-GCM: %w", err)
	}
	return &AES{aead: aead}, nil
}

func (a *AES) Encrypt(plainText string) (string, error) {
	nonce := make([]byte, a.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate AES-GCM nonce: %w", err)
	}
	cipherText := a.aead.Seal(nonce, nonce, []byte(plainText), nil)
	return base64.RawURLEncoding.EncodeToString(cipherText), nil
}

func (a *AES) Decrypt(encoded string) (string, error) {
	cipherText, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode AES-GCM ciphertext: %w", err)
	}
	nonceSize := a.aead.NonceSize()
	if len(cipherText) < nonceSize {
		return "", fmt.Errorf("AES-GCM ciphertext is too short")
	}
	nonce, cipherText := cipherText[:nonceSize], cipherText[nonceSize:]
	plainText, err := a.aead.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt AES-GCM ciphertext: %w", err)
	}
	return string(plainText), nil
}

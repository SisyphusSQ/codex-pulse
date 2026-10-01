package aes

import (
	"encoding/base64"
	"testing"
)

func TestAESGCMRoundTrip(t *testing.T) {
	cipher, err := New("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	encrypted, err := cipher.Encrypt("sensitive value")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if decrypted != "sensitive value" {
		t.Fatalf("Decrypt() = %q", decrypted)
	}
}

func TestAESGCMRejectsTampering(t *testing.T) {
	cipher, err := New("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	encrypted, err := cipher.Encrypt("sensitive value")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	raw[len(raw)-1] ^= 1
	tampered := base64.RawURLEncoding.EncodeToString(raw)
	if _, err = cipher.Decrypt(tampered); err == nil {
		t.Fatal("Decrypt() accepted tampered ciphertext")
	}
}

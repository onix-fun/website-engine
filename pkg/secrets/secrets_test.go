package secrets

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func newTestBox(t *testing.T, key string) *Box {
	t.Helper()
	box, err := New(key)
	if err != nil {
		t.Fatalf("New(%q): %v", key, err)
	}
	return box
}

func testKey() string { return "0123456789abcdef0123456789abcdef" } // raw 32 bytes

func TestNewKeyFormats(t *testing.T) {
	t.Run("raw key", func(t *testing.T) {
		newTestBox(t, testKey())
	})
	t.Run("base64 key", func(t *testing.T) {
		encoded := base64.StdEncoding.EncodeToString(make([]byte, 32))
		if _, err := New(encoded); err != nil {
			t.Fatalf("base64 key rejected: %v", err)
		}
	})
	t.Run("empty key", func(t *testing.T) {
		if _, err := New(""); err == nil {
			t.Fatal("expected error for empty key")
		}
	})
	t.Run("short key", func(t *testing.T) {
		if _, err := New("too-short"); err == nil {
			t.Fatal("expected error for short key")
		}
	})
	t.Run("bad base64 treated as raw", func(t *testing.T) {
		// 15 raw chars that are not valid base64 must fail on length, not panic.
		if _, err := New("not base64!!!"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	box := newTestBox(t, testKey())
	secret := "ghp_16C7e42F292c6912E7710c838347Ae178B4a"
	ciphertext, nonce, err := box.Encrypt(secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(ciphertext) == 0 || len(nonce) == 0 {
		t.Fatal("ciphertext and nonce must be set")
	}
	plain, err := box.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plain != secret {
		t.Fatalf("roundtrip mismatch: %q", plain)
	}
}

func TestEncryptEmptyPlaintext(t *testing.T) {
	box := newTestBox(t, testKey())
	ciphertext, nonce, err := box.Encrypt("")
	if err != nil {
		t.Fatalf("encrypt empty: %v", err)
	}
	if ciphertext != nil || nonce != nil {
		t.Fatalf("empty plaintext must produce empty output, got %v %v", ciphertext, nonce)
	}
	plain, err := box.Decrypt(nil, nil)
	if err != nil {
		t.Fatalf("decrypt empty: %v", err)
	}
	if plain != "" {
		t.Fatalf("expected empty plaintext, got %q", plain)
	}
}

func TestEncryptNonceIsUnique(t *testing.T) {
	box := newTestBox(t, testKey())
	c1, n1, _ := box.Encrypt("same")
	c2, n2, _ := box.Encrypt("same")
	if bytes.Equal(n1, n2) {
		t.Fatal("nonce reuse detected")
	}
	if bytes.Equal(c1, c2) {
		t.Fatal("identical plaintexts must encrypt to distinct ciphertexts")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	box := newTestBox(t, testKey())
	ciphertext, nonce, err := box.Encrypt("tok")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	other := newTestBox(t, "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk")
	if _, err := other.Decrypt(ciphertext, nonce); err == nil {
		t.Fatal("expected failure with wrong key")
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	box := newTestBox(t, testKey())
	ciphertext, nonce, err := box.Encrypt("provider-token-12345")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	tampered := bytes.Clone(ciphertext)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := box.Decrypt(tampered, nonce); err == nil {
		t.Fatal("expected failure on tampered ciphertext")
	}
}

func TestDecryptTamperedNonce(t *testing.T) {
	box := newTestBox(t, testKey())
	ciphertext, nonce, err := box.Encrypt("provider-token-12345")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	badNonce := bytes.Clone(nonce)
	badNonce[0] ^= 0x01
	if _, err := box.Decrypt(ciphertext, badNonce); err == nil {
		t.Fatal("expected failure on tampered nonce")
	}
}

func TestDecryptBadNonceSize(t *testing.T) {
	box := newTestBox(t, testKey())
	if _, err := box.Decrypt([]byte("payload"), []byte("short")); err == nil {
		t.Fatal("expected nonce size error")
	}
}

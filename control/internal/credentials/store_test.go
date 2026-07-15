package credentials

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestDecodeKey(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	for _, encoded := range []string{hex.EncodeToString(key), base64.StdEncoding.EncodeToString(key), base64.RawURLEncoding.EncodeToString(key)} {
		got, err := DecodeKey(encoded)
		if err != nil || !bytes.Equal(got, key) {
			t.Fatalf("decode %q: %x %v", encoded, got, err)
		}
	}
	if _, err := DecodeKey("short"); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestSealDoesNotExposePlaintext(t *testing.T) {
	store, err := New(nil, bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, nonce, err := store.seal("openai", "openai", "sk-super-secret")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("sk-super-secret")) {
		t.Fatal("ciphertext contains plaintext")
	}
	plain, err := store.aead.Open(nil, nonce, ciphertext, []byte("openai\x00openai"))
	if err != nil || string(plain) != "sk-super-secret" {
		t.Fatalf("open: %q %v", plain, err)
	}
}

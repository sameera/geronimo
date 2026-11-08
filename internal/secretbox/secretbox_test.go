package secretbox_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/sameera/geronimo/internal/secretbox"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealOpenRoundTrip(t *testing.T) { // G14
	key := newKey(t)
	ct, err := secretbox.Seal(key, []byte("row-1"), []byte("refresh-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("refresh-secret")) {
		t.Fatal("ciphertext contains the plaintext")
	}
	pt, err := secretbox.Open(key, []byte("row-1"), ct)
	if err != nil || string(pt) != "refresh-secret" {
		t.Fatalf("open = %q, %v", pt, err)
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	key := newKey(t)
	a, _ := secretbox.Seal(key, nil, []byte("x"))
	b, _ := secretbox.Seal(key, nil, []byte("x"))
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same plaintext are identical")
	}
}

func TestOpenRefusesWrongKeyContextOrTampering(t *testing.T) { // G14, D6 binding
	key := newKey(t)
	ct, _ := secretbox.Seal(key, []byte("row-1"), []byte("refresh-secret"))
	if _, err := secretbox.Open(newKey(t), []byte("row-1"), ct); err == nil {
		t.Error("opened with another key")
	}
	if _, err := secretbox.Open(key, []byte("row-2"), ct); err == nil {
		t.Error("opened under another record binding")
	}
	bad := append([]byte{}, ct...)
	bad[len(bad)-1] ^= 1
	if _, err := secretbox.Open(key, []byte("row-1"), bad); err == nil {
		t.Error("opened a tampered ciphertext")
	}
	if _, err := secretbox.Open(key, []byte("row-1"), []byte("short")); err == nil {
		t.Error("opened a truncated ciphertext")
	}
}

func TestParseKey(t *testing.T) { // G16
	good := base64.StdEncoding.EncodeToString(newKey(t))
	if k, err := secretbox.ParseKey(good); err != nil || len(k) != 32 {
		t.Fatalf("good key: %v %d", err, len(k))
	}
	for name, v := range map[string]string{
		"empty":     "",
		"not b64":   "!!!not base64!!!",
		"too short": base64.StdEncoding.EncodeToString([]byte("short")),
		"too long":  base64.StdEncoding.EncodeToString(make([]byte, 33)),
	} {
		_, err := secretbox.ParseKey(v)
		if err == nil {
			t.Errorf("%s accepted", name)
		} else if v != "" && strings.Contains(err.Error(), v) {
			t.Errorf("%s: error echoes the key", name)
		}
	}
}

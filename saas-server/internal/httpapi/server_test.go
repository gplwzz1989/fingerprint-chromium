package httpapi

import (
	"encoding/base64"
	"testing"
)

func TestAccountIDValidation(t *testing.T) {
	valid := []string{"account-01", "A_B_2"}
	for _, value := range valid {
		if !validAccountID(value) {
			t.Errorf("validAccountID(%q) = false", value)
		}
	}
	invalid := []string{"", "account/id", "账号", "account id"}
	for _, value := range invalid {
		if validAccountID(value) {
			t.Errorf("validAccountID(%q) = true", value)
		}
	}
}

func TestPageTokenRoundTrip(t *testing.T) {
	encoded := encodePageToken(150)
	offset, err := decodePageToken(encoded)
	if err != nil || offset != 150 {
		t.Fatalf("page token round trip = (%d, %v), want (150, nil)", offset, err)
	}
	if _, err := decodePageToken("not-a-page-token"); err == nil {
		t.Fatal("decodePageToken() accepted invalid input")
	}
}

func TestSnapshotEnvelopeValidation(t *testing.T) {
	encode := base64.StdEncoding.EncodeToString
	valid := encryptedSnapshotEnvelope{
		Algorithm:  "AES-256-GCM",
		KeyWrap:    encode(make([]byte, 32)),
		Nonce:      encode(make([]byte, 12)),
		Ciphertext: encode(make([]byte, 16)),
		Tag:        encode(make([]byte, 16)),
	}
	if !validateEnvelope(valid) {
		t.Fatal("validateEnvelope() rejected a valid envelope")
	}
	valid.Algorithm = "AES-128-GCM"
	if validateEnvelope(valid) {
		t.Fatal("validateEnvelope() accepted an unsupported algorithm")
	}
}

func TestParseIfMatch(t *testing.T) {
	if revision, err := parseIfMatch("12"); err != nil || revision != 12 {
		t.Fatalf("parseIfMatch() = (%d, %v), want (12, nil)", revision, err)
	}
	if _, err := parseIfMatch(""); err == nil {
		t.Fatal("parseIfMatch() accepted an empty header")
	}
}

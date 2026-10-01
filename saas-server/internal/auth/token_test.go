package auth

import (
	"strings"
	"testing"
	"time"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	secret := []byte("a sufficiently long test signing secret")
	token, err := IssueAccessToken(secret, "user-id", "session-id", "device-id", time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v", err)
	}
	claims, err := ParseAccessToken(secret, token)
	if err != nil {
		t.Fatalf("ParseAccessToken() error = %v", err)
	}
	if claims.Subject != "user-id" || claims.ID != "session-id" || claims.DeviceID != "device-id" {
		t.Fatalf("unexpected claims: subject=%q id=%q device=%q", claims.Subject, claims.ID, claims.DeviceID)
	}
}

func TestAccessTokenRejectsTampering(t *testing.T) {
	secret := []byte("a sufficiently long test signing secret")
	token, err := IssueAccessToken(secret, "user-id", "session-id", "device-id", time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v", err)
	}
	parts := strings.Split(token, ".")
	parts[2] = strings.Repeat("0", len(parts[2]))
	if _, err := ParseAccessToken(secret, strings.Join(parts, ".")); err == nil {
		t.Fatal("ParseAccessToken() accepted a tampered token")
	}
}

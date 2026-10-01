package config

import "testing"

func TestLoadRequiresDatabaseAndStrongJWTSecret(t *testing.T) {
	t.Setenv("SAAS_DATABASE_URL", "")
	t.Setenv("SAAS_JWT_SECRET", "short")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted missing database configuration")
	}

	t.Setenv("SAAS_DATABASE_URL", "postgres://configured")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a weak JWT secret")
	}
}

func TestParsePageSize(t *testing.T) {
	if size, err := ParsePageSize(""); err != nil || size != 50 {
		t.Fatalf("ParsePageSize(empty) = (%d, %v), want (50, nil)", size, err)
	}
	if _, err := ParsePageSize("201"); err == nil {
		t.Fatal("ParsePageSize() accepted a size above the limit")
	}
}

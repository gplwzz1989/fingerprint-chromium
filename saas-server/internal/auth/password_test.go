package auth

import "testing"

func TestPasswordHashAndVerify(t *testing.T) {
	password := "正确的测试密码-1234"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	valid, err := VerifyPassword(hash, password)
	if err != nil || !valid {
		t.Fatalf("VerifyPassword() = (%v, %v), want (true, nil)", valid, err)
	}
	valid, err = VerifyPassword(hash, password+"-wrong")
	if err != nil || valid {
		t.Fatalf("VerifyPassword(wrong) = (%v, %v), want (false, nil)", valid, err)
	}
}

func TestPasswordHashRejectsInvalidLength(t *testing.T) {
	if _, err := HashPassword("too-short"); err == nil {
		t.Fatal("HashPassword() accepted a short password")
	}
}

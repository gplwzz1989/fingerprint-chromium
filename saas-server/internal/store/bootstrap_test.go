package store

import "testing"

func TestNormalizeBootstrapUserInput(t *testing.T) {
	input, err := normalizeBootstrapUserInput(BootstrapUserInput{
		Email:         " Admin@Example.com ",
		Password:      "a-secure-password",
		DisplayName:   " 管理员 ",
		WorkspaceName: " 主工作区 ",
	})
	if err != nil {
		t.Fatalf("normalizeBootstrapUserInput() returned error: %v", err)
	}
	if input.Email != "admin@example.com" || input.DisplayName != "管理员" || input.WorkspaceName != "主工作区" {
		t.Fatalf("normalizeBootstrapUserInput() returned %+v", input)
	}
}

func TestNormalizeBootstrapUserInputRejectsInvalidValues(t *testing.T) {
	cases := []BootstrapUserInput{
		{Email: "invalid", Password: "a-secure-password", WorkspaceName: "主工作区"},
		{Email: "admin@example.com", Password: "short", WorkspaceName: "主工作区"},
		{Email: "admin@example.com", Password: "a-secure-password", WorkspaceName: ""},
	}
	for _, input := range cases {
		if _, err := normalizeBootstrapUserInput(input); err == nil {
			t.Fatalf("normalizeBootstrapUserInput(%+v) accepted invalid input", input)
		}
	}
}

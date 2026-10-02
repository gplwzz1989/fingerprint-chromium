package httpapi

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/config"
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

func TestWorkspaceNameValidation(t *testing.T) {
	for _, value := range []string{"主工作区", "team-01", "工作区 2"} {
		if !validWorkspaceName(value) {
			t.Errorf("validWorkspaceName(%q) = false", value)
		}
	}
	for _, value := range []string{"", "\n工作区", strings.Repeat("a", 129)} {
		if validWorkspaceName(value) {
			t.Errorf("validWorkspaceName(%q) = true", value)
		}
	}
}

func TestSessionIDValidation(t *testing.T) {
	if !looksLikeUUID("123e4567-e89b-12d3-a456-426614174000") {
		t.Fatal("looksLikeUUID() rejected a valid UUID")
	}
	for _, value := range []string{"", "123", "123e4567-e89b-12d3-a456-42661417400z", "123e4567/e89b/12d3/a456/426614174000"} {
		if looksLikeUUID(value) {
			t.Fatalf("looksLikeUUID(%q) accepted invalid input", value)
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
	if revision, force, err := parseIfMatch("12"); err != nil ||
		force || revision != 12 {
		t.Fatalf("parseIfMatch() = (%d, %t, %v), want (12, false, nil)",
			revision, force, err)
	}
	if revision, force, err := parseIfMatch("*"); err != nil ||
		!force || revision != 0 {
		t.Fatalf("parseIfMatch(*) = (%d, %t, %v), want (0, true, nil)",
			revision, force, err)
	}
	if _, _, err := parseIfMatch(""); err == nil {
		t.Fatal("parseIfMatch() accepted an empty header")
	}
}

func TestSnapshotLeaseState(t *testing.T) {
	now := time.Unix(100, 0)
	if state := snapshotLeaseState("device-a", "device-a", now.Add(time.Minute), now); state != "" {
		t.Fatalf("active lease state = %q, want empty", state)
	}
	if state := snapshotLeaseState("device-a", "device-a", now.Add(-time.Second), now); state != "lease_required" {
		t.Fatalf("expired lease state = %q, want lease_required", state)
	}
	if state := snapshotLeaseState("device-a", "device-b", now.Add(time.Minute), now); state != "lease_conflict" {
		t.Fatalf("other-device lease state = %q, want lease_conflict", state)
	}
}

func TestCORSAllowsPatchPreflight(t *testing.T) {
	server := &Server{cfg: config.Config{
		AllowedOrigins: map[string]struct{}{
			"https://manager.example": {},
		},
	}}
	handler := server.withCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/accounts/account-01", nil)
	request.Header.Set("Origin", "https://manager.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if !strings.Contains(response.Header().Get("Access-Control-Allow-Methods"), http.MethodPatch) {
		t.Fatal("preflight response did not allow PATCH")
	}

	fullRequest := httptest.NewRequest(http.MethodOptions,
		"/api/v1/accounts/account-01", nil)
	fullRequest.Header.Set("Origin", "https://manager.example")
	fullRequest.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	fullResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(fullResponse, fullRequest)
	if fullResponse.Code != http.StatusNoContent {
		t.Fatalf("full handler preflight status = %d, want %d",
			fullResponse.Code, http.StatusNoContent)
	}
}

func TestWebHandlerServesStaticFilesAndSpaFallback(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(webDir+string(os.PathSeparator)+"index.html", []byte("index"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(webDir+string(os.PathSeparator)+"styles.css", []byte("styles"), 0600); err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: config.Config{WebDir: webDir}}

	tests := []struct {
		path string
		want string
	}{
		{path: "/", want: "index"},
		{path: "/styles.css", want: "styles"},
		{path: "/console/accounts", want: "index"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		response := httptest.NewRecorder()
		server.handleWeb(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", test.path, response.Code, http.StatusOK)
		}
		if response.Body.String() != test.want {
			t.Fatalf("GET %s body = %q, want %q", test.path, response.Body.String(), test.want)
		}
	}
}

func TestWorkspaceInviteRolePolicy(t *testing.T) {
	tests := []struct {
		inviter string
		invited string
		want    bool
	}{
		{inviter: "owner", invited: "admin", want: true},
		{inviter: "owner", invited: "editor", want: true},
		{inviter: "admin", invited: "viewer", want: true},
		{inviter: "admin", invited: "admin", want: false},
		{inviter: "editor", invited: "viewer", want: false},
		{inviter: "owner", invited: "owner", want: false},
	}
	for _, test := range tests {
		if got := canInviteMember(test.inviter, test.invited); got != test.want {
			t.Errorf("canInviteMember(%q, %q) = %t, want %t",
				test.inviter, test.invited, got, test.want)
		}
	}
}

func TestWorkspaceMemberManagementPolicy(t *testing.T) {
	tests := []struct {
		actor  string
		member string
		want   bool
	}{
		{actor: "owner", member: "admin", want: true},
		{actor: "owner", member: "editor", want: true},
		{actor: "admin", member: "editor", want: true},
		{actor: "admin", member: "viewer", want: true},
		{actor: "admin", member: "admin", want: false},
		{actor: "editor", member: "viewer", want: false},
		{actor: "owner", member: "owner", want: false},
	}
	for _, test := range tests {
		if got := canManageMember(test.actor, test.member); got != test.want {
			t.Errorf("canManageMember(%q, %q) = %t, want %t",
				test.actor, test.member, got, test.want)
		}
	}
}

func TestAccountMemberPolicy(t *testing.T) {
	if !canManageAccountMembers("owner") || !canManageAccountMembers("admin") {
		t.Fatal("owner and admin should manage account members")
	}
	for _, role := range []string{"editor", "viewer"} {
		if canManageAccountMembers(role) {
			t.Fatalf("%s should not manage account members", role)
		}
	}
	for _, role := range []string{"editor", "viewer"} {
		if !validAccountMemberRole(role) {
			t.Fatalf("validAccountMemberRole(%q) = false", role)
		}
	}
	for _, role := range []string{"", "owner", "admin", "invalid"} {
		if validAccountMemberRole(role) {
			t.Fatalf("validAccountMemberRole(%q) = true", role)
		}
	}
}

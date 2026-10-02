package httpapi

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/config"
	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/store"
	"golang.org/x/crypto/pbkdf2"
)

func TestPostgresAccountSyncIntegration(t *testing.T) {
	databaseURL := os.Getenv("SAAS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("未配置独立 PostgreSQL 测试数据库")
	}
	ctx := context.Background()
	admin, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	schema := fmt.Sprintf("saas_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	password := "integration-password-123"
	owner, err := store.BootstrapUser(ctx, db, store.BootstrapUserInput{
		Email: "owner@example.test", Password: password, DisplayName: "集成测试", WorkspaceName: "隔离测试工作区",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{JWTSecret: []byte("integration-secret-with-at-least-32-bytes"),
		AccessTokenTTL: time.Minute, RefreshTokenTTL: time.Hour}
	server := httptest.NewServer(NewServer(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	t.Cleanup(server.Close)
	call := func(method, path, token string, body any, revision string) (int, []byte) {
		var reader io.Reader
		if body != nil {
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(encoded)
		}
		r, err := http.NewRequest(method, server.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if revision != "" {
			r.Header.Set("If-Match", revision)
		}
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, data
	}
	expect := func(method, path, token string, body any, revision string, status int) []byte {
		code, data := call(method, path, token, body, revision)
		if code != status {
			t.Fatalf("%s %s：状态 %d，预期 %d，响应 %s", method, path, code, status, data)
		}
		return data
	}
	login := func(device string) sessionResponse {
		data := expect("POST", "/api/v1/sessions", "", createSessionRequest{
			Email: "owner@example.test", Password: password, DeviceID: device, DeviceName: "测试设备",
		}, "", http.StatusOK)
		var session sessionResponse
		if err := json.Unmarshal(data, &session); err != nil {
			t.Fatal(err)
		}
		return session
	}
	a, b := login("device-a"), login("device-b")
	accountPath := "/api/v1/accounts/test-account"
	expect("POST", "/api/v1/workspaces/"+owner.WorkspaceID+"/accounts", a.AccessToken,
		map[string]any{"account_id": "test-account", "name": "同步测试"}, "", http.StatusCreated)
	expect("GET", accountPath+"/snapshot", a.AccessToken, nil, "", http.StatusNotFound)
	// 使用真实 PBKDF2 和 AES-GCM 生成信封，服务端仅保存密文。
	salt, nonce := make([]byte, 16), make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(pbkdf2.Key([]byte(password), salt, 600000, 32, sha256.New))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	encrypted := gcm.Seal(nil, nonce, []byte(`{"schema_version":1,"account_id":"test-account","cookies":[],"local_storage":{}}`), []byte("fingerprint-manager:v1:test-account"))
	encode := base64.StdEncoding.EncodeToString
	envelope := encryptedSnapshotEnvelope{Algorithm: "AES-256-GCM", KDF: "PBKDF2-HMAC-SHA-256", Iterations: 600000,
		Salt: encode(salt), Nonce: encode(nonce), Ciphertext: encode(encrypted[:len(encrypted)-16]), Tag: encode(encrypted[len(encrypted)-16:])}
	body := putSnapshotRequest{SchemaVersion: 1, DeviceID: "device-a", Envelope: envelope}
	expect("PUT", accountPath+"/snapshot", a.AccessToken, body, "0", http.StatusConflict)
	var lease leaseResponse
	if err := json.Unmarshal(expect("POST", accountPath+"/leases", a.AccessToken, acquireLeaseRequest{DeviceID: "device-a"}, "", http.StatusCreated), &lease); err != nil {
		t.Fatal(err)
	}
	expect("POST", accountPath+"/leases", b.AccessToken, acquireLeaseRequest{DeviceID: "device-b"}, "", http.StatusConflict)
	expect("PUT", accountPath+"/snapshot", a.AccessToken, body, "0", http.StatusOK)
	expect("PUT", accountPath+"/snapshot", a.AccessToken, body, "0", http.StatusConflict)
	data := expect("GET", accountPath+"/snapshot", b.AccessToken, nil, "", http.StatusOK)
	var saved accountSnapshotResponse
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 || saved.Envelope != envelope {
		t.Fatal("跨设备读取内容或版本不一致")
	}
	expect("PUT", accountPath+"/snapshot", a.AccessToken, body, "*", http.StatusOK)
	expect("DELETE", accountPath+"/leases/"+lease.LeaseID, a.AccessToken, nil, "", http.StatusNoContent)
	// 两台设备并发首次抢占租约：一个成功，一个返回冲突，不能返回内部错误。
	var group sync.WaitGroup
	codes := make(chan int, 2)
	for _, item := range []struct{ token, device string }{{a.AccessToken, "device-a"}, {b.AccessToken, "device-b"}} {
		group.Add(1)
		go func(token, device string) {
			defer group.Done()
			code, _ := call("POST", accountPath+"/leases", token, acquireLeaseRequest{DeviceID: device}, "")
			codes <- code
		}(item.token, item.device)
	}
	group.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[http.StatusCreated] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("并发租约结果无效：%v", counts)
	}
	audit := expect("GET", accountPath+"/audit-events", a.AccessToken, nil, "", http.StatusOK)
	if !bytes.Contains(audit, []byte("snapshot_overwritten")) {
		t.Fatal("覆盖操作未记录审计")
	}
	expect("GET", "/api/v1/sessions", a.AccessToken, nil, "", http.StatusOK)
	expect("POST", "/api/v1/sessions/revoke", b.AccessToken, nil, "", http.StatusNoContent)
	expect("GET", accountPath+"/snapshot", b.AccessToken, nil, "", http.StatusUnauthorized)
}

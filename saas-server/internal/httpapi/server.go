package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/auth"
	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/config"
)

type Server struct {
	db     *sql.DB
	cfg    config.Config
	logger *slog.Logger
}

const (
	currentSnapshotSchemaVersion = 1
	maxSnapshotBodySize          = 20 << 20
	leaseDuration                = 5 * time.Minute
)

func NewServer(db *sql.DB, cfg config.Config, logger *slog.Logger) *Server {
	return &Server{db: db, cfg: cfg, logger: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/v1/sessions", s.handleCreateSession)
	mux.HandleFunc("POST /api/v1/sessions/refresh", s.handleRefreshSession)
	mux.HandleFunc("POST /api/v1/sessions/revoke", s.handleRevokeSession)
	mux.HandleFunc("GET /api/v1/sessions", s.handleListSessions)
	mux.HandleFunc("DELETE /api/v1/sessions/{session_id}", s.handleRevokeSessionByID)
	mux.HandleFunc("GET /api/v1/workspaces", s.handleListWorkspaces)
	mux.HandleFunc("GET /api/v1/workspaces/{workspace_id}/accounts", s.handleListAccounts)
	mux.HandleFunc("POST /api/v1/workspaces/{workspace_id}/accounts", s.handleCreateAccount)
	mux.HandleFunc("PATCH /api/v1/accounts/{account_id}", s.handleUpdateAccount)
	mux.HandleFunc("GET /api/v1/accounts/{account_id}/snapshot", s.handleGetSnapshot)
	mux.HandleFunc("PUT /api/v1/accounts/{account_id}/snapshot", s.handlePutSnapshot)
	mux.HandleFunc("POST /api/v1/accounts/{account_id}/leases", s.handleAcquireLease)
	mux.HandleFunc("DELETE /api/v1/accounts/{account_id}/leases/{lease_id}", s.handleReleaseLease)
	mux.HandleFunc("GET /api/v1/accounts/{account_id}/audit-events", s.handleListAuditEvents)
	return s.withSecurityHeaders(s.withCORS(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		s.logger.Error("健康检查失败", "error", err.Error())
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "服务暂时不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type createSessionRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
}

type sessionResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token,omitempty"`
	ExpiresIn    int64        `json:"expires_in"`
	User         userResponse `json:"user"`
}

type userResponse struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var request createSessionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	request.DeviceID = strings.TrimSpace(request.DeviceID)
	if !validEmail(request.Email) || len(request.Password) < 12 ||
		len(request.Password) > 256 || !validDeviceID(request.DeviceID) ||
		len(request.DeviceName) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_request", "登录参数无效")
		return
	}

	var userID, email, passwordHash, displayName string
	err := s.db.QueryRowContext(r.Context(), `
		SELECT id::text, email, password_hash, display_name
		FROM users WHERE lower(email) = $1`, request.Email).
		Scan(&userID, &email, &passwordHash, &displayName)
	if err != nil && err != sql.ErrNoRows {
		s.logger.Error("查询用户失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "登录失败，请稍后重试")
		return
	}
	if err == sql.ErrNoRows {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "邮箱或密码错误")
		return
	}
	valid, verifyErr := auth.VerifyPassword(passwordHash, request.Password)
	if verifyErr != nil {
		s.logger.Error("校验密码哈希失败", "error", verifyErr.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "登录失败，请稍后重试")
		return
	}
	if !valid {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "邮箱或密码错误")
		return
	}

	sessionID, err := newUUID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "登录失败，请稍后重试")
		return
	}
	refreshToken, refreshDigest, err := auth.NewRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "登录失败，请稍后重试")
		return
	}
	expiresAt := time.Now().Add(s.cfg.RefreshTokenTTL)
	_, err = s.db.ExecContext(r.Context(), `
		WITH revoked AS (
			UPDATE sessions SET revoked_at = now()
			WHERE user_id = $2::uuid AND device_id = $3 AND revoked_at IS NULL
		)
		INSERT INTO sessions (id, user_id, device_id, device_name,
		                     refresh_token_hash, expires_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)`,
		sessionID, userID, request.DeviceID, strings.TrimSpace(request.DeviceName),
		refreshDigest, expiresAt)
	if err != nil {
		s.logger.Error("创建会话失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "登录失败，请稍后重试")
		return
	}
	accessToken, err := auth.IssueAccessToken(s.cfg.JWTSecret, userID, sessionID,
		request.DeviceID, s.cfg.AccessTokenTTL)
	if err != nil {
		s.logger.Error("签发访问令牌失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "登录失败，请稍后重试")
		return
	}
	if err := s.writeAudit(r.Context(), userID, "login", "", request.DeviceID); err != nil {
		s.logger.Error("写入登录审计失败", "error", err.Error())
	}

	writeJSON(w, http.StatusOK, sessionResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.cfg.AccessTokenTTL / time.Second),
		User: userResponse{
			UserID:      userID,
			Email:       email,
			DisplayName: displayName,
		},
	})
}

type refreshSessionRequest struct {
	RefreshToken string `json:"refresh_token"`
	DeviceID     string `json:"device_id"`
}

func (s *Server) handleRefreshSession(w http.ResponseWriter, r *http.Request) {
	var request refreshSessionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.RefreshToken = strings.TrimSpace(request.RefreshToken)
	request.DeviceID = strings.TrimSpace(request.DeviceID)
	if request.RefreshToken == "" || !validDeviceID(request.DeviceID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "刷新会话参数无效")
		return
	}
	hash := sha256.Sum256([]byte(request.RefreshToken))
	refreshDigest := base64.RawURLEncoding.EncodeToString(hash[:])

	var sessionID, userID, deviceID, email, displayName string
	err := s.db.QueryRowContext(r.Context(), `
		SELECT s.id::text, s.user_id::text, s.device_id, u.email, u.display_name
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.refresh_token_hash = $1
		  AND s.revoked_at IS NULL AND s.expires_at > now()`, refreshDigest).
		Scan(&sessionID, &userID, &deviceID, &email, &displayName)
	if err != nil {
		if err != sql.ErrNoRows {
			s.logger.Error("查询刷新会话失败", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "internal_error", "刷新登录状态失败")
			return
		}
		writeError(w, http.StatusUnauthorized, "unauthorized", "刷新令牌无效或已过期")
		return
	}
	if deviceID != request.DeviceID {
		writeError(w, http.StatusUnauthorized, "unauthorized", "刷新令牌与设备不匹配")
		return
	}

	newRefreshToken, newDigest, err := auth.NewRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "刷新登录状态失败")
		return
	}
	newExpiry := time.Now().Add(s.cfg.RefreshTokenTTL)
	result, err := s.db.ExecContext(r.Context(), `
		UPDATE sessions
		SET refresh_token_hash = $1, expires_at = $2
		WHERE id = $3::uuid AND refresh_token_hash = $4
		  AND revoked_at IS NULL AND expires_at > now()`,
		newDigest, newExpiry, sessionID, refreshDigest)
	if err != nil {
		s.logger.Error("轮换刷新令牌失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "刷新登录状态失败")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		writeError(w, http.StatusUnauthorized, "unauthorized", "刷新令牌已失效，请重新登录")
		return
	}
	accessToken, err := auth.IssueAccessToken(s.cfg.JWTSecret, userID, sessionID,
		deviceID, s.cfg.AccessTokenTTL)
	if err != nil {
		s.logger.Error("签发刷新后的访问令牌失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "刷新登录状态失败")
		return
	}
	if err := s.writeAudit(r.Context(), userID, "token_refreshed", "", deviceID); err != nil {
		s.logger.Error("写入刷新审计失败", "error", err.Error())
	}
	writeJSON(w, http.StatusOK, sessionResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		ExpiresIn:    int64(s.cfg.AccessTokenTTL / time.Second),
		User: userResponse{
			UserID:      userID,
			Email:       email,
			DisplayName: displayName,
		},
	})
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	userID, sessionID, deviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	result, err := s.db.ExecContext(r.Context(), `
		UPDATE sessions SET revoked_at = now()
		WHERE id = $1::uuid AND user_id = $2::uuid AND revoked_at IS NULL`,
		sessionID, userID)
	if err != nil {
		s.logger.Error("撤销会话失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "退出登录失败，请稍后重试")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		writeError(w, http.StatusUnauthorized, "unauthorized", "登录状态已失效")
		return
	}
	if err := s.writeAudit(r.Context(), userID, "logout", "", deviceID); err != nil {
		s.logger.Error("写入退出审计失败", "error", err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
}

type deviceSessionResponse struct {
	SessionID  string `json:"session_id"`
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
	Current    bool   `json:"current"`
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	userID, currentSessionID, _, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id::text, device_id, device_name, created_at, expires_at
		FROM sessions
		WHERE user_id = $1::uuid AND revoked_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC`, userID)
	if err != nil {
		s.logger.Error("查询设备会话失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取设备会话")
		return
	}
	defer rows.Close()

	sessions := make([]deviceSessionResponse, 0)
	for rows.Next() {
		var session deviceSessionResponse
		var createdAt, expiresAt time.Time
		if err := rows.Scan(&session.SessionID, &session.DeviceID,
			&session.DeviceName, &createdAt, &expiresAt); err != nil {
			s.logger.Error("读取设备会话失败", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "internal_error", "无法读取设备会话")
			return
		}
		session.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		session.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
		session.Current = session.SessionID == currentSessionID
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		s.logger.Error("遍历设备会话失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取设备会话")
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleRevokeSessionByID(w http.ResponseWriter, r *http.Request) {
	userID, currentSessionID, deviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	sessionID := strings.TrimSpace(r.PathValue("session_id"))
	if !looksLikeUUID(sessionID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "设备会话标识无效")
		return
	}
	result, err := s.db.ExecContext(r.Context(), `
		UPDATE sessions SET revoked_at = now()
		WHERE id = $1::uuid AND user_id = $2::uuid AND revoked_at IS NULL`,
		sessionID, userID)
	if err != nil {
		s.logger.Error("撤销指定设备会话失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "撤销设备会话失败，请稍后重试")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		s.logger.Error("读取撤销会话结果失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "撤销设备会话失败，请稍后重试")
		return
	}
	if affected != 1 {
		writeError(w, http.StatusNotFound, "not_found", "设备会话不存在或已经失效")
		return
	}
	action := "session_revoked"
	if sessionID == currentSessionID {
		action = "logout"
	}
	if err := s.writeAudit(r.Context(), userID, action, "", deviceID); err != nil {
		s.logger.Error("写入设备会话撤销审计失败", "error", err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
}

type workspaceResponse struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Role        string `json:"role"`
}

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	userID, _, _, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT w.id::text, w.name, wm.role
		FROM workspaces w
		JOIN workspace_members wm ON wm.workspace_id = w.id
		WHERE wm.user_id = $1::uuid
		ORDER BY w.name, w.id`, userID)
	if err != nil {
		s.logger.Error("查询工作区失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取工作区")
		return
	}
	defer rows.Close()
	workspaces := make([]workspaceResponse, 0)
	for rows.Next() {
		var workspace workspaceResponse
		if err := rows.Scan(&workspace.WorkspaceID, &workspace.Name, &workspace.Role); err != nil {
			s.logger.Error("读取工作区失败", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "internal_error", "无法读取工作区")
			return
		}
		workspaces = append(workspaces, workspace)
	}
	if err := rows.Err(); err != nil {
		s.logger.Error("遍历工作区失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取工作区")
		return
	}
	writeJSON(w, http.StatusOK, workspaces)
}

type createAccountRequest struct {
	AccountID string   `json:"account_id"`
	Name      string   `json:"name"`
	Labels    []string `json:"labels"`
}

type updateAccountRequest struct {
	Name   *string   `json:"name"`
	Labels *[]string `json:"labels"`
}

type accountResponse struct {
	AccountID   string   `json:"account_id"`
	WorkspaceID string   `json:"workspace_id"`
	Name        string   `json:"name"`
	Labels      []string `json:"labels"`
	Revision    int64    `json:"revision"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type accountPageResponse struct {
	Items         []accountResponse `json:"items"`
	NextPageToken string            `json:"next_page_token,omitempty"`
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	userID, _, _, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	workspaceID := r.PathValue("workspace_id")
	if !looksLikeUUID(workspaceID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "工作区标识无效")
		return
	}
	if _, ok := s.workspaceRole(r.Context(), userID, workspaceID); !ok {
		writeError(w, http.StatusForbidden, "forbidden", "没有访问该工作区的权限")
		return
	}
	pageSize, err := config.ParsePageSize(r.URL.Query().Get("page_size"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "分页参数无效")
		return
	}
	offset, err := decodePageToken(r.URL.Query().Get("page_token"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "分页参数无效")
		return
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT account_id, workspace_id::text, name, labels::text, revision,
		       created_at, updated_at
		FROM accounts
		WHERE workspace_id = $1::uuid
		ORDER BY account_id
		LIMIT $2 OFFSET $3`, workspaceID, pageSize+1, offset)
	if err != nil {
		s.logger.Error("查询账号目录失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取账号目录")
		return
	}
	defer rows.Close()
	accounts := make([]accountResponse, 0, pageSize)
	for rows.Next() {
		var account accountResponse
		var labelsJSON string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&account.AccountID, &account.WorkspaceID, &account.Name,
			&labelsJSON, &account.Revision, &createdAt, &updatedAt); err != nil {
			s.logger.Error("读取账号目录失败", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "internal_error", "无法读取账号目录")
			return
		}
		if err := json.Unmarshal([]byte(labelsJSON), &account.Labels); err != nil {
			s.logger.Error("解析账号标签失败", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "internal_error", "账号目录数据无效")
			return
		}
		account.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		account.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		accounts = append(accounts, account)
		if len(accounts) > pageSize {
			break
		}
	}
	if err := rows.Err(); err != nil {
		s.logger.Error("遍历账号目录失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取账号目录")
		return
	}
	response := accountPageResponse{Items: accounts}
	if len(accounts) > pageSize {
		response.Items = accounts[:pageSize]
		response.NextPageToken = encodePageToken(offset + pageSize)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	userID, _, deviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	workspaceID := r.PathValue("workspace_id")
	if !looksLikeUUID(workspaceID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "工作区标识无效")
		return
	}
	role, ok := s.workspaceRole(r.Context(), userID, workspaceID)
	if !ok || (role != "owner" && role != "admin" && role != "editor") {
		writeError(w, http.StatusForbidden, "forbidden", "没有创建账号的权限")
		return
	}

	var request createAccountRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.Name = strings.TrimSpace(request.Name)
	if !validAccountID(request.AccountID) || request.Name == "" || len(request.Name) > 256 ||
		len(request.Labels) > 50 {
		writeError(w, http.StatusBadRequest, "invalid_request", "账号参数无效")
		return
	}
	for _, label := range request.Labels {
		if label == "" || len(label) > 64 {
			writeError(w, http.StatusBadRequest, "invalid_request", "账号标签无效")
			return
		}
	}
	labelsJSON, err := json.Marshal(request.Labels)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "账号标签无效")
		return
	}

	var account accountResponse
	var labels string
	var createdAt, updatedAt time.Time
	err = s.db.QueryRowContext(r.Context(), `
		INSERT INTO accounts (account_id, workspace_id, name, labels)
		VALUES ($1, $2::uuid, $3, $4::jsonb)
		RETURNING account_id, workspace_id::text, name, labels::text, revision,
		          created_at, updated_at`,
		request.AccountID, workspaceID, request.Name, string(labelsJSON)).Scan(
		&account.AccountID, &account.WorkspaceID, &account.Name, &labels,
		&account.Revision, &createdAt, &updatedAt)
	if err != nil {
		s.logger.Error("创建账号失败", "error", err.Error())
		writeError(w, http.StatusConflict, "conflict", "账号标识已存在或无法创建")
		return
	}
	if err := json.Unmarshal([]byte(labels), &account.Labels); err != nil {
		s.logger.Error("解析新账号标签失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "账号创建结果无效")
		return
	}
	account.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	account.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	if err := s.writeAudit(r.Context(), userID, "account_created", account.AccountID, deviceID); err != nil {
		s.logger.Error("写入账号审计失败", "error", err.Error())
	}
	writeJSON(w, http.StatusCreated, account)
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	userID, _, deviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	accountID := r.PathValue("account_id")
	if !validAccountID(accountID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "账号标识无效")
		return
	}
	_, role, ok := s.accountAccess(r.Context(), userID, accountID)
	if !ok || !canEdit(role) {
		writeError(w, http.StatusForbidden, "forbidden", "没有修改该账号的权限")
		return
	}

	var request updateAccountRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.Name == nil && request.Labels == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "至少需要修改一个账号字段")
		return
	}

	nameProvided := request.Name != nil
	name := ""
	if nameProvided {
		name = strings.TrimSpace(*request.Name)
		if name == "" || len(name) > 256 {
			writeError(w, http.StatusBadRequest, "invalid_request", "账号名称无效")
			return
		}
	}

	labelsProvided := request.Labels != nil
	labelsJSON := ""
	if labelsProvided {
		if len(*request.Labels) > 50 {
			writeError(w, http.StatusBadRequest, "invalid_request", "账号标签无效")
			return
		}
		for _, label := range *request.Labels {
			if label == "" || len(label) > 64 {
				writeError(w, http.StatusBadRequest, "invalid_request", "账号标签无效")
				return
			}
		}
		encoded, err := json.Marshal(*request.Labels)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "账号标签无效")
			return
		}
		labelsJSON = string(encoded)
	}

	var account accountResponse
	var labels string
	var createdAt, updatedAt time.Time
	var labelsArgument any
	if labelsProvided {
		labelsArgument = labelsJSON
	}
	err := s.db.QueryRowContext(r.Context(), `
		UPDATE accounts
		SET name = CASE WHEN $2::boolean THEN $3 ELSE name END,
		    labels = CASE WHEN $4::boolean THEN $5::jsonb ELSE labels END,
		    updated_at = now()
		WHERE account_id = $1
		RETURNING account_id, workspace_id::text, name, labels::text, revision,
		          created_at, updated_at`,
		accountID, nameProvided, name, labelsProvided, labelsArgument).Scan(
		&account.AccountID, &account.WorkspaceID, &account.Name, &labels,
		&account.Revision, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "not_found", "账号不存在")
			return
		}
		s.logger.Error("更新账号目录失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法更新账号目录")
		return
	}
	if err := json.Unmarshal([]byte(labels), &account.Labels); err != nil {
		s.logger.Error("解析更新后的账号标签失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "账号更新结果无效")
		return
	}
	account.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	account.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	if err := s.writeAudit(r.Context(), userID, "account_updated", account.AccountID, deviceID); err != nil {
		s.logger.Error("写入账号更新审计失败", "error", err.Error())
	}
	writeJSON(w, http.StatusOK, account)
}

type encryptedSnapshotEnvelope struct {
	Algorithm  string `json:"algorithm"`
	KeyWrap    string `json:"key_wrap,omitempty"`
	KDF        string `json:"kdf,omitempty"`
	Iterations int    `json:"iterations,omitempty"`
	Salt       string `json:"salt,omitempty"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
	Tag        string `json:"tag"`
}

type accountSnapshotResponse struct {
	AccountID     string                    `json:"account_id"`
	Revision      int64                     `json:"revision"`
	SchemaVersion int                       `json:"schema_version"`
	DeviceID      string                    `json:"device_id,omitempty"`
	UpdatedAt     string                    `json:"updated_at"`
	Envelope      encryptedSnapshotEnvelope `json:"envelope"`
}

type putSnapshotRequest struct {
	SchemaVersion int                       `json:"schema_version"`
	DeviceID      string                    `json:"device_id"`
	Envelope      encryptedSnapshotEnvelope `json:"envelope"`
}

type snapshotConflictResponse struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	CurrentRevision int64  `json:"current_revision"`
}

func (s *Server) handleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	userID, _, deviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	accountID := r.PathValue("account_id")
	if !validAccountID(accountID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "账号标识无效")
		return
	}
	if _, _, ok := s.accountAccess(r.Context(), userID, accountID); !ok {
		writeError(w, http.StatusForbidden, "forbidden", "没有访问该账号的权限")
		return
	}

	var snapshot accountSnapshotResponse
	var envelopeJSON string
	var updatedAt time.Time
	err := s.db.QueryRowContext(r.Context(), `
		SELECT account_id, revision, schema_version, device_id, envelope::text,
		       created_at
		FROM account_snapshots
		WHERE account_id = $1
		ORDER BY revision DESC
		LIMIT 1`, accountID).Scan(
		&snapshot.AccountID, &snapshot.Revision, &snapshot.SchemaVersion,
		&snapshot.DeviceID, &envelopeJSON, &updatedAt)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "该账号暂无环境快照")
		return
	}
	if err != nil {
		s.logger.Error("读取账号快照失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取账号环境快照")
		return
	}
	if err := json.Unmarshal([]byte(envelopeJSON), &snapshot.Envelope); err != nil {
		s.logger.Error("解析账号快照信封失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "账号环境快照格式无效")
		return
	}
	snapshot.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	if err := s.writeAudit(r.Context(), userID, "snapshot_read", accountID, deviceID); err != nil {
		s.logger.Error("写入快照读取审计失败", "error", err.Error())
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handlePutSnapshot(w http.ResponseWriter, r *http.Request) {
	userID, _, deviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	accountID := r.PathValue("account_id")
	if !validAccountID(accountID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "账号标识无效")
		return
	}
	_, role, ok := s.accountAccess(r.Context(), userID, accountID)
	if !ok || !canEdit(role) {
		writeError(w, http.StatusForbidden, "forbidden", "没有写入该账号快照的权限")
		return
	}
	if deviceID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "登录设备无效")
		return
	}
	expectedRevision, err := parseIfMatch(r.Header.Get("If-Match"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "缺少有效的 If-Match 版本")
		return
	}
	var request putSnapshotRequest
	if !decodeJSONLimit(w, r, &request, maxSnapshotBodySize) {
		return
	}
	request.DeviceID = strings.TrimSpace(request.DeviceID)
	if request.SchemaVersion != currentSnapshotSchemaVersion ||
		request.DeviceID == "" || request.DeviceID != deviceID ||
		!validateEnvelope(request.Envelope) {
		writeError(w, http.StatusBadRequest, "invalid_snapshot", "账号环境快照格式或设备标识无效")
		return
	}
	envelopeJSON, err := json.Marshal(request.Envelope)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_snapshot", "账号环境快照格式无效")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Error("开始写入快照事务失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法写入账号环境快照")
		return
	}
	var currentRevision int64
	if err := tx.QueryRowContext(r.Context(), `
		SELECT revision FROM accounts WHERE account_id = $1 FOR UPDATE`, accountID).
		Scan(&currentRevision); err != nil {
		_ = tx.Rollback()
		s.logger.Error("锁定账号版本失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法写入账号环境快照")
		return
	}
	var leaseDeviceID string
	var leaseExpiresAt time.Time
	leaseErr := tx.QueryRowContext(r.Context(), `
		SELECT device_id, expires_at FROM account_leases
		WHERE account_id = $1 FOR UPDATE`, accountID).
		Scan(&leaseDeviceID, &leaseExpiresAt)
	if leaseErr != nil && leaseErr != sql.ErrNoRows {
		_ = tx.Rollback()
		s.logger.Error("检查账号租约失败", "error", leaseErr.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法校验账号编辑租约")
		return
	}
	leaseState := ""
	if leaseErr == sql.ErrNoRows {
		leaseState = "lease_required"
	} else {
		leaseState = snapshotLeaseState(deviceID, leaseDeviceID, leaseExpiresAt, time.Now())
	}
	if leaseState == "lease_required" {
		_ = tx.Rollback()
		writeError(w, http.StatusConflict, "lease_required", "账号编辑租约已失效，请重新获取")
		return
	}
	if leaseState == "lease_conflict" {
		_ = tx.Rollback()
		writeError(w, http.StatusConflict, "lease_conflict", "该账号正在被其他设备编辑")
		return
	}
	if currentRevision != expectedRevision {
		_ = tx.Rollback()
		writeJSON(w, http.StatusConflict, snapshotConflictResponse{
			Code:            "snapshot_revision_conflict",
			Message:         "账号快照版本已变化，请重新读取后选择合并或覆盖",
			CurrentRevision: currentRevision,
		})
		return
	}
	newRevision := currentRevision + 1
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO account_snapshots
		       (account_id, revision, schema_version, device_id, envelope)
		VALUES ($1, $2, $3, $4, $5::jsonb)`, accountID, newRevision,
		request.SchemaVersion, request.DeviceID, string(envelopeJSON)); err != nil {
		_ = tx.Rollback()
		s.logger.Error("写入账号快照失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法写入账号环境快照")
		return
	}
	var updatedAt time.Time
	if err := tx.QueryRowContext(r.Context(), `
		UPDATE accounts SET revision = $1, updated_at = now()
		WHERE account_id = $2 RETURNING updated_at`, newRevision, accountID).
		Scan(&updatedAt); err != nil {
		_ = tx.Rollback()
		s.logger.Error("更新账号版本失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法更新账号版本")
		return
	}
	if err := tx.Commit(); err != nil {
		s.logger.Error("提交账号快照失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法提交账号环境快照")
		return
	}
	if err := s.writeAudit(r.Context(), userID, "snapshot_written", accountID, deviceID); err != nil {
		s.logger.Error("写入快照审计失败", "error", err.Error())
	}
	writeJSON(w, http.StatusOK, accountSnapshotResponse{
		AccountID:     accountID,
		Revision:      newRevision,
		SchemaVersion: request.SchemaVersion,
		DeviceID:      request.DeviceID,
		UpdatedAt:     updatedAt.UTC().Format(time.RFC3339),
		Envelope:      request.Envelope,
	})
}

type acquireLeaseRequest struct {
	DeviceID string `json:"device_id"`
}

type leaseResponse struct {
	LeaseID   string `json:"lease_id"`
	AccountID string `json:"account_id"`
	DeviceID  string `json:"device_id"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Server) handleAcquireLease(w http.ResponseWriter, r *http.Request) {
	userID, _, sessionDeviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	accountID := r.PathValue("account_id")
	if !validAccountID(accountID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "账号标识无效")
		return
	}
	_, role, ok := s.accountAccess(r.Context(), userID, accountID)
	if !ok || !canEdit(role) {
		writeError(w, http.StatusForbidden, "forbidden", "没有编辑该账号的权限")
		return
	}
	var request acquireLeaseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.DeviceID = strings.TrimSpace(request.DeviceID)
	if request.DeviceID == "" || request.DeviceID != sessionDeviceID {
		writeError(w, http.StatusBadRequest, "invalid_request", "设备标识与当前会话不一致")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "无法获取账号租约")
		return
	}
	var leaseID, currentDeviceID string
	var currentExpiry time.Time
	rowErr := tx.QueryRowContext(r.Context(), `
		SELECT lease_id::text, device_id, expires_at
		FROM account_leases WHERE account_id = $1 FOR UPDATE`, accountID).
		Scan(&leaseID, &currentDeviceID, &currentExpiry)
	if rowErr != nil && rowErr != sql.ErrNoRows {
		_ = tx.Rollback()
		s.logger.Error("查询账号租约失败", "error", rowErr.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法获取账号租约")
		return
	}
	expiresAt := time.Now().Add(leaseDuration)
	if rowErr == nil && currentExpiry.After(time.Now()) && currentDeviceID != request.DeviceID {
		_ = tx.Rollback()
		writeError(w, http.StatusConflict, "lease_conflict", "该账号正在被其他设备编辑")
		return
	}
	if rowErr == nil {
		if _, err = tx.ExecContext(r.Context(), `
			UPDATE account_leases SET device_id = $1, expires_at = $2
			WHERE lease_id = $3::uuid`, request.DeviceID, expiresAt, leaseID); err != nil {
			_ = tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "无法更新账号租约")
			return
		}
	} else {
		leaseID, err = newUUID()
		if err != nil {
			_ = tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "无法创建账号租约")
			return
		}
		if _, err = tx.ExecContext(r.Context(), `
			INSERT INTO account_leases (lease_id, account_id, device_id, expires_at)
			VALUES ($1::uuid, $2, $3, $4)`, leaseID, accountID, request.DeviceID, expiresAt); err != nil {
			_ = tx.Rollback()
			writeError(w, http.StatusInternalServerError, "internal_error", "无法创建账号租约")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "无法提交账号租约")
		return
	}
	if err := s.writeAudit(r.Context(), userID, "lease_acquired", accountID, request.DeviceID); err != nil {
		s.logger.Error("写入租约审计失败", "error", err.Error())
	}
	writeJSON(w, http.StatusCreated, leaseResponse{
		LeaseID: leaseID, AccountID: accountID, DeviceID: request.DeviceID,
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleReleaseLease(w http.ResponseWriter, r *http.Request) {
	userID, _, deviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	accountID := r.PathValue("account_id")
	leaseID := r.PathValue("lease_id")
	if !validAccountID(accountID) || !looksLikeUUID(leaseID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "账号租约标识无效")
		return
	}
	_, role, ok := s.accountAccess(r.Context(), userID, accountID)
	if !ok || !canEdit(role) {
		writeError(w, http.StatusForbidden, "forbidden", "没有编辑该账号的权限")
		return
	}
	result, err := s.db.ExecContext(r.Context(), `
		DELETE FROM account_leases
		WHERE lease_id = $1::uuid AND account_id = $2 AND device_id = $3`,
		leaseID, accountID, deviceID)
	if err != nil {
		s.logger.Error("释放账号租约失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法释放账号租约")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		writeError(w, http.StatusNotFound, "not_found", "账号租约不存在或不属于当前设备")
		return
	}
	if err := s.writeAudit(r.Context(), userID, "lease_released", accountID, deviceID); err != nil {
		s.logger.Error("写入租约释放审计失败", "error", err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
}

type auditEventResponse struct {
	EventID   string `json:"event_id"`
	Action    string `json:"action"`
	DeviceID  string `json:"device_id,omitempty"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	userID, _, _, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	accountID := r.PathValue("account_id")
	if !validAccountID(accountID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "账号标识无效")
		return
	}
	if _, _, ok := s.accountAccess(r.Context(), userID, accountID); !ok {
		writeError(w, http.StatusForbidden, "forbidden", "没有访问该账号的权限")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT event_id::text, action, COALESCE(device_id, ''), created_at
		FROM audit_events WHERE account_id = $1
		ORDER BY created_at DESC LIMIT 200`, accountID)
	if err != nil {
		s.logger.Error("查询账号审计失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取账号审计记录")
		return
	}
	defer rows.Close()
	events := make([]auditEventResponse, 0)
	for rows.Next() {
		var event auditEventResponse
		var createdAt time.Time
		if err := rows.Scan(&event.EventID, &event.Action, &event.DeviceID, &createdAt); err != nil {
			s.logger.Error("读取账号审计失败", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "internal_error", "无法读取账号审计记录")
			return
		}
		event.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		s.logger.Error("遍历账号审计失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取账号审计记录")
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (string, string, string, bool) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(authorization) < len("Bearer ") ||
		!strings.EqualFold(authorization[:len("Bearer ")], "Bearer ") {
		writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return "", "", "", false
	}
	claims, err := auth.ParseAccessToken(s.cfg.JWTSecret,
		strings.TrimSpace(authorization[len("Bearer "):]))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "登录已失效，请重新登录")
		return "", "", "", false
	}
	var active bool
	if err := s.db.QueryRowContext(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM sessions
			WHERE id = $1::uuid AND user_id = $2::uuid
			  AND revoked_at IS NULL AND expires_at > now()
		)`, claims.ID, claims.Subject).Scan(&active); err != nil {
		s.logger.Error("校验会话失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法校验登录状态")
		return "", "", "", false
	}
	if !active {
		writeError(w, http.StatusUnauthorized, "unauthorized", "登录已失效，请重新登录")
		return "", "", "", false
	}
	return claims.Subject, claims.ID, claims.DeviceID, true
}

func (s *Server) workspaceRole(ctx context.Context, userID, workspaceID string) (string, bool) {
	var role string
	err := s.db.QueryRowContext(ctx, `
		SELECT role FROM workspace_members
		WHERE workspace_id = $1::uuid AND user_id = $2::uuid`,
		workspaceID, userID).Scan(&role)
	if err != nil {
		if err != sql.ErrNoRows {
			s.logger.Error("查询工作区权限失败", "error", err.Error())
		}
		return "", false
	}
	return role, true
}

func (s *Server) accountAccess(ctx context.Context, userID, accountID string) (string, string, bool) {
	var workspaceID, role string
	err := s.db.QueryRowContext(ctx, `
		SELECT a.workspace_id::text, wm.role
		FROM accounts a
		JOIN workspace_members wm ON wm.workspace_id = a.workspace_id
		WHERE a.account_id = $1 AND wm.user_id = $2::uuid`,
		accountID, userID).Scan(&workspaceID, &role)
	if err != nil {
		if err != sql.ErrNoRows {
			s.logger.Error("查询账号权限失败", "error", err.Error())
		}
		return "", "", false
	}
	return workspaceID, role, true
}

func canEdit(role string) bool {
	return role == "owner" || role == "admin" || role == "editor"
}

func snapshotLeaseState(deviceID, leaseDeviceID string, expiresAt, now time.Time) string {
	if !expiresAt.After(now) {
		return "lease_required"
	}
	if leaseDeviceID != deviceID {
		return "lease_conflict"
	}
	return ""
}

func (s *Server) writeAudit(ctx context.Context, userID, action, accountID, deviceID string) error {
	eventID, err := newUUID()
	if err != nil {
		return err
	}
	var account any
	if accountID != "" {
		account = accountID
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO audit_events (event_id, user_id, account_id, action, device_id)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5)`,
		eventID, userID, account, action, deviceID)
	return err
}

func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			if _, ok := s.cfg.AllowedOrigins[origin]; !ok {
				if r.Method == http.MethodOptions {
					writeError(w, http.StatusForbidden, "forbidden", "请求来源未获允许")
					return
				}
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, If-Match")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Add("Vary", "Origin")
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	return decodeJSONLimit(w, r, target, 2<<20)
}

func decodeJSONLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求数据格式无效")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求数据格式无效")
		return false
	}
	return true
}

func parseIfMatch(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("缺少 If-Match")
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision < 0 {
		return 0, fmt.Errorf("If-Match 无效")
	}
	return revision, nil
}

func validateEnvelope(envelope encryptedSnapshotEnvelope) bool {
	if envelope.Algorithm != "AES-256-GCM" {
		return false
	}
	nonce, nonceOK := decodeBase64(envelope.Nonce)
	ciphertext, ciphertextOK := decodeBase64(envelope.Ciphertext)
	tag, tagOK := decodeBase64(envelope.Tag)
	if !nonceOK || !ciphertextOK || !tagOK || len(nonce) != 12 ||
		len(ciphertext) < 16 || len(ciphertext) > 16<<20 || len(tag) != 16 {
		return false
	}
	if envelope.KeyWrap != "" {
		if keyWrap, ok := decodeBase64(envelope.KeyWrap); !ok || len(keyWrap) == 0 {
			return false
		}
	}
	if envelope.KDF != "" {
		if envelope.KDF != "PBKDF2-HMAC-SHA-256" ||
			envelope.Iterations < 600000 || envelope.Iterations > 2000000 {
			return false
		}
		salt, ok := decodeBase64(envelope.Salt)
		if !ok || len(salt) < 16 || len(salt) > 64 {
			return false
		}
	}
	if envelope.KeyWrap == "" && envelope.KDF == "" {
		return false
	}
	return true
}

func decodeBase64(value string) ([]byte, bool) {
	if value == "" {
		return nil, false
	}
	decoders := []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	}
	for _, decoder := range decoders {
		if decoded, err := decoder.DecodeString(value); err == nil {
			return decoded, true
		}
	}
	return nil, false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Code: code, Message: message})
}

func validEmail(value string) bool {
	if len(value) == 0 || len(value) > 320 {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}

func validDeviceID(value string) bool {
	return len(value) >= 1 && len(value) <= 128
}

func validAccountID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !((character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') ||
			(character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func encodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodePageToken(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, fmt.Errorf("分页标记无效")
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("分页标记无效")
	}
	return offset, nil
}

func newUUID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成标识失败: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]), nil
}

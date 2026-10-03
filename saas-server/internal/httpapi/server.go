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
	"os"
	"path"
	"path/filepath"
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
	workspaceInviteDuration      = 7 * 24 * time.Hour
)

func NewServer(db *sql.DB, cfg config.Config, logger *slog.Logger) *Server {
	return &Server{db: db, cfg: cfg, logger: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("OPTIONS /api/", s.handleAPIOptions)
	mux.HandleFunc("OPTIONS /api", s.handleAPIOptions)
	mux.Handle("/", http.HandlerFunc(s.handleWeb))
	mux.HandleFunc("POST /api/v1/sessions", s.handleCreateSession)
	mux.HandleFunc("POST /api/v1/sessions/refresh", s.handleRefreshSession)
	mux.HandleFunc("POST /api/v1/sessions/revoke", s.handleRevokeSession)
	mux.HandleFunc("GET /api/v1/sessions", s.handleListSessions)
	mux.HandleFunc("DELETE /api/v1/sessions/{session_id}", s.handleRevokeSessionByID)
	mux.HandleFunc("POST /api/v1/invitations/accept", s.handleAcceptWorkspaceInvite)
	mux.HandleFunc("POST /api/v1/workspaces", s.handleCreateWorkspace)
	mux.HandleFunc("GET /api/v1/workspaces", s.handleListWorkspaces)
	mux.HandleFunc("POST /api/v1/workspaces/{workspace_id}/invitations", s.handleCreateWorkspaceInvite)
	mux.HandleFunc("GET /api/v1/workspaces/{workspace_id}/members", s.handleListWorkspaceMembers)
	mux.HandleFunc("PATCH /api/v1/workspaces/{workspace_id}/members/{user_id}", s.handleUpdateWorkspaceMember)
	mux.HandleFunc("DELETE /api/v1/workspaces/{workspace_id}/members/{user_id}", s.handleRemoveWorkspaceMember)
	mux.HandleFunc("GET /api/v1/workspaces/{workspace_id}/accounts", s.handleListAccounts)
	mux.HandleFunc("POST /api/v1/workspaces/{workspace_id}/accounts", s.handleCreateAccount)
	mux.HandleFunc("PATCH /api/v1/accounts/{account_id}", s.handleUpdateAccount)
	mux.HandleFunc("DELETE /api/v1/accounts/{account_id}", s.handleDeleteAccount)
	mux.HandleFunc("GET /api/v1/accounts/{account_id}/members", s.handleListAccountMembers)
	mux.HandleFunc("PATCH /api/v1/accounts/{account_id}/members/{user_id}", s.handleUpdateAccountMember)
	mux.HandleFunc("DELETE /api/v1/accounts/{account_id}/members/{user_id}", s.handleRemoveAccountMember)
	mux.HandleFunc("GET /api/v1/accounts/{account_id}/snapshot", s.handleGetSnapshot)
	mux.HandleFunc("PUT /api/v1/accounts/{account_id}/snapshot", s.handlePutSnapshot)
	mux.HandleFunc("POST /api/v1/accounts/{account_id}/leases", s.handleAcquireLease)
	mux.HandleFunc("DELETE /api/v1/accounts/{account_id}/leases/{lease_id}", s.handleReleaseLease)
	mux.HandleFunc("GET /api/v1/accounts/{account_id}/audit-events", s.handleListAuditEvents)
	return s.withSecurityHeaders(s.withCORS(s.withRateLimit(mux)))
}

func (s *Server) handleAPIOptions(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWeb(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebDir == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "该页面只支持读取")
		return
	}

	root, err := filepath.Abs(s.cfg.WebDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	relativePath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if relativePath == "" || relativePath == "." {
		relativePath = "index.html"
	}
	target := filepath.Join(root, filepath.FromSlash(relativePath))
	relativeTarget, err := filepath.Rel(root, target)
	if err != nil || relativeTarget == ".." || strings.HasPrefix(relativeTarget, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		// 页面路由交给前端处理，但目录不能被当作静态文件目录列出。
		target = filepath.Join(root, "index.html")
		info, err = os.Stat(target)
	}
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, target)
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

type createWorkspaceRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	userID, _, deviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}

	var request createWorkspaceRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if !validWorkspaceName(request.Name) {
		writeError(w, http.StatusBadRequest, "invalid_request", "工作区名称无效")
		return
	}

	workspaceID, err := newUUID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "创建工作区失败，请稍后重试")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Error("开始创建工作区事务失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "创建工作区失败，请稍后重试")
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO workspaces (id, name) VALUES ($1::uuid, $2)`,
		workspaceID, request.Name); err != nil {
		s.logger.Error("保存工作区失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "创建工作区失败，请稍后重试")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1::uuid, $2::uuid, 'owner')`, workspaceID, userID); err != nil {
		s.logger.Error("保存工作区所有者失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "创建工作区失败，请稍后重试")
		return
	}
	if err := tx.Commit(); err != nil {
		s.logger.Error("提交工作区创建事务失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "创建工作区失败，请稍后重试")
		return
	}

	if err := s.writeAudit(r.Context(), userID, "workspace_created", "", deviceID); err != nil {
		s.logger.Error("写入工作区创建审计失败", "error", err.Error())
	}
	writeJSON(w, http.StatusCreated, workspaceResponse{
		WorkspaceID: workspaceID,
		Name:        request.Name,
		Role:        "owner",
	})
}

type createWorkspaceInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type workspaceInviteResponse struct {
	InviteID    string `json:"invite_id"`
	WorkspaceID string `json:"workspace_id"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	ExpiresAt   string `json:"expires_at"`
	InviteToken string `json:"invite_token,omitempty"`
}

type acceptWorkspaceInviteRequest struct {
	InviteToken string `json:"invite_token"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type acceptWorkspaceInviteResponse struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	WorkspaceID string `json:"workspace_id"`
	Role        string `json:"role"`
}

func (s *Server) handleCreateWorkspaceInvite(w http.ResponseWriter, r *http.Request) {
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
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "没有访问该工作区的权限")
		return
	}

	var request createWorkspaceInviteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	request.Role = strings.ToLower(strings.TrimSpace(request.Role))
	if !validEmail(request.Email) || !validWorkspaceInviteRole(request.Role) {
		writeError(w, http.StatusBadRequest, "invalid_request", "邀请参数无效")
		return
	}
	if !canInviteMember(role, request.Role) {
		writeError(w, http.StatusForbidden, "forbidden", "当前用户无权授予该成员角色")
		return
	}

	inviteToken, tokenHash, err := auth.NewInviteToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "生成邀请令牌失败，请稍后重试")
		return
	}
	inviteID, err := newUUID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "生成邀请标识失败，请稍后重试")
		return
	}
	expiresAt := time.Now().Add(workspaceInviteDuration)

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Error("开始创建工作区邀请事务失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "创建邀请失败，请稍后重试")
		return
	}
	defer tx.Rollback()

	var memberExists bool
	if err := tx.QueryRowContext(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM workspace_members wm
			JOIN users u ON u.id = wm.user_id
			WHERE wm.workspace_id = $1::uuid AND lower(u.email) = $2
		)`, workspaceID, request.Email).Scan(&memberExists); err != nil {
		s.logger.Error("检查工作区成员失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "创建邀请失败，请稍后重试")
		return
	}
	if memberExists {
		writeError(w, http.StatusConflict, "conflict", "该邮箱已经是工作区成员")
		return
	}

	if _, err := tx.ExecContext(r.Context(), `
		UPDATE workspace_invites
		SET revoked_at = now()
		WHERE workspace_id = $1::uuid AND lower(email) = $2
		  AND accepted_at IS NULL AND revoked_at IS NULL`,
		workspaceID, request.Email); err != nil {
		s.logger.Error("撤销旧工作区邀请失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "创建邀请失败，请稍后重试")
		return
	}

	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO workspace_invites
		    (invite_id, workspace_id, inviter_user_id, email, role,
		     token_hash, expires_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7)`,
		inviteID, workspaceID, userID, request.Email, request.Role,
		tokenHash, expiresAt); err != nil {
		s.logger.Error("保存工作区邀请失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "创建邀请失败，请稍后重试")
		return
	}
	if err := tx.Commit(); err != nil {
		s.logger.Error("提交工作区邀请失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "创建邀请失败，请稍后重试")
		return
	}

	if err := s.writeAudit(r.Context(), userID, "member_invited", "", deviceID); err != nil {
		s.logger.Error("写入成员邀请审计失败", "error", err.Error())
	}
	writeJSON(w, http.StatusCreated, workspaceInviteResponse{
		InviteID: inviteID, WorkspaceID: workspaceID, Email: request.Email,
		Role: request.Role, ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
		InviteToken: inviteToken,
	})
}

func (s *Server) handleAcceptWorkspaceInvite(w http.ResponseWriter, r *http.Request) {
	var request acceptWorkspaceInviteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.InviteToken = strings.TrimSpace(request.InviteToken)
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	if request.InviteToken == "" || len(request.InviteToken) > 256 ||
		len(request.Password) < 12 || len(request.Password) > 256 ||
		len(request.DisplayName) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_request", "邀请加入参数无效")
		return
	}

	tokenDigest := sha256.Sum256([]byte(request.InviteToken))
	tokenHash := base64.RawURLEncoding.EncodeToString(tokenDigest[:])
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Error("开始接受工作区邀请事务失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "接受邀请失败，请稍后重试")
		return
	}
	defer tx.Rollback()

	var inviteID, workspaceID, email, role string
	var expiresAt time.Time
	err = tx.QueryRowContext(r.Context(), `
		SELECT invite_id::text, workspace_id::text, email, role, expires_at
		FROM workspace_invites
		WHERE token_hash = $1 AND accepted_at IS NULL AND revoked_at IS NULL
		FOR UPDATE`, tokenHash).Scan(
		&inviteID, &workspaceID, &email, &role, &expiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusUnauthorized, "unauthorized", "邀请令牌无效或已过期")
			return
		}
		s.logger.Error("查询工作区邀请失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "接受邀请失败，请稍后重试")
		return
	}
	if !expiresAt.After(time.Now()) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "邀请令牌无效或已过期")
		return
	}

	var userID, passwordHash, displayName string
	userErr := tx.QueryRowContext(r.Context(), `
		SELECT id::text, password_hash, display_name
		FROM users WHERE lower(email) = $1 FOR UPDATE`, email).
		Scan(&userID, &passwordHash, &displayName)
	if userErr != nil && userErr != sql.ErrNoRows {
		s.logger.Error("查询受邀用户失败", "error", userErr.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "接受邀请失败，请稍后重试")
		return
	}
	if userErr == sql.ErrNoRows {
		passwordHash, err = auth.HashPassword(request.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "密码长度或格式无效")
			return
		}
		userID, err = newUUID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "生成用户标识失败，请稍后重试")
			return
		}
		displayName = request.DisplayName
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO users (id, email, password_hash, display_name)
			VALUES ($1::uuid, $2, $3, $4)`,
			userID, email, passwordHash, displayName); err != nil {
			s.logger.Error("创建受邀用户失败", "error", err.Error())
			writeError(w, http.StatusConflict, "conflict", "用户邮箱已存在，请直接登录后重试")
			return
		}
	} else {
		valid, verifyErr := auth.VerifyPassword(passwordHash, request.Password)
		if verifyErr != nil {
			s.logger.Error("校验受邀用户密码失败", "error", verifyErr.Error())
			writeError(w, http.StatusInternalServerError, "internal_error", "接受邀请失败，请稍后重试")
			return
		}
		if !valid {
			writeError(w, http.StatusUnauthorized, "unauthorized", "用户密码错误")
			return
		}
	}

	result, err := tx.ExecContext(r.Context(), `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT (workspace_id, user_id) DO NOTHING`, workspaceID, userID, role)
	if err != nil {
		s.logger.Error("加入工作区失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "接受邀请失败，请稍后重试")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		writeError(w, http.StatusConflict, "conflict", "用户已经是该工作区成员")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE workspace_invites SET accepted_at = now()
		WHERE invite_id = $1::uuid`, inviteID); err != nil {
		s.logger.Error("标记工作区邀请已接受失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "接受邀请失败，请稍后重试")
		return
	}
	if err := tx.Commit(); err != nil {
		s.logger.Error("提交工作区成员加入失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "接受邀请失败，请稍后重试")
		return
	}

	if err := s.writeAudit(r.Context(), userID, "member_joined", "", ""); err != nil {
		s.logger.Error("写入成员加入审计失败", "error", err.Error())
	}
	writeJSON(w, http.StatusCreated, acceptWorkspaceInviteResponse{
		UserID: userID, Email: email, DisplayName: displayName,
		WorkspaceID: workspaceID, Role: role,
	})
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

type workspaceMemberResponse struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
}

type updateWorkspaceMemberRequest struct {
	Role string `json:"role"`
}

func (s *Server) handleListWorkspaceMembers(w http.ResponseWriter, r *http.Request) {
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

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT u.id::text, u.email, u.display_name, wm.role, wm.created_at
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		WHERE wm.workspace_id = $1::uuid
		ORDER BY lower(u.email), u.id`, workspaceID)
	if err != nil {
		s.logger.Error("查询工作区成员失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取工作区成员")
		return
	}
	defer rows.Close()
	members := make([]workspaceMemberResponse, 0)
	for rows.Next() {
		var member workspaceMemberResponse
		var createdAt time.Time
		if err := rows.Scan(&member.UserID, &member.Email, &member.DisplayName,
			&member.Role, &createdAt); err != nil {
			s.logger.Error("读取工作区成员失败", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "internal_error", "无法读取工作区成员")
			return
		}
		member.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		s.logger.Error("遍历工作区成员失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取工作区成员")
		return
	}
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) handleUpdateWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	userID, _, deviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	workspaceID := r.PathValue("workspace_id")
	targetUserID := r.PathValue("user_id")
	if !looksLikeUUID(workspaceID) || !looksLikeUUID(targetUserID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "成员标识无效")
		return
	}
	actorRole, ok := s.workspaceRole(r.Context(), userID, workspaceID)
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "没有访问该工作区的权限")
		return
	}
	if userID == targetUserID {
		writeError(w, http.StatusConflict, "conflict", "不能修改当前用户自己的角色")
		return
	}

	var request updateWorkspaceMemberRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.Role = strings.ToLower(strings.TrimSpace(request.Role))
	if !validWorkspaceInviteRole(request.Role) || !canInviteMember(actorRole, request.Role) {
		writeError(w, http.StatusForbidden, "forbidden", "当前用户无权授予该成员角色")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Error("开始调整工作区成员事务失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "调整成员角色失败，请稍后重试")
		return
	}
	defer tx.Rollback()

	var currentRole string
	if err := tx.QueryRowContext(r.Context(), `
		SELECT role FROM workspace_members
		WHERE workspace_id = $1::uuid AND user_id = $2::uuid
		FOR UPDATE`, workspaceID, targetUserID).Scan(&currentRole); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "not_found", "工作区成员不存在")
			return
		}
		s.logger.Error("查询待调整成员失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "调整成员角色失败，请稍后重试")
		return
	}
	if !canManageMember(actorRole, currentRole) {
		writeError(w, http.StatusForbidden, "forbidden", "当前用户无权调整该成员")
		return
	}

	if _, err := tx.ExecContext(r.Context(), `
		UPDATE workspace_members SET role = $1
		WHERE workspace_id = $2::uuid AND user_id = $3::uuid`,
		request.Role, workspaceID, targetUserID); err != nil {
		s.logger.Error("更新工作区成员角色失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "调整成员角色失败，请稍后重试")
		return
	}
	var member workspaceMemberResponse
	var createdAt time.Time
	if err := tx.QueryRowContext(r.Context(), `
		SELECT u.id::text, u.email, u.display_name, wm.role, wm.created_at
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		WHERE wm.workspace_id = $1::uuid AND wm.user_id = $2::uuid`,
		workspaceID, targetUserID).Scan(&member.UserID, &member.Email,
		&member.DisplayName, &member.Role, &createdAt); err != nil {
		s.logger.Error("读取更新后的工作区成员失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "调整成员角色失败，请稍后重试")
		return
	}
	if err := tx.Commit(); err != nil {
		s.logger.Error("提交工作区成员角色调整失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "调整成员角色失败，请稍后重试")
		return
	}
	member.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	if err := s.writeAudit(r.Context(), userID, "member_role_updated", "", deviceID); err != nil {
		s.logger.Error("写入成员角色审计失败", "error", err.Error())
	}
	writeJSON(w, http.StatusOK, member)
}

func (s *Server) handleRemoveWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	userID, _, deviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	workspaceID := r.PathValue("workspace_id")
	targetUserID := r.PathValue("user_id")
	if !looksLikeUUID(workspaceID) || !looksLikeUUID(targetUserID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "成员标识无效")
		return
	}
	actorRole, ok := s.workspaceRole(r.Context(), userID, workspaceID)
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "没有访问该工作区的权限")
		return
	}
	if userID == targetUserID {
		writeError(w, http.StatusConflict, "conflict", "不能移除当前用户自己")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Error("开始移除工作区成员事务失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "移除成员失败，请稍后重试")
		return
	}
	defer tx.Rollback()
	var currentRole string
	if err := tx.QueryRowContext(r.Context(), `
		SELECT role FROM workspace_members
		WHERE workspace_id = $1::uuid AND user_id = $2::uuid
		FOR UPDATE`, workspaceID, targetUserID).Scan(&currentRole); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "not_found", "工作区成员不存在")
			return
		}
		s.logger.Error("查询待移除成员失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "移除成员失败，请稍后重试")
		return
	}
	if !canManageMember(actorRole, currentRole) {
		writeError(w, http.StatusForbidden, "forbidden", "当前用户无权移除该成员")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		DELETE FROM workspace_members
		WHERE workspace_id = $1::uuid AND user_id = $2::uuid`,
		workspaceID, targetUserID); err != nil {
		s.logger.Error("删除工作区成员失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "移除成员失败，请稍后重试")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		DELETE FROM account_members am
		USING accounts a
		WHERE a.account_id = am.account_id
		  AND a.workspace_id = $1::uuid
		  AND am.user_id = $2::uuid`, workspaceID, targetUserID); err != nil {
		s.logger.Error("清理工作区成员账号权限失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "移除成员失败，请稍后重试")
		return
	}
	if err := tx.Commit(); err != nil {
		s.logger.Error("提交工作区成员移除失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "移除成员失败，请稍后重试")
		return
	}
	if err := s.writeAudit(r.Context(), userID, "member_removed", "", deviceID); err != nil {
		s.logger.Error("写入成员移除审计失败", "error", err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
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
	Role        string   `json:"role,omitempty"`
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
		SELECT accounts.account_id, accounts.workspace_id::text, accounts.name,
		       accounts.labels::text, accounts.revision, accounts.created_at, accounts.updated_at,
		       CASE
		         WHEN access_member.role IN ('owner', 'admin') THEN access_member.role
		         WHEN access_member.role = 'viewer' THEN 'viewer'
		         ELSE COALESCE(member_assignment.role, access_member.role)
		       END
		FROM accounts
		JOIN workspace_members access_member
		  ON access_member.workspace_id = accounts.workspace_id AND access_member.user_id = $2::uuid
		LEFT JOIN account_members member_assignment
		  ON member_assignment.account_id = accounts.account_id AND member_assignment.user_id = $2::uuid
		WHERE accounts.workspace_id = $1::uuid
		  AND (
			EXISTS (
				SELECT 1 FROM workspace_members privileged
				WHERE privileged.workspace_id = accounts.workspace_id
				  AND privileged.user_id = $2::uuid
				  AND privileged.role IN ('owner', 'admin')
			)
			OR NOT EXISTS (
				SELECT 1 FROM account_members restricted
				WHERE restricted.account_id = accounts.account_id
			)
			OR EXISTS (
				SELECT 1 FROM account_members assigned
				WHERE assigned.account_id = accounts.account_id
				  AND assigned.user_id = $2::uuid
			)
		  )
		ORDER BY accounts.account_id
		LIMIT $3 OFFSET $4`, workspaceID, userID, pageSize+1, offset)
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
			&labelsJSON, &account.Revision, &createdAt, &updatedAt, &account.Role); err != nil {
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

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
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
	if !ok || !canManageAccountMembers(role) {
		writeError(w, http.StatusForbidden, "forbidden", "只有工作区所有者或管理员可以删除账号")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Error("开始删除账号事务失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法删除账号")
		return
	}
	defer tx.Rollback()
	var leaseDeviceID string
	var leaseExpiresAt time.Time
	leaseErr := tx.QueryRowContext(r.Context(), `
		SELECT device_id, expires_at FROM account_leases
		WHERE account_id = $1 FOR UPDATE`, accountID).Scan(&leaseDeviceID, &leaseExpiresAt)
	if leaseErr != nil && leaseErr != sql.ErrNoRows {
		s.logger.Error("检查账号删除租约失败", "error", leaseErr.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法检查账号占用状态")
		return
	}
	if leaseErr == nil && leaseExpiresAt.After(time.Now()) {
		writeError(w, http.StatusConflict, "lease_conflict", "该账号正在被设备编辑，请等待租约释放后再删除")
		return
	}
	var deletedID string
	if err := tx.QueryRowContext(r.Context(), `
		DELETE FROM accounts WHERE account_id = $1 RETURNING account_id`, accountID).Scan(&deletedID); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "not_found", "账号不存在")
			return
		}
		s.logger.Error("删除账号失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法删除账号")
		return
	}
	eventID, err := newUUID()
	if err != nil {
		s.logger.Error("生成删除账号审计标识失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "账号删除未完成")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO audit_events (event_id, user_id, account_id, action, device_id)
		VALUES ($1::uuid, $2::uuid, $3, 'account_deleted', $4)`, eventID, userID, deletedID, deviceID); err != nil {
		s.logger.Error("写入删除账号审计失败", "error", err.Error())
	}
	if err := tx.Commit(); err != nil {
		s.logger.Error("提交删除账号事务失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "账号删除未完成")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account_id": deletedID, "deleted": true})
}

type updateAccountMemberRequest struct {
	Role string `json:"role"`
}

type accountMemberResponse struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
}

type accountMemberListResponse struct {
	Restricted bool                    `json:"restricted"`
	Members    []accountMemberResponse `json:"members"`
}

func (s *Server) handleListAccountMembers(w http.ResponseWriter, r *http.Request) {
	userID, _, _, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	accountID := r.PathValue("account_id")
	if !validAccountID(accountID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "账号标识无效")
		return
	}
	_, role, ok := s.accountAccess(r.Context(), userID, accountID)
	if !ok || !canManageAccountMembers(role) {
		writeError(w, http.StatusForbidden, "forbidden", "没有管理该账号成员的权限")
		return
	}

	var restricted bool
	if err := s.db.QueryRowContext(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM account_members WHERE account_id = $1
		)`, accountID).Scan(&restricted); err != nil {
		s.logger.Error("查询账号成员范围失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取账号成员范围")
		return
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT am.user_id::text, u.email, u.display_name, am.role, am.created_at
		FROM account_members am
		JOIN users u ON u.id = am.user_id
		WHERE am.account_id = $1
		ORDER BY lower(u.email), u.id`, accountID)
	if err != nil {
		s.logger.Error("查询账号成员失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取账号成员")
		return
	}
	defer rows.Close()
	members := make([]accountMemberResponse, 0)
	for rows.Next() {
		var member accountMemberResponse
		var createdAt time.Time
		if err := rows.Scan(&member.UserID, &member.Email, &member.DisplayName,
			&member.Role, &createdAt); err != nil {
			s.logger.Error("读取账号成员失败", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "internal_error", "无法读取账号成员")
			return
		}
		member.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		s.logger.Error("遍历账号成员失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "无法读取账号成员")
		return
	}
	writeJSON(w, http.StatusOK, accountMemberListResponse{
		Restricted: restricted,
		Members:    members,
	})
}

func (s *Server) handleUpdateAccountMember(w http.ResponseWriter, r *http.Request) {
	userID, _, deviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	accountID := r.PathValue("account_id")
	targetUserID := r.PathValue("user_id")
	if !validAccountID(accountID) || !looksLikeUUID(targetUserID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "成员标识无效")
		return
	}
	workspaceID, actorRole, ok := s.accountAccess(r.Context(), userID, accountID)
	if !ok || !canManageAccountMembers(actorRole) {
		writeError(w, http.StatusForbidden, "forbidden", "没有管理该账号成员的权限")
		return
	}

	var request updateAccountMemberRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.Role = strings.ToLower(strings.TrimSpace(request.Role))
	if !validAccountMemberRole(request.Role) {
		writeError(w, http.StatusBadRequest, "invalid_request", "账号成员角色无效")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Error("开始保存账号成员权限事务失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "保存账号成员权限失败，请稍后重试")
		return
	}
	defer tx.Rollback()

	var targetWorkspaceID, targetWorkspaceRole string
	err = tx.QueryRowContext(r.Context(), `
		SELECT a.workspace_id::text, wm.role
		FROM accounts a
		JOIN workspace_members wm ON wm.workspace_id = a.workspace_id
		WHERE a.account_id = $1 AND wm.user_id = $2::uuid
		FOR SHARE`,
		accountID, targetUserID).Scan(&targetWorkspaceID, &targetWorkspaceRole)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "not_found", "目标用户不是该工作区成员")
			return
		}
		s.logger.Error("查询账号成员所属工作区失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "保存账号成员权限失败，请稍后重试")
		return
	}
	if targetWorkspaceID != workspaceID {
		writeError(w, http.StatusForbidden, "forbidden", "目标用户不属于该账号工作区")
		return
	}
	if targetWorkspaceRole == "owner" || targetWorkspaceRole == "admin" {
		writeError(w, http.StatusConflict, "conflict", "所有者或管理员无需单独分配账号权限")
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO account_members (account_id, user_id, role)
		VALUES ($1, $2::uuid, $3)
		ON CONFLICT (account_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		accountID, targetUserID, request.Role)
	if err != nil {
		s.logger.Error("保存账号成员权限失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "保存账号成员权限失败，请稍后重试")
		return
	}

	var member accountMemberResponse
	var createdAt time.Time
	err = tx.QueryRowContext(r.Context(), `
		SELECT am.user_id::text, u.email, u.display_name, am.role, am.created_at
		FROM account_members am
		JOIN users u ON u.id = am.user_id
		WHERE am.account_id = $1 AND am.user_id = $2::uuid`,
		accountID, targetUserID).Scan(&member.UserID, &member.Email,
		&member.DisplayName, &member.Role, &createdAt)
	if err != nil {
		s.logger.Error("读取更新后的账号成员失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "保存账号成员权限失败，请稍后重试")
		return
	}
	member.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	if err := tx.Commit(); err != nil {
		s.logger.Error("提交账号成员权限事务失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "保存账号成员权限失败，请稍后重试")
		return
	}
	if err := s.writeAudit(r.Context(), userID, "account_member_role_updated", accountID, deviceID); err != nil {
		s.logger.Error("写入账号成员权限审计失败", "error", err.Error())
	}
	writeJSON(w, http.StatusOK, member)
}

func (s *Server) handleRemoveAccountMember(w http.ResponseWriter, r *http.Request) {
	userID, _, deviceID, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	accountID := r.PathValue("account_id")
	targetUserID := r.PathValue("user_id")
	if !validAccountID(accountID) || !looksLikeUUID(targetUserID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "成员标识无效")
		return
	}
	if _, role, ok := s.accountAccess(r.Context(), userID, accountID); !ok ||
		!canManageAccountMembers(role) {
		writeError(w, http.StatusForbidden, "forbidden", "没有管理该账号成员的权限")
		return
	}

	result, err := s.db.ExecContext(r.Context(), `
		DELETE FROM account_members
		WHERE account_id = $1 AND user_id = $2::uuid`, accountID, targetUserID)
	if err != nil {
		s.logger.Error("移除账号成员权限失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "移除账号成员权限失败，请稍后重试")
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		s.logger.Error("读取账号成员移除结果失败", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "移除账号成员权限失败，请稍后重试")
		return
	}
	if count == 0 {
		writeError(w, http.StatusNotFound, "not_found", "账号成员权限不存在")
		return
	}
	if err := s.writeAudit(r.Context(), userID, "account_member_removed", accountID, deviceID); err != nil {
		s.logger.Error("写入账号成员移除审计失败", "error", err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
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
	Overwrite     bool                      `json:"overwrite,omitempty"`
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
	expectedRevision, forceOverwrite, err := parseIfMatch(
		r.Header.Get("If-Match"))
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
	if currentRevision != expectedRevision && !forceOverwrite {
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
	auditAction := "snapshot_written"
	if forceOverwrite || request.Overwrite {
		auditAction = "snapshot_overwritten"
	}
	if err := s.writeAudit(r.Context(), userID, auditAction, accountID, deviceID); err != nil {
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
	// 先锁定账号，避免两个设备同时首次获取租约时触发唯一键冲突。
	// 与快照写入保持一致的加锁顺序：账号在前，租约在后。
	var lockedAccountID string
	if err := tx.QueryRowContext(r.Context(),
		`SELECT account_id FROM accounts WHERE account_id = $1 FOR UPDATE`, accountID).
		Scan(&lockedAccountID); err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "internal_error", "无法锁定账号租约")
		return
	}
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
		SELECT a.workspace_id::text,
		       CASE
		         WHEN wm.role IN ('owner', 'admin') THEN wm.role
		         WHEN wm.role = 'viewer' THEN 'viewer'
		         ELSE COALESCE(account_assignment.role, wm.role)
		       END
		FROM accounts a
		JOIN workspace_members wm ON wm.workspace_id = a.workspace_id
		LEFT JOIN account_members account_assignment
		  ON account_assignment.account_id = a.account_id
		 AND account_assignment.user_id = wm.user_id
		WHERE a.account_id = $1 AND wm.user_id = $2::uuid
		  AND (
			wm.role IN ('owner', 'admin')
			OR NOT EXISTS (
				SELECT 1 FROM account_members restricted
				WHERE restricted.account_id = a.account_id
			)
			OR EXISTS (
				SELECT 1 FROM account_members assigned
				WHERE assigned.account_id = a.account_id
				  AND assigned.user_id = $2::uuid
			)
		  )`,
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

func canManageAccountMembers(role string) bool {
	return role == "owner" || role == "admin"
}

func validAccountMemberRole(role string) bool {
	return role == "editor" || role == "viewer"
}

func validWorkspaceInviteRole(role string) bool {
	return role == "admin" || role == "editor" || role == "viewer"
}

func canInviteMember(inviterRole, invitedRole string) bool {
	if !validWorkspaceInviteRole(invitedRole) {
		return false
	}
	if inviterRole == "owner" {
		return true
	}
	return inviterRole == "admin" && invitedRole != "admin"
}

func canManageMember(actorRole, memberRole string) bool {
	if memberRole == "owner" {
		return false
	}
	return canInviteMember(actorRole, memberRole)
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
		// SaaS 仅允许同来源页面或原生控制台宿主嵌入，避免外部页面伪造桥接宿主。
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'self' chrome://fingerprint-manager")
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
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
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

func parseIfMatch(value string) (int64, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, fmt.Errorf("缺少 If-Match")
	}
	if value == "*" {
		return 0, true, nil
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision < 0 {
		return 0, false, fmt.Errorf("If-Match 无效")
	}
	return revision, false, nil
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

func validWorkspaceName(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character < ' ' || character == '\u007f' {
			return false
		}
	}
	return true
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

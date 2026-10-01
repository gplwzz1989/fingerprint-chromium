package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/auth"
)

type BootstrapUserInput struct {
	Email         string
	Password      string
	DisplayName   string
	WorkspaceName string
}

type BootstrapUserResult struct {
	UserID      string
	WorkspaceID string
}

func BootstrapUser(ctx context.Context, db *sql.DB, input BootstrapUserInput) (BootstrapUserResult, error) {
	normalized, err := normalizeBootstrapUserInput(input)
	if err != nil {
		return BootstrapUserResult{}, err
	}
	if db == nil {
		return BootstrapUserResult{}, errors.New("数据库连接无效")
	}

	passwordHash, err := auth.HashPassword(normalized.Password)
	if err != nil {
		return BootstrapUserResult{}, err
	}
	userID, err := newUUID()
	if err != nil {
		return BootstrapUserResult{}, errors.New("生成用户标识失败")
	}
	workspaceID, err := newUUID()
	if err != nil {
		return BootstrapUserResult{}, errors.New("生成工作区标识失败")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return BootstrapUserResult{}, errors.New("开始初始化事务失败")
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = $1)`, normalized.Email).
		Scan(&exists); err != nil {
		return BootstrapUserResult{}, errors.New("检查用户是否存在失败")
	}
	if exists {
		return BootstrapUserResult{}, errors.New("用户邮箱已存在")
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, display_name)
		VALUES ($1::uuid, $2, $3, $4)`,
		userID, normalized.Email, passwordHash, normalized.DisplayName); err != nil {
		return BootstrapUserResult{}, errors.New("创建用户失败")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO workspaces (id, name)
		VALUES ($1::uuid, $2)`, workspaceID, normalized.WorkspaceName); err != nil {
		return BootstrapUserResult{}, errors.New("创建工作区失败")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1::uuid, $2::uuid, 'owner')`, workspaceID, userID); err != nil {
		return BootstrapUserResult{}, errors.New("设置工作区所有者失败")
	}
	if err := tx.Commit(); err != nil {
		return BootstrapUserResult{}, errors.New("提交初始化事务失败")
	}

	return BootstrapUserResult{UserID: userID, WorkspaceID: workspaceID}, nil
}

func normalizeBootstrapUserInput(input BootstrapUserInput) (BootstrapUserInput, error) {
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.WorkspaceName = strings.TrimSpace(input.WorkspaceName)
	if len(input.Email) == 0 || len(input.Email) > 320 {
		return BootstrapUserInput{}, errors.New("邮箱格式无效")
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email {
		return BootstrapUserInput{}, errors.New("邮箱格式无效")
	}
	if len(input.Password) < 12 || len(input.Password) > 256 {
		return BootstrapUserInput{}, errors.New("密码长度必须在 12 到 256 个字符之间")
	}
	if len(input.DisplayName) > 128 {
		return BootstrapUserInput{}, errors.New("显示名称不能超过 128 个字符")
	}
	if len(input.WorkspaceName) == 0 || len(input.WorkspaceName) > 256 {
		return BootstrapUserInput{}, errors.New("工作区名称长度无效")
	}
	return input, nil
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

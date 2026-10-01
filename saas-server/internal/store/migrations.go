package store

import (
	"context"
	"database/sql"
	"fmt"
)

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS users (
        id UUID PRIMARY KEY,
        email TEXT NOT NULL UNIQUE,
        password_hash TEXT NOT NULL,
        display_name TEXT NOT NULL DEFAULT '',
        created_at TIMESTAMPTZ NOT NULL DEFAULT now()
    )`,
	`CREATE TABLE IF NOT EXISTS workspaces (
        id UUID PRIMARY KEY,
        name TEXT NOT NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT now()
    )`,
	`CREATE TABLE IF NOT EXISTS workspace_members (
        workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
        user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
        role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
        created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
        PRIMARY KEY (workspace_id, user_id)
    )`,
	`CREATE TABLE IF NOT EXISTS sessions (
        id UUID PRIMARY KEY,
        user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		device_id TEXT NOT NULL,
		device_name TEXT NOT NULL DEFAULT '',
		refresh_token_hash TEXT NOT NULL UNIQUE,
        expires_at TIMESTAMPTZ NOT NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
        revoked_at TIMESTAMPTZ
    )`,
	`CREATE TABLE IF NOT EXISTS accounts (
        account_id TEXT PRIMARY KEY CHECK (account_id ~ '^[A-Za-z0-9_-]+$'),
        workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
        name TEXT NOT NULL,
        labels JSONB NOT NULL DEFAULT '[]'::jsonb,
        revision BIGINT NOT NULL DEFAULT 0,
        created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
        updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
    )`,
	`CREATE TABLE IF NOT EXISTS account_snapshots (
        account_id TEXT NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
        revision BIGINT NOT NULL,
        schema_version INTEGER NOT NULL,
        device_id TEXT NOT NULL,
        envelope JSONB NOT NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
        PRIMARY KEY (account_id, revision)
    )`,
	`CREATE TABLE IF NOT EXISTS account_leases (
        lease_id UUID PRIMARY KEY,
        account_id TEXT NOT NULL UNIQUE REFERENCES accounts(account_id) ON DELETE CASCADE,
        device_id TEXT NOT NULL,
        expires_at TIMESTAMPTZ NOT NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT now()
    )`,
	`CREATE TABLE IF NOT EXISTS audit_events (
        event_id UUID PRIMARY KEY,
        user_id UUID REFERENCES users(id) ON DELETE SET NULL,
        account_id TEXT REFERENCES accounts(account_id) ON DELETE SET NULL,
        action TEXT NOT NULL,
        device_id TEXT,
        created_at TIMESTAMPTZ NOT NULL DEFAULT now()
    )`,
	`CREATE INDEX IF NOT EXISTS account_snapshots_created_idx
     ON account_snapshots (account_id, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS audit_events_account_idx
     ON audit_events (account_id, created_at DESC)`,
	`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS device_name TEXT NOT NULL DEFAULT ''`,
	`CREATE TABLE IF NOT EXISTS workspace_invites (
        invite_id UUID PRIMARY KEY,
        workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
        inviter_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
        email TEXT NOT NULL,
        role TEXT NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
        token_hash TEXT NOT NULL UNIQUE,
        expires_at TIMESTAMPTZ NOT NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
        accepted_at TIMESTAMPTZ,
        revoked_at TIMESTAMPTZ
    )`,
	`CREATE INDEX IF NOT EXISTS workspace_invites_lookup_idx
     ON workspace_invites (workspace_id, lower(email), expires_at)`,
}

func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version INTEGER PRIMARY KEY,
            applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
        )`); err != nil {
		return fmt.Errorf("创建迁移表失败: %w", err)
	}

	for index, migration := range migrations {
		version := index + 1
		var applied bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`,
			version).Scan(&applied); err != nil {
			return fmt.Errorf("读取迁移状态失败: %w", err)
		}
		if applied {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("开始数据库迁移失败: %w", err)
		}
		if _, err = tx.ExecContext(ctx, migration); err == nil {
			_, err = tx.ExecContext(ctx,
				`INSERT INTO schema_migrations (version) VALUES ($1)`, version)
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("执行数据库迁移 %d 失败: %w", version, err)
		}
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("提交数据库迁移 %d 失败: %w", version, err)
		}
	}
	return nil
}

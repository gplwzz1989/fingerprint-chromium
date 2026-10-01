package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/config"
	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/httpapi"
	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if len(os.Args) > 1 {
		if os.Args[1] != "bootstrap-user" || len(os.Args) != 2 {
			logger.Error("命令无效", "command", os.Args[1])
			os.Exit(2)
		}
		if err := runBootstrapUser(logger); err != nil {
			logger.Error("初始化首个用户失败", "reason", err.Error())
			os.Exit(1)
		}
		return
	}
	cfg, err := config.Load()
	if err != nil {
		logger.Error("读取服务配置失败", "error", err.Error())
		os.Exit(1)
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStartup()
	db, err := store.Open(startupContext, cfg.DatabaseURL)
	if err != nil {
		logger.Error("初始化数据库失败", "error", err.Error())
		os.Exit(1)
	}
	defer db.Close()
	if err := store.Migrate(startupContext, db); err != nil {
		logger.Error("数据库迁移失败", "error", err.Error())
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewServer(db, cfg, logger).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("SaaS 服务已启动", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("SaaS 服务异常退出", "error", err.Error())
			os.Exit(1)
		}
	}()

	stopContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-stopContext.Done()
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("SaaS 服务停止失败", "error", err.Error())
	}
}

func runBootstrapUser(logger *slog.Logger) error {
	databaseURL, err := config.LoadDatabaseURL()
	if err != nil {
		return err
	}
	password := os.Getenv("SAAS_BOOTSTRAP_PASSWORD")
	if strings.TrimSpace(os.Getenv("SAAS_BOOTSTRAP_EMAIL")) == "" ||
		password == "" || strings.TrimSpace(os.Getenv("SAAS_BOOTSTRAP_WORKSPACE")) == "" {
		return errors.New("必须配置 SAAS_BOOTSTRAP_EMAIL、SAAS_BOOTSTRAP_PASSWORD 和 SAAS_BOOTSTRAP_WORKSPACE")
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStartup()
	db, err := store.Open(startupContext, databaseURL)
	if err != nil {
		return errors.New("连接数据库失败")
	}
	defer db.Close()
	if err := store.Migrate(startupContext, db); err != nil {
		return errors.New("数据库迁移失败")
	}

	result, err := store.BootstrapUser(startupContext, db, store.BootstrapUserInput{
		Email:         os.Getenv("SAAS_BOOTSTRAP_EMAIL"),
		Password:      password,
		DisplayName:   os.Getenv("SAAS_BOOTSTRAP_DISPLAY_NAME"),
		WorkspaceName: os.Getenv("SAAS_BOOTSTRAP_WORKSPACE"),
	})
	if err != nil {
		return err
	}
	logger.Info("首个用户和工作区初始化完成",
		"user_id", result.UserID, "workspace_id", result.WorkspaceID)
	return nil
}

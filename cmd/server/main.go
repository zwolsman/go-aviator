package main

import (
	"log/slog"
	"os"

	"github.com/zwolsman/go-aviator/internal/auth"
	"github.com/zwolsman/go-aviator/internal/engine"
	sshsrv "github.com/zwolsman/go-aviator/internal/ssh"
	"github.com/zwolsman/go-aviator/internal/store"
)

func main() {
	dsn := getenv("DATABASE_URL", "postgres://crashgame:crashgame@localhost:5433/crashgame?sslmode=disable")
	addr := getenv("SSH_ADDR", ":2222")
	keyPath := getenv("HOST_KEY_PATH", "keys/host_ed25519")

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	sqlDB, queries, err := store.Open(dsn)
	if err != nil {
		slog.Error("database init failed", "err", err)
		os.Exit(1)
	}
	defer sqlDB.Close()
	slog.Info("database connected and migrations applied")

	idm := auth.New(queries)
	eng := engine.New(queries, sqlDB)

	srv := sshsrv.New(addr, keyPath, queries, sqlDB, idm, eng)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

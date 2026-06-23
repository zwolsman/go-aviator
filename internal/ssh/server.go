package sshsrv

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	cssh "github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	wishbt "github.com/charmbracelet/wish/bubbletea"
	"github.com/muesli/termenv"

	"github.com/zwolsman/go-aviator/internal/auth"
	dbpkg "github.com/zwolsman/go-aviator/internal/db"
	"github.com/zwolsman/go-aviator/internal/engine"
	"github.com/zwolsman/go-aviator/internal/tui"
)

// Server wraps the Wish SSH server.
type Server struct {
	addr    string
	keyPath string
	db      *dbpkg.Queries
	sqlDB   *sql.DB
	idm     *auth.Manager
	eng     *engine.Engine
}

// New creates a new SSH server.
func New(addr, keyPath string, db *dbpkg.Queries, sqlDB *sql.DB, idm *auth.Manager, eng *engine.Engine) *Server {
	return &Server{
		addr:    addr,
		keyPath: keyPath,
		db:      db,
		sqlDB:   sqlDB,
		idm:     idm,
		eng:     eng,
	}
}

// ListenAndServe starts the SSH server and blocks until a shutdown signal.
func (s *Server) ListenAndServe() error {
	srv, err := wish.NewServer(
		wish.WithAddress(s.addr),
		wish.WithHostKeyPath(s.keyPath),
		wish.WithPublicKeyAuth(s.publicKeyHandler()),
		wish.WithMiddleware(
			wishbt.Middleware(s.bubbleTeaHandler()),
		),
	)
	if err != nil {
		return fmt.Errorf("create ssh server: %w", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	slog.Info("SSH server listening", "addr", s.addr)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			slog.Error("ssh server error", "err", err)
		}
	}()

	<-done
	slog.Info("shutting down SSH server")
	return srv.Shutdown(context.Background())
}

// publicKeyHandler accepts any public key (the key is the identity).
func (s *Server) publicKeyHandler() cssh.PublicKeyHandler {
	return func(_ cssh.Context, _ cssh.PublicKey) bool {
		return true
	}
}

// bubbleTeaHandler returns the bubbletea handler for each SSH session.
func (s *Server) bubbleTeaHandler() wishbt.Handler {
	return func(sess cssh.Session) (tea.Model, []tea.ProgramOption) {
		key := sess.PublicKey()
		if key == nil {
			wish.Fatalln(sess, "public key auth required\r")
			return nil, nil
		}
		fp := auth.Fingerprint(key)

		displayName := sess.User()
		if displayName == "" {
			displayName = fp[:12]
		}

		player, alreadyActive, isNew, err := s.idm.Login(sess.Context(), fp, displayName)
		if err != nil {
			slog.Error("login failed", "fp", fp, "err", err)
			wish.Fatalf(sess, "login error: %v\r\n", err)
			return nil, nil
		}
		if alreadyActive {
			wish.Fatalln(sess, "Already connected from another session. Only one session per key is allowed.\r")
			return nil, nil
		}

		snapCh, unsub := s.eng.Subscribe(player.ID, displayName)

		// deregister on session close
		go func() {
			<-sess.Context().Done()
			unsub()
			s.idm.Logout(fp)
			slog.Info("session ended", "displayName", displayName)
		}()

		slog.Info("session started", "displayName", displayName, "balance", player.Balance)
		renderer := wishbt.MakeRenderer(sess)
		renderer.SetColorProfile(termenv.TrueColor)
		model := tui.New(player, s.db, s.eng, snapCh, unsub, renderer, isNew)
		return model, []tea.ProgramOption{tea.WithAltScreen()}
	}
}

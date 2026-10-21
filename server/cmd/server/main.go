// Command server runs the Pocket Arcade API.
//
// Configuration comes from the environment:
//
//	ADDR           listen address (default :8080)
//	DATABASE_URL   PostgreSQL DSN
//	REDIS_URL      Redis URL, e.g. redis://localhost:6379/0
//	SESSION_KEY    secret for signing sessions (required unless STORE=memory)
//	STORE          "memory" to run with no database or Redis
//	ALLOW_ORIGINS  comma separated CORS origins
//	STATIC_DIR     optional directory of built web assets to serve at /
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vaibenz/pocket-arcade/server/internal/api"
	"github.com/vaibenz/pocket-arcade/server/internal/session"
	"github.com/vaibenz/pocket-arcade/server/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &api.Server{Log: log, AllowOrigins: strings.Split(env("ALLOW_ORIGINS", "*"), ",")}
	key := []byte(os.Getenv("SESSION_KEY"))

	if env("STORE", "") == "memory" {
		m := store.NewMemory()
		srv.Durable, srv.Board = m, m
		if len(key) == 0 {
			key = make([]byte, 32)
			_, _ = rand.Read(key)
		}
		log.Info("using in memory store")
	} else {
		if len(key) < 16 {
			return errors.New("SESSION_KEY must be at least 16 bytes")
		}
		openCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		pg, err := store.OpenPostgres(openCtx, env("DATABASE_URL", "postgres://arcade:arcade@localhost:5432/arcade?sslmode=disable"))
		if err != nil {
			return err
		}
		defer pg.Close()
		rd, err := store.OpenRedis(openCtx, env("REDIS_URL", "redis://localhost:6379/0"))
		if err != nil {
			return err
		}
		defer rd.Close()
		srv.Durable, srv.Board = pg, rd
	}
	srv.Signer = session.NewSigner(key, 15*time.Minute)

	mux := http.NewServeMux()
	mux.Handle("/v1/", srv.Handler())
	mux.Handle("/healthz", srv.Handler())
	if dir := os.Getenv("STATIC_DIR"); dir != "" {
		mux.Handle("/", http.FileServer(http.Dir(dir)))
	}

	hs := &http.Server{
		Addr:              env("ADDR", ":8080"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	log.Info("listening", "addr", hs.Addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return hs.Shutdown(shutdown)
}

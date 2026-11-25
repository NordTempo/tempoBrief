// Command tempobrief is the tempoBrief server: it fetches feeds, groups and
// scores stories with the user's own model, sends briefs and serves the web
// app. `tempobrief healthcheck` probes a running instance for Docker's
// HEALTHCHECK (the image has no shell).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/NordTempo/tempoBrief/internal/api"
	"github.com/NordTempo/tempoBrief/internal/auth"
	"github.com/NordTempo/tempoBrief/internal/config"
	"github.com/NordTempo/tempoBrief/internal/deliver"
	"github.com/NordTempo/tempoBrief/internal/embed"
	"github.com/NordTempo/tempoBrief/internal/feeds"
	"github.com/NordTempo/tempoBrief/internal/pipeline"
	"github.com/NordTempo/tempoBrief/internal/settings"
	"github.com/NordTempo/tempoBrief/internal/store"
	"github.com/NordTempo/tempoBrief/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}
	log := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	db, err := store.Open(ctx, filepath.Join(cfg.DataDir, "tempobrief.db"))
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	authSvc, err := auth.New(ctx, cfg, db, log)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	// Hashing the password (argon2id, 19 MiB) is the biggest allocation at
	// startup; hand that memory back so idle memory stays small.
	debug.FreeOSMemory()

	st, err := settings.Load(ctx, db)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	deliverer := deliver.New(log, db.SetVote)
	deliverer.Configure(st)
	defer deliverer.Close()

	pipe := pipeline.New(pipeline.Deps{
		Config:   cfg,
		DB:       db,
		Log:      log,
		Fetcher:  feeds.NewFetcher(version),
		Embedder: embed.New(cfg.DataDir, cfg.EmbedModel, cfg.EmbedFile, log),
		Sender:   deliverer,
	})

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: api.New(api.Deps{
			Config: cfg, DB: db, Auth: authSvc, Log: log, Assets: web.Dist(), Version: version,
			Pipeline: pipe, Sender: deliverer, Configure: deliverer.Configure,
		}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	log.Info("tempoBrief started", "version", version, "listen", ln.Addr().String(), "auth", cfg.Auth, "public_url", cfg.PublicURL)

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	pipeDone := make(chan error, 1)
	go func() { pipeDone <- pipe.Run(ctx) }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	select {
	case err := <-pipeDone:
		return err
	case <-shutdownCtx.Done():
		return errors.New("pipeline did not stop within 10 s")
	}
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	l.UnmarshalText([]byte(level))
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

func healthcheck() int {
	listen := os.Getenv("TEMPOBRIEF_LISTEN")
	if listen == "" {
		listen = ":8080"
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

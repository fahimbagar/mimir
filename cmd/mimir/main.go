// Command mimir serves a private ebook library over HTTP.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"mimir/internal/auth"
	"mimir/internal/library"
	"mimir/internal/server"
	"mimir/internal/store"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	if err := run(); err != nil {
		slog.Error("mimir", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// Flags win over environment variables, which win over defaults.
	addr := flag.String("addr", env("MIMIR_ADDR", ":8080"), "listen address")
	books := flag.String("books", env("MIMIR_BOOKS", "./ebook"), "directory with .pdf and .epub files")
	static := flag.String("static", env("MIMIR_STATIC", "./web/dist"), "built frontend directory")
	dbPath := flag.String("db", env("MIMIR_DB", "./data/mimir.db"), "SQLite database file")
	baseURL := flag.String("base-url", env("MIMIR_BASE_URL", "https://example.dev"), "public origin, used for the Google redirect")
	trusted := flag.String("trusted-proxies", env("MIMIR_TRUSTED_PROXIES", ""), "comma-separated IPs/CIDRs whose X-Real-IP header is logged as the client address")
	flag.Parse()

	base, err := auth.ParseBaseURL(*baseURL)
	if err != nil {
		return err
	}
	proxies, err := server.ParseProxies(*trusted)
	if err != nil {
		return err
	}
	lib, err := library.New(*books)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o700); err != nil {
		return err
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := auth.New(context.WithoutCancel(ctx), auth.Config{
		BaseURL:       base,
		ClientID:      os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret:  os.Getenv("GOOGLE_CLIENT_SECRET"),
		AllowedEmails: []string{os.Getenv("MIMIR_ALLOWED_EMAILS")},
	}, st)
	if err != nil {
		return err
	}
	handler, err := server.New(lib, st, a, *static, base).TrustProxies(proxies).Routes()
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    32 << 10,
		// No WriteTimeout: large books can take a while to stream.
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("mimir listening", "addr", *addr, "books", lib.Root(), "db", *dbPath, "base", base)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

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
	"syscall"
	"time"

	"mimir/internal/library"
	"mimir/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	books := flag.String("books", "./ebook", "directory with .pdf and .epub files")
	static := flag.String("static", "./web/dist", "built frontend directory")
	flag.Parse()

	lib, err := library.New(*books)
	if err != nil {
		slog.Error("open library", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.New(lib, *static).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: large books can take a while to stream.
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("mimir listening", "addr", *addr, "books", lib.Root())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "err", err)
	}
}

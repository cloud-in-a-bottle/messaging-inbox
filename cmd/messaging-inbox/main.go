package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloud-in-a-bottle/messaging-service-spec/messaging"

	"github.com/cloud-in-a-bottle/messaging-inbox/internal/hub"
	"github.com/cloud-in-a-bottle/messaging-inbox/internal/server"
)

// Overridable at build time (-ldflags "-X main.listenAddr=:8081") to run beside a provider on the same machine.
var listenAddr = ":8080"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("Exiting", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	// Uses the shortname "messaging", as openhost.toml declares.
	client, err := messaging.NewClient()
	if err != nil {
		return err
	}
	// Uploads go through the proxy, so allow them time; event streams don't use this timeout.
	client.HTTP = &http.Client{Timeout: 5 * time.Minute}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	h := hub.New(client, log)
	go h.Run(ctx)
	srv := &http.Server{
		Addr:    listenAddr,
		Handler: server.New(server.Options{Hub: h, Log: log}),
		// Ends open event streams on shutdown.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.ListenAndServe()
	}()
	log.Info("Serving", "addr", listenAddr)
	select {
	case <-ctx.Done():
	case err = <-serveErr:
		return err
	}
	log.Info("Shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = srv.Shutdown(shutdownCtx); errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

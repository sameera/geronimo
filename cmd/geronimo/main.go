// Command geronimo runs the Geronimo web service.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/sameera/geronimo/internal/auth"
	"github.com/sameera/geronimo/internal/config"
	"github.com/sameera/geronimo/internal/gmail"
	"github.com/sameera/geronimo/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("geronimo stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath, time.Now)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.SetTokenKey(cfg.TokenKey); err != nil {
		return err
	}

	go func() {
		for {
			if err := st.PurgeExpired(context.Background()); err != nil {
				slog.Error("purge expired rows", "err", err)
			}
			time.Sleep(time.Hour)
		}
	}()

	svc := auth.New(auth.Options{
		Store:            st,
		Provider:         auth.NewGoogle(auth.GoogleIssuer, cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.RedirectURL()),
		Connector:        auth.NewGoogleConnect(auth.GoogleIssuer, cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.ConnectRedirectURL()),
		Profiler:         gmail.New(gmail.DefaultBaseURL, &http.Client{Timeout: 10 * time.Second}),
		BaseURL:          cfg.BaseURL,
		IdleLifetime:     cfg.IdleLifetime,
		AbsoluteLifetime: cfg.AbsoluteLifetime,
	})
	app := http.NewServeMux()

	srv := &http.Server{Addr: cfg.Addr, Handler: svc.Handler(app), ReadHeaderTimeout: 10 * time.Second}
	slog.Info("listening", "addr", cfg.Addr)
	return srv.ListenAndServe()
}

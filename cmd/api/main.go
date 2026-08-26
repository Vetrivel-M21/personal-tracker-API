// Command api runs the Aura backend: applies pending migrations, starts the
// HTTP server, and purges expired sessions once a day until shutdown.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"aura/server/internal/api"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	loadDotEnv(".env")

	cfg, err := api.LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := api.RunMigrations(cfg.DatabaseURL); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	pool, err := api.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db pool: %v", err)
	}
	defer pool.Close()

	server := api.NewServer(cfg, pool)

	httpServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      server.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go runSessionCleanupTicker(ctx, server)

	go func() {
		log.Printf("listening on :%s", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

// loadDotEnv reads a plain KEY=VALUE .env file, if present, into the process
// environment - skipping blank lines, comments, and any key that's already
// set so real environment variables (e.g. docker-compose in production)
// always take priority over the file.
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		os.Setenv(key, strings.TrimSpace(value))
	}
}

// runSessionCleanupTicker purges sessions expired more than 30 days ago,
// once at startup and then once every 24 hours until ctx is cancelled.
func runSessionCleanupTicker(ctx context.Context, s *api.Server) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	purge := func() {
		if err := s.PurgeExpiredSessions(ctx); err != nil {
			log.Printf("session cleanup: %v", err)
		}
	}
	purge()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}

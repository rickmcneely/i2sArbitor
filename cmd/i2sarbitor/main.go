package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/zditech/i2sarbitor/internal/api"
	"github.com/zditech/i2sarbitor/internal/arbiter"
	"github.com/zditech/i2sarbitor/internal/config"
)

func main() {
	// Setup logging
	log.Logger = zerolog.New(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}).With().Timestamp().Logger()

	log.Info().Msg("i2sArbitor starting...")

	// Load configuration
	cfg, err := config.Load("/etc/i2sarbitor/i2sarbitor.yaml")
	if err != nil {
		log.Warn().Err(err).Msg("failed to load config, using defaults")
		cfg = config.Default()
	}

	// Create arbiter
	arb := arbiter.New(cfg)

	// Start monitoring services
	arb.StartMonitoring()

	// Create and start API server
	server := api.NewServer(cfg, arb)
	go func() {
		if err := server.Start(); err != nil {
			log.Fatal().Err(err).Msg("failed to start API server")
		}
	}()

	log.Info().Int("port", cfg.APIPort).Msg("i2sArbitor running")

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	arb.StopMonitoring()
	server.Shutdown(ctx)

	log.Info().Msg("i2sArbitor stopped")
}

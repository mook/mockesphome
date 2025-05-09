package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/coreos/go-systemd/v22/daemon"
	_ "github.com/mook/mockesphome/api"
	_ "github.com/mook/mockesphome/bluetooth_proxy"
	"github.com/mook/mockesphome/components"
)

var (
	flagConfig  = flag.String("config", "config.yaml", "configuration file")
	flagVerbose = flag.Bool("verbose", false, "emit extra logging")
)

func run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	flag.Parse()

	if *flagVerbose {
		handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
		slog.SetDefault(slog.New(handler))
	}

	configFile, err := os.Open(*flagConfig)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	defer configFile.Close()
	if err := components.LoadConfiguration(ctx, configFile); err != nil {
		return err
	}
	if err := components.StartComponents(ctx); err != nil {
		return err
	}

	if _, err := daemon.SdNotify(true, daemon.SdNotifyReady); err != nil {
		return err
	}

	// Wait for SIGINT / SIGTERM
	slog.InfoContext(ctx, "started; press Ctrl+C to exit")
	<-ctx.Done()

	slog.InfoContext(ctx, "shutting down...")

	return nil
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		slog.ErrorContext(ctx, "Fatal error", "error", err)
		os.Exit(1)
	}
}

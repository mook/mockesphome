package main

import (
	"compress/gzip"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

var (
	verbose = flag.Bool("verbose", false, "extra logging")
)

func run(ctx context.Context) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	noticeFile, err := os.Create(filepath.Join(cwd, "doc", "notice.txt"))
	if err != nil {
		return fmt.Errorf("failed to create license notice file: %w", err)
	}
	compressedNoticeFile, err := os.Create(filepath.Join(cwd, "doc", "notice.txt.gz"))
	if err != nil {
		return fmt.Errorf("failed to create compressed license notice file: %w", err)
	}
	gzipWriter := gzip.NewWriter(compressedNoticeFile)
	defer gzipWriter.Close()
	noticeWriter := io.MultiWriter(noticeFile, gzipWriter)
	if err := generateLicenseNotice(ctx, noticeWriter); err != nil {
		return err
	}
	if err := noticeFile.Close(); err != nil {
		return err
	}
	docsFile, err := os.Create(filepath.Join(cwd, "doc", "index.md"))
	if err != nil {
		return fmt.Errorf("failed to create documentation file: %w", err)
	}
	if err := generateDocs(ctx, docsFile); err != nil {
		return err
	}
	if err := docsFile.Close(); err != nil {
		return err
	}

	return nil
}

func main() {
	ctx := context.Background()
	flag.Parse()

	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	slog.SetDefault(slog.New(handler))

	if err := run(ctx); err != nil {
		slog.ErrorContext(ctx, "failed to generate", "error", err)
		os.Exit(1)
	}
}

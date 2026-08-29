package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/MarBarb/selfsend/internal/server"
)

var version = "dev"

func main() {
	config := server.Config{
		ListenAddr:    env("SELFSEND_LISTEN", ":8080"),
		DataDir:       env("SELFSEND_DATA_DIR", "./data"),
		AdminPassword: os.Getenv("SELFSEND_ADMIN_PASSWORD"),
		MaxUploadSize: envInt64("SELFSEND_MAX_UPLOAD_SIZE", 20<<30),
		TrustProxy:    envBool("SELFSEND_TRUST_PROXY", false),
		Version:       version,
		CanonicalURL:  os.Getenv("SELFSEND_CANONICAL_URL"),
		Discovery:     envBool("SELFSEND_DISCOVERY", true),
	}

	flag.StringVar(&config.ListenAddr, "listen", config.ListenAddr, "HTTP listen address")
	flag.StringVar(&config.DataDir, "data", config.DataDir, "data directory")
	flag.Int64Var(&config.MaxUploadSize, "max-upload-size", config.MaxUploadSize, "maximum upload size in bytes")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := server.Run(config, logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

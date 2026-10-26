package main

import (
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Skpow1234/Shugo/internal/api/graphql"
	"github.com/Skpow1234/Shugo/internal/app/fileserver"
	"github.com/Skpow1234/Shugo/internal/crypto"
	"github.com/Skpow1234/Shugo/internal/peer"
	"github.com/Skpow1234/Shugo/internal/storage"
	netp2p "github.com/Skpow1234/Shugo/internal/transport/p2p"
)

func main() {
	// Parse command line flags
	var (
		port             = flag.Int("port", 8080, "Port to listen on")
		storageRoot      = flag.String("storage", "./storage", "Storage root directory")
		bootstrapNodes   = flag.String("bootstrap", "", "Comma-separated list of bootstrap nodes")
		enablePlayground = flag.Bool("playground", false, "Enable GraphQL Playground")
		logLevel         = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	)
	flag.Parse()

	// Set up logging
	logger := setupLogger(*logLevel)

	// Initialize key manager
	keyManager, err := crypto.NewKeyManager()
	if err != nil {
		logger.Error("Failed to create key manager", "error", err)
		os.Exit(1)
	}

	// Initialize transport
	transport := netp2p.NewTCPTransport(netp2p.TCPTransportOpts{
		ListenAddr: ":3000", // Default transport port
		OnPeer:     nil,     // Will be set by fileserver
		OnStream:   nil,     // Will be set by fileserver
	})

	// Initialize fileserver
	opts := fileserver.Options{
		ID:                "",
		EncKey:            nil,
		KeyManager:        keyManager,
		StorageRoot:       *storageRoot,
		PathTransformFunc: storage.CASPathTransformFunc,
		Transport:         transport,
		BootstrapNodes:    parseBootstrapNodes(*bootstrapNodes),
		ResourceLimits:    peer.DefaultResourceLimits(),
	}

	server := fileserver.New(opts)
	transport.OnPeer = server.OnPeer
	transport.OnStream = server.OnStream

	// Initialize GraphQL server
	authToken := os.Getenv("SHUGO_AUTH_TOKEN")
	if authToken == "" {
		logger.Error("SHUGO_AUTH_TOKEN is required")
		os.Exit(1)
	}

	config := &graphql.Config{
		Port:             *port,
		PlaygroundPath:   "/playground",
		GraphQLPath:      "/graphql",
		AllowedOrigins:   nil,
		EnablePlayground: *enablePlayground,
		AuthToken:        authToken,
	}

	graphqlServer := graphql.NewServer(server, config)

	// Start the fileserver
	go func() {
		if err := server.Start(); err != nil {
			logger.Error("Failed to start fileserver", "error", err)
			os.Exit(1)
		}
	}()

	// Start the GraphQL server
	go func() {
		if err := graphqlServer.Start(config); err != nil {
			logger.Error("Failed to start GraphQL server", "error", err)
			os.Exit(1)
		}
	}()

	logger.Info("Shugo GraphQL server started",
		"port", *port,
		"storage", *storageRoot,
		"playground", *enablePlayground,
	)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down...")

	// Graceful shutdown
	if err := graphqlServer.Stop(); err != nil {
		logger.Error("Error stopping GraphQL server", "error", err)
	}

	server.Stop()

	logger.Info("Server stopped")
}

func setupLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	handler := slog.NewJSONHandler(os.Stdout, opts)
	return slog.New(handler)
}

func parseBootstrapNodes(nodes string) []string {
	if nodes == "" {
		return nil
	}

	var result []string
	// Simple comma-separated parsing
	// TODO: Implement proper parsing with validation
	for _, node := range strings.Split(nodes, ",") {
		if node != "" {
			result = append(result, strings.TrimSpace(node))
		}
	}
	return result
}

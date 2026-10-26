package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Skpow1234/Shugo/internal/api/grpc"
	"github.com/Skpow1234/Shugo/internal/app/fileserver"
	"github.com/Skpow1234/Shugo/internal/crypto"
	"github.com/Skpow1234/Shugo/internal/peer"
	"github.com/Skpow1234/Shugo/internal/storage"
	netp2p "github.com/Skpow1234/Shugo/internal/transport/p2p"
)

func main() {
	port := flag.String("port", "8082", "HTTP/JSON API port (temporary gRPC facade)")
	authToken := flag.String("auth-token", "", "Authentication token, or set SHUGO_AUTH_TOKEN")
	listen := flag.String("listen", ":3020", "P2P listen address for embedded fileserver")
	storageRoot := flag.String("storage", "shugo-grpc-storage", "Storage root")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	keyManager, err := crypto.NewKeyManager()
	if err != nil {
		logger.Error("key manager", "error", err)
		os.Exit(1)
	}
	nodeID := crypto.GenerateID()
	tr := netp2p.NewTCPTransport(netp2p.TCPTransportOpts{
		ListenAddr:    *listen,
		HandshakeFunc: netp2p.AuthenticatedHandshakeFunc(nodeID),
		Decoder:       netp2p.LengthPrefixedDecoder{},
	})
	fsServer := fileserver.New(fileserver.Options{
		ID:                nodeID,
		KeyManager:        keyManager,
		StorageRoot:       *storageRoot,
		PathTransformFunc: storage.CASPathTransformFunc,
		Transport:         tr,
		ResourceLimits:    peer.DefaultResourceLimits(),
	})
	tr.OnPeer = fsServer.OnPeer
	tr.OnStream = fsServer.OnStream
	go func() {
		if err := fsServer.Start(); err != nil {
			logger.Error("fileserver error", "error", err)
		}
	}()

	token := *authToken
	if token == "" {
		token = os.Getenv("SHUGO_AUTH_TOKEN")
	}
	if token == "" {
		logger.Error("authentication token required: set --auth-token or SHUGO_AUTH_TOKEN")
		os.Exit(1)
	}
	config := grpc.DefaultConfig()
	config.Port = ":" + *port
	config.AuthToken = token
	server := grpc.NewServer(config, logger, fsServer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logger.Info("Received shutdown signal", "signal", sig)
		cancel()
		fsServer.Stop()
		if err := server.Stop(); err != nil {
			logger.Error("Error stopping server", "error", err)
		}
	}()

	logger.Info("Starting Shugo gRPC/HTTP API", "port", config.Port)
	if err := server.Start(); err != nil {
		logger.Error("Server error", "error", err)
		os.Exit(1)
	}
	<-ctx.Done()
	logger.Info("Server shutdown complete")
}

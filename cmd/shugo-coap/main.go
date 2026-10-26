package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Skpow1234/Shugo/internal/api/coap"
	fs "github.com/Skpow1234/Shugo/internal/app/fileserver"
	"github.com/Skpow1234/Shugo/internal/crypto"
	"github.com/Skpow1234/Shugo/internal/peer"
	"github.com/Skpow1234/Shugo/internal/storage"
	netp2p "github.com/Skpow1234/Shugo/internal/transport/p2p"
)

func main() {
	// Parse command line flags
	var (
		port       = flag.Int("port", 5683, "CoAP server port")
		host       = flag.String("host", "localhost", "CoAP server host")
		listenAddr = flag.String("listen", ":3003", "P2P listen address")
		verbose    = flag.Bool("verbose", false, "Enable verbose logging")
		enableDTLS = flag.Bool("enable-dtls", false, "Enable DTLS security")
		dtlsPort   = flag.Int("dtls-port", 5684, "DTLS CoAP server port")
	)
	flag.Parse()

	// Setup logging
	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Create file server instance (simplified for CoAP API)
	fileServer := createFileServer(*listenAddr, logger)

	// Create CoAP server configuration
	serverConfig := &coap.ServerConfig{
		Port:           *port,
		Host:           *host,
		EnableDTLS:     *enableDTLS,
		DTLSPort:       *dtlsPort,
		MaxConnections: 1000,
		MaxMessageSize: 1024, // CoAP is designed for small messages
		BlockSize:      64,   // Default block size for block-wise transfers
		MaxAge:         60,   // Default max age for responses
		EnableObserve:  true, // Enable observation patterns
		ObserveTimeout: 30 * time.Second,
	}

	// Create CoAP server
	server := coap.NewServer(fileServer, serverConfig, logger)

	// Start CoAP server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start UDP listener
	go func() {
		addr := fmt.Sprintf("%s:%d", *host, *port)
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			logger.Error("Failed to resolve UDP address", "error", err, "addr", addr)
			os.Exit(1)
		}

		conn, err := net.ListenUDP("udp", udpAddr)
		if err != nil {
			logger.Error("Failed to start CoAP UDP listener", "error", err, "addr", addr)
			os.Exit(1)
		}
		defer func() {
			if err := conn.Close(); err != nil {
				logger.Error("Failed to close connection", "error", err)
			}
		}()

		logger.Info("Starting CoAP server",
			"host", *host,
			"port", *port,
			"protocol", "UDP",
		)

		if err := server.ServeUDP(ctx, conn); err != nil {
			logger.Error("CoAP UDP server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Start DTLS listener if enabled
	if *enableDTLS {
		go func() {
			dtlsAddr := fmt.Sprintf("%s:%d", *host, *dtlsPort)
			logger.Info("Starting CoAP over DTLS",
				"host", *host,
				"port", *dtlsPort,
				"protocol", "DTLS",
			)

			if err := server.ServeDTLS(ctx, dtlsAddr); err != nil {
				logger.Error("CoAP DTLS server failed", "error", err)
				os.Exit(1)
			}
		}()
	}

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down CoAP server...")

	// Cancel context to stop all listeners
	cancel()

	// Give some time for graceful shutdown
	time.Sleep(2 * time.Second)

	logger.Info("CoAP server stopped")
}

// createFileServer creates a file server instance for the CoAP API
func createFileServer(listenAddr string, logger *slog.Logger) *fs.Server {
	// Generate a unique node ID for this server
	nodeID := crypto.GenerateID()

	tcptransportOpts := netp2p.TCPTransportOpts{
		ListenAddr:    listenAddr,
		HandshakeFunc: netp2p.AuthenticatedHandshakeFunc(nodeID),
		Decoder:       netp2p.LengthPrefixedDecoder{},
	}
	tcpTransport := netp2p.NewTCPTransport(tcptransportOpts)
	encKey, err := crypto.NewEncryptionKey()
	if err != nil {
		logger.Error("failed to generate encryption key", "error", err)
		os.Exit(1)
	}
	fileServerOpts := fs.Options{
		ID:                nodeID,
		EncKey:            encKey,
		StorageRoot:       storage.SanitizeStorageRootFromAddr(listenAddr),
		PathTransformFunc: storage.CASPathTransformFunc,
		Transport:         tcpTransport,
		BootstrapNodes:    []string{}, // No bootstrap nodes for CoAP API
		ResourceLimits:    peer.DefaultResourceLimits(),
	}
	s := fs.New(fileServerOpts)
	tcpTransport.OnPeer = s.OnPeer
	tcpTransport.OnStream = s.OnStream
	return s
}

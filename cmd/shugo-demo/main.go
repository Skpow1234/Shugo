package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"time"

	fs "github.com/Skpow1234/Shugo/internal/app/fileserver"
	"github.com/Skpow1234/Shugo/internal/crypto"
	"github.com/Skpow1234/Shugo/internal/logging"
	"github.com/Skpow1234/Shugo/internal/peer"
	"github.com/Skpow1234/Shugo/internal/storage"
	netp2p "github.com/Skpow1234/Shugo/internal/transport/p2p"
)

func main() {
	// Parse command line flags
	var (
		targetNode     = flag.String("target", "localhost:5000", "Target node to connect to")
		bootstrapNodes = flag.String("bootstrap", "", "Comma-separated list of bootstrap node addresses")
		logLevel       = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		iterations     = flag.Int("iterations", 20, "Number of store/get iterations to run")
	)
	flag.Parse()

	// Configure structured logging
	logging.ConfigureLogger(*logLevel)
	slog.Info("starting Shugo demo client",
		"target_node", *targetNode,
		"bootstrap_nodes", *bootstrapNodes,
		"iterations", *iterations,
		"log_level", *logLevel)

	// Create a demo client that connects to the network
	client := createDemoClient(*targetNode, *bootstrapNodes)

	// Run the demo operations
	runDemo(client, *iterations)
}

func createDemoClient(targetNode, bootstrapNodes string) *fs.Server {
	// Generate a unique node ID for this client
	nodeID := crypto.GenerateID()

	// Create transport for the client
	tcptransportOpts := netp2p.TCPTransportOpts{
		ListenAddr:    ":0", // Use random port for client
		HandshakeFunc: netp2p.AuthenticatedHandshakeFunc(nodeID),
		Decoder:       netp2p.LengthPrefixedDecoder{},
	}
	tcpTransport := netp2p.NewTCPTransport(tcptransportOpts)

	// Parse bootstrap nodes
	var bootstrapList []string
	if bootstrapNodes != "" {
		bootstrapList = []string{bootstrapNodes}
	}

	encKey, err := crypto.NewEncryptionKey()
	if err != nil {
		log.Fatalf("encryption key: %v", err)
	}
	fileServerOpts := fs.Options{
		ID:                nodeID,
		EncKey:            encKey,
		StorageRoot:       "demo-client-data",
		PathTransformFunc: storage.CASPathTransformFunc,
		Transport:         tcpTransport,
		BootstrapNodes:    bootstrapList,
		ResourceLimits:    peer.DefaultResourceLimits(),
	}
	s := fs.New(fileServerOpts)
	tcpTransport.OnPeer = s.OnPeer
	tcpTransport.OnStream = s.OnStream
	return s
}

func runDemo(client *fs.Server, iterations int) {
	// Start the client in a goroutine
	go func() {
		if err := client.Start(); err != nil {
			log.Fatal("failed to start client:", err)
		}
	}()

	// Give the client time to start and connect
	time.Sleep(2 * time.Second)

	// Create a context with timeout for the operations
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slog.Info("starting demo operations", "iterations", iterations)

	// Run store/get operations
	for i := 0; i < iterations; i++ {
		key := fmt.Sprintf("demo_file_%d.txt", i)
		data := fmt.Sprintf("Demo data for file %d - timestamp: %s", i, time.Now().Format(time.RFC3339))

		// Store the file
		slog.Info("storing file", "key", key, "iteration", i+1)
		if err := client.Store(ctx, key, bytes.NewReader([]byte(data))); err != nil {
			slog.Error("failed to store file", "key", key, "error", err)
			continue
		}

		// Get the file back
		slog.Info("retrieving file", "key", key, "iteration", i+1)
		reader, err := client.Get(ctx, key)
		if err != nil {
			slog.Error("failed to get file", "key", key, "error", err)
			continue
		}

		// Read and verify the data
		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(reader); err != nil {
			slog.Error("failed to read file data", "key", key, "error", err)
			continue
		}

		retrievedData := buf.String()
		if retrievedData == data {
			slog.Info("demo operation successful", "key", key, "iteration", i+1)
		} else {
			slog.Error("data mismatch", "key", key, "expected", data, "got", retrievedData)
		}

		// Small delay between operations
		time.Sleep(100 * time.Millisecond)
	}

	slog.Info("demo completed", "total_iterations", iterations)
}

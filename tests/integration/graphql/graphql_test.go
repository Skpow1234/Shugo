package graphql_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Skpow1234/Shugo/internal/api/graphql"
	"github.com/Skpow1234/Shugo/internal/app/fileserver"
	"github.com/Skpow1234/Shugo/internal/crypto"
	"github.com/Skpow1234/Shugo/internal/peer"
	"github.com/Skpow1234/Shugo/internal/storage"
	netp2p "github.com/Skpow1234/Shugo/internal/transport/p2p"
)

func TestGraphQLServerHealth(t *testing.T) {
	// Initialize key manager
	keyManager, err := crypto.NewKeyManager()
	if err != nil {
		t.Fatalf("Failed to create key manager: %v", err)
	}

	// Initialize transport
	transport := netp2p.NewTCPTransport(netp2p.TCPTransportOpts{
		ListenAddr: ":0", // Use random port for testing
		OnPeer:     nil,
		OnStream:   nil,
	})

	// Initialize fileserver
	opts := fileserver.Options{
		ID:                "test-node",
		EncKey:            nil,
		KeyManager:        keyManager,
		StorageRoot:       "./test-storage",
		PathTransformFunc: storage.CASPathTransformFunc,
		Transport:         transport,
		BootstrapNodes:    nil,
		ResourceLimits:    peer.DefaultResourceLimits(),
	}

	server := fileserver.New(opts)
	transport.OnPeer = server.OnPeer
	transport.OnStream = server.OnStream

	// Initialize GraphQL server
	config := &graphql.Config{
		Port:             0, // Use random port for testing
		PlaygroundPath:   "/playground",
		GraphQLPath:      "/graphql",
		AllowedOrigins:   []string{"*"},
		EnablePlayground: true,
		AuthToken:        "test-token",
	}

	graphqlServer := graphql.NewServer(server, config)

	// Create a test server
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			graphqlServer.HealthHandler(w, r)
		case "/graphql":
			graphqlServer.GraphQLHandler(w, r)
		case "/playground":
			graphqlServer.PlaygroundHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	// Test health endpoint
	t.Run("HealthEndpoint", func(t *testing.T) {
		resp, err := http.Get(testServer.URL + "/health")
		if err != nil {
			t.Fatalf("Failed to make health request: %v", err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var health map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
			t.Fatalf("Failed to decode health response: %v", err)
		}

		if health["status"] != "healthy" {
			t.Errorf("Expected status 'healthy', got %v", health["status"])
		}
	})

	// Test GraphQL endpoint
	t.Run("GraphQLEndpoint", func(t *testing.T) {
		query := `{"query": "{ health { status timestamp } }"}`
		req, err := http.NewRequest("POST", testServer.URL+"/graphql", bytes.NewBufferString(query))
		if err != nil {
			t.Fatalf("Failed to create GraphQL request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to make GraphQL request: %v", err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var response graphql.GraphQLResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode GraphQL response: %v", err)
		}

		if response.Data == nil {
			t.Error("Expected data in response")
		}
	})

	// Test playground endpoint
	t.Run("PlaygroundEndpoint", func(t *testing.T) {
		req, err := http.NewRequest("GET", testServer.URL+"/playground", nil)
		if err != nil {
			t.Fatalf("Failed to create playground request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer test-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to make playground request: %v", err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType != "text/html" {
			t.Errorf("Expected Content-Type 'text/html', got %s", contentType)
		}
	})

	// Clean up
	server.Stop()
}

func TestGraphQLServerCORS(t *testing.T) {
	// Initialize key manager
	keyManager, err := crypto.NewKeyManager()
	if err != nil {
		t.Fatalf("Failed to create key manager: %v", err)
	}

	// Initialize transport
	transport := netp2p.NewTCPTransport(netp2p.TCPTransportOpts{
		ListenAddr: ":0",
		OnPeer:     nil,
		OnStream:   nil,
	})

	// Initialize fileserver
	opts := fileserver.Options{
		ID:                "test-node",
		EncKey:            nil,
		KeyManager:        keyManager,
		StorageRoot:       "./test-storage",
		PathTransformFunc: storage.CASPathTransformFunc,
		Transport:         transport,
		BootstrapNodes:    nil,
		ResourceLimits:    peer.DefaultResourceLimits(),
	}

	server := fileserver.New(opts)
	transport.OnPeer = server.OnPeer
	transport.OnStream = server.OnStream

	// Initialize GraphQL server
	config := &graphql.Config{
		Port:             0,
		PlaygroundPath:   "/playground",
		GraphQLPath:      "/graphql",
		AllowedOrigins:   []string{"*"},
		EnablePlayground: true,
		AuthToken:        "test-token",
	}

	graphqlServer := graphql.NewServer(server, config)

	// Create a test server
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		graphqlServer.CORSMiddleware(graphqlServer.HealthHandler)(w, r)
	}))
	defer testServer.Close()

	// Test CORS headers
	t.Run("CORSHeaders", func(t *testing.T) {
		req, err := http.NewRequest("OPTIONS", testServer.URL+"/health", nil)
		if err != nil {
			t.Fatalf("Failed to create OPTIONS request: %v", err)
		}
		req.Header.Set("Origin", "https://app.example")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to make OPTIONS request: %v", err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for OPTIONS request, got %d", resp.StatusCode)
		}

		corsHeaders := []string{
			"Access-Control-Allow-Origin",
			"Access-Control-Allow-Methods",
			"Access-Control-Allow-Headers",
		}

		for _, header := range corsHeaders {
			if resp.Header.Get(header) == "" {
				t.Errorf("Expected CORS header %s to be set", header)
			}
		}
	})

	// Clean up
	server.Stop()
}

package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Skpow1234/Shugo/internal/api/graphql/resolvers"
	"github.com/Skpow1234/Shugo/internal/api/graphql/subscriptions"
	"github.com/Skpow1234/Shugo/internal/app/fileserver"
	"github.com/Skpow1234/Shugo/internal/httpsecure"
	"github.com/Skpow1234/Shugo/internal/websocket"
)

// Server represents the GraphQL server
type Server struct {
	server               *fileserver.Server
	resolver             resolvers.Resolver
	config               *Config
	logger               *slog.Logger
	hub                  *websocket.Hub
	subscriptionManager  *websocket.SubscriptionManager
	subscriptionResolver *subscriptions.SubscriptionResolver
	startTime            time.Time
}

// Config holds the configuration for the GraphQL server
type Config struct {
	Port             int
	PlaygroundPath   string
	GraphQLPath      string
	WebSocketPath    string
	AllowedOrigins   []string
	EnablePlayground bool
	EnableWebSocket  bool
	AuthToken        string
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Port:             8080,
		PlaygroundPath:   "/playground",
		GraphQLPath:      "/graphql",
		WebSocketPath:    "/ws",
		AllowedOrigins:   nil,
		EnablePlayground: false,
		EnableWebSocket:  true,
		AuthToken:        os.Getenv("SHUGO_AUTH_TOKEN"),
	}
}

// NewServer creates a new GraphQL server
func NewServer(fileserver *fileserver.Server, config *Config) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	hub := websocket.NewHub(logger)
	subscriptionManager := websocket.NewSubscriptionManager(hub, logger)
	subscriptionResolver := subscriptions.NewSubscriptionResolver(hub, subscriptionManager, logger)

	server := &Server{
		server:               fileserver,
		resolver:             resolvers.NewResolver(fileserver),
		config:               config,
		logger:               logger,
		hub:                  hub,
		subscriptionManager:  subscriptionManager,
		subscriptionResolver: subscriptionResolver,
		startTime:            time.Now().UTC(),
	}

	// Start the WebSocket hub
	go hub.Run(context.Background())

	return server
}

// Start starts the GraphQL server
func (s *Server) Start(config *Config) error {
	if config == nil {
		config = s.config
	}
	if config == nil {
		config = DefaultConfig()
	}
	s.config = config

	mux := http.NewServeMux()

	mux.HandleFunc(config.GraphQLPath, s.CORSMiddleware(s.GraphQLHandler))

	if config.EnableWebSocket {
		wsHandler := websocket.NewGraphQLSubscriptionHandler(s.hub, s.logger)
		mux.HandleFunc(config.WebSocketPath, s.CORSMiddleware(s.requireAuth(wsHandler.ServeHTTP)))
	}

	if config.EnablePlayground {
		mux.HandleFunc(config.PlaygroundPath, s.CORSMiddleware(s.PlaygroundHandler))
	}

	mux.HandleFunc("/health", s.HealthHandler)
	mux.HandleFunc("/metrics", s.CORSMiddleware(s.MetricsHandler))

	s.logger.Info("Starting GraphQL server",
		"port", config.Port,
		"playground", config.PlaygroundPath,
		"graphql", config.GraphQLPath,
		"websocket", config.WebSocketPath,
		"websocketEnabled", config.EnableWebSocket,
	)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", config.Port),
		Handler:           mux,
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if s.config.AuthToken == "" {
		return fmt.Errorf("SHUGO_AUTH_TOKEN is required")
	}
	return httpsecure.ListenAndServe(server)
}

// CORSMiddleware adds CORS headers to responses
func (s *Server) CORSMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpsecure.SetCORS(w, r, s.config.AllowedOrigins)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	}
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !httpsecure.AuthorizeBearer(r.Header.Get("Authorization"), s.config.AuthToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// GraphQLRequest represents a GraphQL request
type GraphQLRequest struct {
	Query         string                 `json:"query"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
	OperationName string                 `json:"operationName,omitempty"`
}

// GraphQLResponse represents a GraphQL response
type GraphQLResponse struct {
	Data   interface{}    `json:"data,omitempty"`
	Errors []GraphQLError `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error
type GraphQLError struct {
	Message    string                 `json:"message"`
	Locations  []ErrorLocation        `json:"locations,omitempty"`
	Path       []interface{}          `json:"path,omitempty"`
	Extensions map[string]interface{} `json:"extensions,omitempty"`
}

// ErrorLocation represents the location of an error
type ErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// GraphQLHandler handles GraphQL requests via the fileserver-backed resolver.
// This is a lightweight dispatcher (full schema engine still pending).
func (s *Server) GraphQLHandler(w http.ResponseWriter, r *http.Request) {
	if !httpsecure.AuthorizeBearer(r.Header.Get("Authorization"), s.config.AuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GraphQLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	data, err := s.dispatchGraphQL(r.Context(), req)
	response := GraphQLResponse{Data: data}
	if err != nil {
		response.Errors = []GraphQLError{{Message: err.Error()}}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) dispatchGraphQL(ctx context.Context, req GraphQLRequest) (map[string]interface{}, error) {
	q := req.Query
	switch {
	case containsOp(q, "health"):
		h, err := s.resolver.Health(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"health": h}, nil
	case containsOp(q, "files"):
		files, err := s.resolver.Files(ctx, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"files": files}, nil
	case containsOp(q, "nodes"):
		nodes, err := s.resolver.Nodes(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"nodes": nodes}, nil
	case containsOp(q, "peerNetwork"):
		net, err := s.resolver.PeerNetwork(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"peerNetwork": net}, nil
	case containsOp(q, "systemMetrics"):
		m, err := s.resolver.SystemMetrics(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"systemMetrics": m}, nil
	case containsOp(q, "storageStats"):
		m, err := s.resolver.StorageStats(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"storageStats": m}, nil
	default:
		h, err := s.resolver.Health(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"health": h}, nil
	}
}

func containsOp(query, name string) bool {
	return len(query) > 0 && (containsFold(query, name+"{") || containsFold(query, name+" {") || containsFold(query, name+"("))
}

func containsFold(s, substr string) bool {
	return len(s) >= len(substr) && (stringIndexFold(s, substr) >= 0)
}

func stringIndexFold(s, substr string) int {
	ls, lsub := len(s), len(substr)
	for i := 0; i+lsub <= ls; i++ {
		if equalFoldASCII(s[i:i+lsub], substr) {
			return i
		}
	}
	return -1
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// PlaygroundHandler serves the GraphQL Playground
func (s *Server) PlaygroundHandler(w http.ResponseWriter, r *http.Request) {
	if !httpsecure.AuthorizeBearer(r.Header.Get("Authorization"), s.config.AuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	playgroundHTML := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8" />
    <title>Shugo GraphQL</title>
</head>
<body>
    <h1>Shugo GraphQL</h1>
    <p>Send authorized POST requests to /graphql.</p>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	if _, err := w.Write([]byte(playgroundHTML)); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

// HealthHandler handles health check requests
func (s *Server) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	h, healthErr := s.resolver.Health(r.Context())
	if healthErr != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if encErr := json.NewEncoder(w).Encode(h); encErr != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// MetricsHandler handles metrics requests
func (s *Server) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	if !httpsecure.AuthorizeBearer(r.Header.Get("Authorization"), s.config.AuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	peers, files := 0, 0
	if s.server != nil {
		peers = s.server.PeerCount()
		files = s.server.FileCount()
	}
	metrics := map[string]interface{}{
		"uptime":    time.Since(s.startTime).Seconds(),
		"peers":     peers,
		"files":     files,
		"timestamp": time.Now().UTC(),
	}
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// HandleFileUpload handles file upload requests
func (s *Server) HandleFileUpload(w http.ResponseWriter, r *http.Request) {
	if !httpsecure.AuthorizeBearer(r.Header.Get("Authorization"), s.config.AuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form
	err := r.ParseMultipartForm(32 << 20) // 32MB max
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to get file", http.StatusBadRequest)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			s.logger.Error("Failed to close file", "error", err)
		}
	}()

	// Get key from form
	key := r.FormValue("key")
	if key == "" {
		key = header.Filename
	}

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}
	contentType := header.Header.Get("Content-Type")
	if err := s.server.StoreWithMeta(r.Context(), key, bytes.NewReader(data), contentType, map[string]string{"name": header.Filename}); err != nil {
		s.logger.Error("upload failed", "error", err)
		http.Error(w, "Failed to store file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"success": true,
		"key":     key,
		"size":    len(data),
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// HandleFileDownload handles file download requests
func (s *Server) HandleFileDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "Key parameter required", http.StatusBadRequest)
		return
	}

	reader, err := s.server.Get(r.Context(), key)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if closer, ok := reader.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", key))
	if _, err := io.Copy(w, reader); err != nil {
		s.logger.Error("download stream failed", "error", err)
	}
}

// Stop gracefully stops the server
func (s *Server) Stop() error {
	s.logger.Info("Stopping GraphQL server")
	// TODO: Implement graceful shutdown
	return nil
}

package sse

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Skpow1234/Shugo/internal/app/fileserver"
	"github.com/Skpow1234/Shugo/internal/httpsecure"
)

// Server represents the SSE API server
type Server struct {
	fileserver *fileserver.Server
	config     *Config
	logger     *slog.Logger
	hub        *Hub
	mu         sync.RWMutex
	startTime  time.Time
}

// Config holds the configuration for the SSE server
type Config struct {
	Port              int
	Host              string
	AllowedOrigins    []string
	EnableCORS        bool
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	KeepAliveInterval time.Duration
	MaxConnections    int
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Port:              8084,
		Host:              "localhost",
		AllowedOrigins:    nil,
		EnableCORS:        true,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		KeepAliveInterval: 30 * time.Second,
		MaxConnections:    1000,
	}
}

// NewServer creates a new SSE API server
func NewServer(fileserver *fileserver.Server, config *Config, logger *slog.Logger) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	// Create SSE hub
	hub := NewHub(logger, config.MaxConnections)

	server := &Server{
		fileserver: fileserver,
		config:     config,
		logger:     logger,
		hub:        hub,
		startTime:  time.Now(),
	}

	// Start the SSE hub
	go hub.Run(context.Background())

	return server
}

// ServeHTTP implements the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers if enabled
	if s.config.EnableCORS {
		s.addCORSHeaders(w, r)
	}

	// Handle preflight requests
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Route requests based on path
	switch r.URL.Path {
	case "/sse":
		s.handleSSE(w, r)
	case "/sse/health":
		s.handleHealth(w, r)
	case "/sse/metrics":
		s.handleMetrics(w, r)
	case "/sse/status":
		s.handleStatus(w, r)
	default:
		http.NotFound(w, r)
	}
}

// addCORSHeaders adds CORS headers to the response
func (s *Server) addCORSHeaders(w http.ResponseWriter, r *http.Request) {
	httpsecure.SetCORS(w, r, s.config.AllowedOrigins)
}

// handleSSE handles Server-Sent Events connections
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")

	// Create SSE client
	client := NewClient(w, r, s.logger)

	// Register client with hub
	s.hub.Register(client)
	defer s.hub.Unregister(client)

	// Send initial connection event
	client.SendEvent("connected", map[string]interface{}{
		"message":   "Connected to Shugo SSE",
		"timestamp": time.Now().UTC(),
		"clientId":  client.ID,
	})

	// Handle client connection
	client.Handle()
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"uptime":    time.Since(s.startTime).String(),
		"version":   "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(health); err != nil {
		s.logger.Error("Failed to encode health response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// handleMetrics handles metrics requests
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metrics := map[string]interface{}{
		"sse": map[string]interface{}{
			"active_connections": s.hub.GetActiveConnections(),
			"total_connections":  s.hub.GetTotalConnections(),
			"uptime":             time.Since(s.startTime).String(),
		},
		"fileserver": map[string]interface{}{
			"status": "running",
		},
		"timestamp": time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		s.logger.Error("Failed to encode metrics response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// handleStatus handles status requests
func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	status := map[string]interface{}{
		"server": map[string]interface{}{
			"name":    "Shugo SSE API",
			"version": "1.0.0",
			"status":  "running",
			"uptime":  time.Since(s.startTime).String(),
		},
		"sse": map[string]interface{}{
			"active_connections": s.hub.GetActiveConnections(),
			"endpoints": []string{
				"/sse",
				"/sse/health",
				"/sse/metrics",
				"/sse/status",
			},
		},
		"timestamp": time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		s.logger.Error("Failed to encode status response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// BroadcastEvent broadcasts an event to all connected SSE clients
func (s *Server) BroadcastEvent(eventType string, data interface{}) {
	s.hub.BroadcastEvent(eventType, data)
}

// BroadcastEventToTopic broadcasts an event to clients subscribed to a specific topic
func (s *Server) BroadcastEventToTopic(topic, eventType string, data interface{}) {
	s.hub.BroadcastEventToTopic(topic, eventType, data)
}

// GetActiveConnections returns the number of active SSE connections
func (s *Server) GetActiveConnections() int {
	return s.hub.GetActiveConnections()
}

// GetTotalConnections returns the total number of connections since startup
func (s *Server) GetTotalConnections() int {
	return s.hub.GetTotalConnections()
}

package websocket

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Skpow1234/Shugo/internal/app/fileserver"
	"github.com/Skpow1234/Shugo/internal/httpsecure"
	"github.com/Skpow1234/Shugo/internal/websocket"
)

// Server represents the WebSocket API server
type Server struct {
	fileserver *fileserver.Server
	config     *Config
	logger     *slog.Logger
	hub        *websocket.Hub
	handler    *websocket.Handler
	mu         sync.RWMutex
	startTime  time.Time
}

// Config holds the configuration for the WebSocket server
type Config struct {
	Port           int
	Host           string
	AllowedOrigins []string
	EnableCORS     bool
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	PingPeriod     time.Duration
	PongWait       time.Duration
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Port:           8083,
		Host:           "localhost",
		AllowedOrigins: nil,
		EnableCORS:     true,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		PingPeriod:     54 * time.Second,
		PongWait:       60 * time.Second,
	}
}

// NewServer creates a new WebSocket API server
func NewServer(fileserver *fileserver.Server, config *Config, logger *slog.Logger) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	// Create WebSocket hub
	hub := websocket.NewHub(logger)
	handler := websocket.NewHandler(hub, logger)

	server := &Server{
		fileserver: fileserver,
		config:     config,
		logger:     logger,
		hub:        hub,
		handler:    handler,
		startTime:  time.Now(),
	}

	// Start the WebSocket hub
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
	case "/ws":
		s.handler.ServeHTTP(w, r)
	case "/ws/health":
		s.handleHealth(w, r)
	case "/ws/metrics":
		s.handleMetrics(w, r)
	case "/ws/status":
		s.handleStatus(w, r)
	default:
		http.NotFound(w, r)
	}
}

// addCORSHeaders adds CORS headers to the response
func (s *Server) addCORSHeaders(w http.ResponseWriter, r *http.Request) {
	httpsecure.SetCORS(w, r, s.config.AllowedOrigins)
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
		"websocket": map[string]interface{}{
			"active_connections": len(s.hub.GetClients()),
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
			"name":    "Shugo WebSocket API",
			"version": "1.0.0",
			"status":  "running",
			"uptime":  time.Since(s.startTime).String(),
		},
		"websocket": map[string]interface{}{
			"active_connections": len(s.hub.GetClients()),
			"endpoints": []string{
				"/ws",
				"/ws/health",
				"/ws/metrics",
				"/ws/status",
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

// BroadcastMessage broadcasts a message to all connected clients
func (s *Server) BroadcastMessage(messageType string, data interface{}) {
	message := websocket.Message{
		Type:      messageType,
		Data:      data,
		Timestamp: time.Now(),
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		s.logger.Error("Failed to marshal message", "error", err)
		return
	}

	s.hub.Broadcast(messageBytes)
}

// BroadcastToTopic broadcasts a message to clients subscribed to a specific topic
func (s *Server) BroadcastToTopic(topic string, data interface{}) {
	s.hub.BroadcastToTopic(topic, data)
}

// GetActiveConnections returns the number of active WebSocket connections
func (s *Server) GetActiveConnections() int {
	return len(s.hub.GetClients())
}

// GetTotalConnections returns the total number of connections since startup
func (s *Server) GetTotalConnections() int {
	return s.hub.GetTotalConnections()
}

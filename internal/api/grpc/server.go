package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Skpow1234/Shugo/internal/api/grpc/services"
	"github.com/Skpow1234/Shugo/internal/app/fileserver"
	"github.com/Skpow1234/Shugo/internal/httpsecure"
	"github.com/Skpow1234/Shugo/proto/shugo"
)

// Server represents the gRPC server (simplified for now)
type Server struct {
	httpServer    *http.Server
	listener      net.Listener
	config        *Config
	logger        *slog.Logger
	fileService   *services.FileService
	peerService   *services.PeerService
	systemService *services.SystemService

	// Streaming channels for event broadcasting
	fileEventSubscribers   map[chan *shugo.FileOperationEvent]bool
	peerEventSubscribers   map[chan *shugo.PeerEvent]bool
	systemEventSubscribers map[chan *shugo.SystemEvent]bool
	eventMutex             sync.RWMutex

	// Server state
	startTime time.Time
	stopChan  chan struct{}
}

// Config represents the server configuration
type Config struct {
	Port      string
	AuthToken string
}

// DefaultConfig returns the default server configuration
func DefaultConfig() *Config {
	return &Config{
		Port:      ":50051",
		AuthToken: os.Getenv("SHUGO_AUTH_TOKEN"),
	}
}

// NewServer creates a new server instance. Pass fsServer to back file ops with Shugo storage.
func NewServer(config *Config, logger *slog.Logger, fsServer *fileserver.Server) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	if logger == nil {
		logger = slog.Default()
	}

	server := &Server{
		config:                 config,
		logger:                 logger,
		fileService:            services.NewFileService(fsServer),
		peerService:            services.NewPeerService(),
		systemService:          services.NewSystemService(),
		fileEventSubscribers:   make(map[chan *shugo.FileOperationEvent]bool),
		peerEventSubscribers:   make(map[chan *shugo.PeerEvent]bool),
		systemEventSubscribers: make(map[chan *shugo.SystemEvent]bool),
		startTime:              time.Now(),
		stopChan:               make(chan struct{}),
	}

	// Create HTTP server with JSON endpoints
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("GET /health", server.HandleHealthCheck)

	// System info endpoint
	mux.HandleFunc("GET /system/info", server.HandleSystemInfo)

	// Metrics endpoint
	mux.HandleFunc("GET /system/metrics", server.HandleMetrics)

	// File operations endpoints
	mux.HandleFunc("GET /files", server.handleListFiles)
	mux.HandleFunc("GET /files/{key}", server.handleGetFile)
	mux.HandleFunc("DELETE /files/{key}", server.handleDeleteFile)

	// Peer operations endpoints
	mux.HandleFunc("GET /peers", server.handleListPeers)
	mux.HandleFunc("GET /peers/{id}", server.handleGetPeer)
	mux.HandleFunc("POST /peers", server.handleAddPeer)
	mux.HandleFunc("DELETE /peers/{id}", server.handleRemovePeer)
	mux.HandleFunc("GET /peers/{id}/health", server.handleGetPeerHealth)

	server.httpServer = &http.Server{
		Addr:              config.Port,
		Handler:           server.authMiddleware(mux),
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return server
}

// Start starts the server
func (s *Server) Start() error {
	if s.config.AuthToken == "" {
		return fmt.Errorf("SHUGO_AUTH_TOKEN is required")
	}
	certFile, keyFile := httpsecure.TLSFiles()
	addr, useTLS, err := httpsecure.ConfigureAddr(s.config.Port, certFile, keyFile)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on port %s: %w", addr, err)
	}
	if useTLS {
		pair, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			_ = listener.Close()
			return fmt.Errorf("load tls certificate: %w", err)
		}
		listener = tls.NewListener(listener, &tls.Config{
			Certificates: []tls.Certificate{pair},
			MinVersion:   tls.VersionTLS12,
		})
	}
	s.listener = listener
	s.httpServer.Addr = addr

	s.logger.Info("Starting gRPC server (HTTP/JSON mode)", "port", addr, "tls", useTLS)

	// Start event broadcasting goroutines
	go s.broadcastFileEvents()
	go s.broadcastPeerEvents()
	go s.broadcastSystemEvents()

	// Start the server
	return s.httpServer.Serve(listener)
}

// Stop stops the server gracefully
func (s *Server) Stop() error {
	s.logger.Info("Stopping gRPC server")

	// Signal stop to event broadcasting goroutines
	close(s.stopChan)

	// Stop the HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}

	// Close the listener
	if s.listener != nil {
		return s.listener.Close()
	}

	return nil
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		if !httpsecure.AuthorizeBearer(r.Header.Get("Authorization"), s.config.AuthToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HTTP Handlers

func (s *Server) HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
	health, err := s.systemService.HealthCheck()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"status":"%s","timestamp":"%s","version":"%s"}`,
		health.Status, health.Timestamp, health.Version); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) HandleSystemInfo(w http.ResponseWriter, r *http.Request) {
	info, err := s.systemService.GetSystemInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"version":"%s","uptime_seconds":%d,"peer_count":%d,"file_count":%d}`,
		info.Version, info.UptimeSeconds, info.PeerCount, info.FileCount); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := s.systemService.GetMetrics()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"requests_total":%d,"requests_per_minute":%f,"active_connections":%d}`,
		metrics.RequestsTotal, metrics.RequestsPerMinute, metrics.ActiveConnections); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	page := 1
	pageSize := 10
	filter := ""

	files, err := s.fileService.ListFiles(page, pageSize, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"files":[],"total":%d,"page":%d,"page_size":%d}`,
		files.Total, files.Page, files.PageSize); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	// Extract key from URL path
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "file key required", http.StatusBadRequest)
		return
	}

	file, err := s.fileService.GetFile(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"key":"%s","name":"%s","size":%d}`,
		file.Key, file.Name, file.Size); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "file key required", http.StatusBadRequest)
		return
	}

	success, err := s.fileService.DeleteFile(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"success":%t,"message":"File deleted successfully"}`, success); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleListPeers(w http.ResponseWriter, r *http.Request) {
	peers, err := s.peerService.ListPeers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"peers":[],"total":%d}`, peers.Total); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleGetPeer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "peer id required", http.StatusBadRequest)
		return
	}

	peer, err := s.peerService.GetPeer(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"id":"%s","address":"%s","port":%d,"status":"%s"}`,
		peer.Id, peer.Address, peer.Port, peer.Status); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleAddPeer(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// For now, use default values
	address := "localhost"
	port := 50051
	metadata := make(map[string]string)

	peer, err := s.peerService.AddPeer(address, port, metadata)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"id":"%s","address":"%s","port":%d,"status":"%s"}`,
		peer.Id, peer.Address, peer.Port, peer.Status); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleRemovePeer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "peer id required", http.StatusBadRequest)
		return
	}

	success, err := s.peerService.RemovePeer(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"success":%t,"message":"Peer removed successfully"}`, success); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleGetPeerHealth(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "peer id required", http.StatusBadRequest)
		return
	}

	health, err := s.peerService.GetPeerHealth(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, `{"peer_id":"%s","status":"%s","latency_ms":%f,"uptime_seconds":%d}`,
		health.PeerId, health.Status, health.LatencyMs, health.UptimeSeconds); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

// Event Broadcasting Methods

// broadcastFileEvent sends a file operation event to all subscribers
func (s *Server) broadcastFileEvent(event *shugo.FileOperationEvent) {
	s.eventMutex.RLock()
	defer s.eventMutex.RUnlock()

	for eventChan := range s.fileEventSubscribers {
		select {
		case eventChan <- event:
		default:
			// Channel is full, skip this event
			s.logger.Warn("File event channel full, dropping event", "event_type", event.EventType)
		}
	}
}

// broadcastPeerEvent sends a peer event to all subscribers
func (s *Server) broadcastPeerEvent(event *shugo.PeerEvent) {
	s.eventMutex.RLock()
	defer s.eventMutex.RUnlock()

	for eventChan := range s.peerEventSubscribers {
		select {
		case eventChan <- event:
		default:
			// Channel is full, skip this event
			s.logger.Warn("Peer event channel full, dropping event", "event_type", event.EventType)
		}
	}
}

// broadcastSystemEvent sends a system event to all subscribers
func (s *Server) broadcastSystemEvent(event *shugo.SystemEvent) {
	s.eventMutex.RLock()
	defer s.eventMutex.RUnlock()

	for eventChan := range s.systemEventSubscribers {
		select {
		case eventChan <- event:
		default:
			// Channel is full, skip this event
			s.logger.Warn("System event channel full, dropping event", "event_type", event.EventType)
		}
	}
}

// Event Broadcasting Goroutines

// broadcastFileEvents periodically sends file operation events
func (s *Server) broadcastFileEvents() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Send periodic file operation events for demonstration
			s.broadcastFileEvent(&shugo.FileOperationEvent{
				EventType: "periodic_check",
				FileKey:   "system",
				Timestamp: shugo.Now(),
				Metadata: map[string]string{
					"type": "periodic",
				},
			})
		case <-s.stopChan:
			return
		}
	}
}

// broadcastPeerEvents periodically sends peer events
func (s *Server) broadcastPeerEvents() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Send periodic peer events for demonstration
			s.broadcastPeerEvent(&shugo.PeerEvent{
				EventType: "health_check",
				PeerId:    "system",
				Timestamp: shugo.Now(),
				Metadata: map[string]string{
					"type": "periodic",
				},
			})
		case <-s.stopChan:
			return
		}
	}
}

// broadcastSystemEvents periodically sends system events
func (s *Server) broadcastSystemEvents() {
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Send periodic system events for demonstration
			s.broadcastSystemEvent(&shugo.SystemEvent{
				EventType: "status_update",
				Component: "grpc_server",
				Timestamp: shugo.Now(),
				Message:   "System running normally",
				Metadata: map[string]string{
					"uptime": fmt.Sprintf("%v", time.Since(s.startTime)),
				},
			})
		case <-s.stopChan:
			return
		}
	}
}

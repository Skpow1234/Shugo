package translation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Skpow1234/Shugo/internal/app/fileserver"
	"github.com/Skpow1234/Shugo/internal/httpsecure"
)

// Server represents the protocol translation server
type Server struct {
	fileserver *fileserver.Server
	config     *ServerConfig
	logger     *slog.Logger

	// Translation engines
	engines   map[string]*TranslationEngine
	enginesMu sync.RWMutex

	// Analytics
	analytics   *Analytics
	analyticsMu sync.RWMutex

	// Statistics
	stats   *ServerStats
	statsMu sync.RWMutex

	// Context for shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

// ServerConfig holds the configuration for the translation server
type ServerConfig struct {
	Port           int
	Host           string
	MaxConnections int
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration

	// Protocol endpoints
	WebSocketAddr string
	SSEAddr       string
	MQTTAddr      string
	CoAPAddr      string

	// Translation settings
	EnableAnalytics bool
	BufferSize      int
	RetryAttempts   int
	RetryDelay      time.Duration
}

// ServerStats holds server statistics
type ServerStats struct {
	StartTime         time.Time
	TotalRequests     int
	TotalTranslations int
	ActiveConnections int
	BytesTranslated   int64
	TranslationErrors int
	ProtocolStats     map[string]*ProtocolStats
}

// ProtocolStats holds statistics for a specific protocol
type ProtocolStats struct {
	Requests     int
	Translations int
	Errors       int
	BytesIn      int64
	BytesOut     int64
	LastActivity time.Time
}

// TranslationEngine represents a protocol translation engine
type TranslationEngine struct {
	Name         string
	FromProtocol string
	ToProtocol   string
	Translator   Translator
	Stats        *ProtocolStats
}

// Translator interface for protocol translation
type Translator interface {
	Translate(message *Message) (*Message, error)
	CanTranslate(from, to string) bool
	GetSupportedProtocols() []string
}

// Message represents a protocol-agnostic message
type Message struct {
	ID        string                 `json:"id"`
	Protocol  string                 `json:"protocol"`
	Type      string                 `json:"type"`
	Topic     string                 `json:"topic,omitempty"`
	Payload   interface{}            `json:"payload"`
	Headers   map[string]string      `json:"headers,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// NewServer creates a new protocol translation server
func NewServer(fileserver *fileserver.Server, config *ServerConfig, logger *slog.Logger) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	server := &Server{
		fileserver: fileserver,
		config:     config,
		logger:     logger,
		engines:    make(map[string]*TranslationEngine),
		analytics:  NewAnalytics(),
		stats: &ServerStats{
			StartTime:     time.Now(),
			ProtocolStats: make(map[string]*ProtocolStats),
		},
		ctx:    ctx,
		cancel: cancel,
	}

	// Initialize translation engines
	server.initializeEngines()

	// Start background tasks
	go server.startBackgroundTasks()

	return server
}

// initializeEngines initializes all translation engines
func (s *Server) initializeEngines() {
	// WebSocket to SSE
	s.registerEngine("websocket-to-sse", &WebSocketToSSETranslator{
		sseAddr: s.config.SSEAddr,
	})

	// WebSocket to MQTT
	s.registerEngine("websocket-to-mqtt", &WebSocketToMQTTTranslator{
		mqttAddr: s.config.MQTTAddr,
	})

	// WebSocket to CoAP
	s.registerEngine("websocket-to-coap", &WebSocketToCoAPTranslator{
		coapAddr: s.config.CoAPAddr,
	})

	// SSE to WebSocket
	s.registerEngine("sse-to-websocket", &SSEToWebSocketTranslator{
		websocketAddr: s.config.WebSocketAddr,
	})

	// SSE to MQTT
	s.registerEngine("sse-to-mqtt", &SSEToMQTTTranslator{
		mqttAddr: s.config.MQTTAddr,
	})

	// SSE to CoAP
	s.registerEngine("sse-to-coap", &SSEToCoAPTranslator{
		coapAddr: s.config.CoAPAddr,
	})

	// MQTT to WebSocket
	s.registerEngine("mqtt-to-websocket", &MQTTToWebSocketTranslator{
		websocketAddr: s.config.WebSocketAddr,
	})

	// MQTT to SSE
	s.registerEngine("mqtt-to-sse", &MQTTToSSETranslator{
		sseAddr: s.config.SSEAddr,
	})

	// MQTT to CoAP
	s.registerEngine("mqtt-to-coap", &MQTTToCoAPTranslator{
		coapAddr: s.config.CoAPAddr,
	})

	// CoAP to WebSocket
	s.registerEngine("coap-to-websocket", &CoAPToWebSocketTranslator{
		websocketAddr: s.config.WebSocketAddr,
	})

	// CoAP to SSE
	s.registerEngine("coap-to-sse", &CoAPToSSETranslator{
		sseAddr: s.config.SSEAddr,
	})

	// CoAP to MQTT
	s.registerEngine("coap-to-mqtt", &CoAPToMQTTTranslator{
		mqttAddr: s.config.MQTTAddr,
	})

	s.logger.Info("Initialized translation engines", "count", len(s.engines))
}

// registerEngine registers a translation engine
func (s *Server) registerEngine(name string, translator Translator) {
	engine := &TranslationEngine{
		Name:       name,
		Translator: translator,
		Stats: &ProtocolStats{
			LastActivity: time.Now(),
		},
	}

	s.enginesMu.Lock()
	s.engines[name] = engine
	s.enginesMu.Unlock()

	// Initialize protocol stats
	s.statsMu.Lock()
	s.stats.ProtocolStats[name] = &ProtocolStats{
		LastActivity: time.Now(),
	}
	s.statsMu.Unlock()

	s.logger.Debug("Registered translation engine", "name", name)
}

// ServeHTTP starts the HTTP server
func (s *Server) ServeHTTP(ctx context.Context, addr string) error {
	mux := http.NewServeMux()

	// Register handlers
	mux.HandleFunc("/translate", s.handleTranslate)
	mux.HandleFunc("/translate/websocket", s.handleWebSocketTranslation)
	mux.HandleFunc("/translate/sse", s.handleSSETranslation)
	mux.HandleFunc("/translate/mqtt", s.handleMQTTTranslation)
	mux.HandleFunc("/translate/coap", s.handleCoAPTranslation)
	mux.HandleFunc("/translate/analytics", s.handleAnalytics)
	mux.HandleFunc("/translate/health", s.handleHealth)

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
	}

	// Start server in goroutine
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("Failed to shutdown translation server", "error", err)
		}
	}()

	return httpsecure.ListenAndServe(server)
}

// handleTranslate handles general translation requests
func (s *Server) handleTranslate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request TranslationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Find appropriate translation engine
	engineName := fmt.Sprintf("%s-to-%s", request.FromProtocol, request.ToProtocol)
	engine, exists := s.getEngine(engineName)
	if !exists {
		http.Error(w, fmt.Sprintf("Translation from %s to %s not supported", request.FromProtocol, request.ToProtocol), http.StatusBadRequest)
		return
	}

	// Translate message
	translatedMessage, err := s.translateMessage(engine, &request.Message)
	if err != nil {
		s.logger.Error("Translation failed", "error", err, "engine", engineName)
		http.Error(w, "Translation failed", http.StatusInternalServerError)
		return
	}

	// Send response
	response := TranslationResponse{
		Success: true,
		Message: translatedMessage,
		Engine:  engineName,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Update statistics
	s.updateStats(engineName, true, len(request.Message.Payload.(string)))
}

// handleWebSocketTranslation handles WebSocket-specific translation
func (s *Server) handleWebSocketTranslation(w http.ResponseWriter, r *http.Request) {
	s.handleProtocolTranslation(w, r, "websocket")
}

// handleSSETranslation handles SSE-specific translation
func (s *Server) handleSSETranslation(w http.ResponseWriter, r *http.Request) {
	s.handleProtocolTranslation(w, r, "sse")
}

// handleMQTTTranslation handles MQTT-specific translation
func (s *Server) handleMQTTTranslation(w http.ResponseWriter, r *http.Request) {
	s.handleProtocolTranslation(w, r, "mqtt")
}

// handleCoAPTranslation handles CoAP-specific translation
func (s *Server) handleCoAPTranslation(w http.ResponseWriter, r *http.Request) {
	s.handleProtocolTranslation(w, r, "coap")
}

// handleProtocolTranslation handles protocol-specific translation
func (s *Server) handleProtocolTranslation(w http.ResponseWriter, r *http.Request, protocol string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request ProtocolTranslationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Find appropriate translation engine
	engineName := fmt.Sprintf("%s-to-%s", protocol, request.ToProtocol)
	engine, exists := s.getEngine(engineName)
	if !exists {
		http.Error(w, fmt.Sprintf("Translation from %s to %s not supported", protocol, request.ToProtocol), http.StatusBadRequest)
		return
	}

	// Create message
	message := &Message{
		ID:        generateMessageID(),
		Protocol:  protocol,
		Type:      request.Type,
		Topic:     request.Topic,
		Payload:   request.Payload,
		Headers:   request.Headers,
		Metadata:  request.Metadata,
		Timestamp: time.Now(),
	}

	// Translate message
	translatedMessage, err := s.translateMessage(engine, message)
	if err != nil {
		s.logger.Error("Translation failed", "error", err, "engine", engineName)
		http.Error(w, "Translation failed", http.StatusInternalServerError)
		return
	}

	// Send response
	response := TranslationResponse{
		Success: true,
		Message: translatedMessage,
		Engine:  engineName,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Update statistics
	s.updateStats(engineName, true, len(fmt.Sprintf("%v", request.Payload)))
}

// handleAnalytics handles analytics requests
func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	analytics := s.GetAnalytics()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(analytics); err != nil {
		s.logger.Error("Failed to encode analytics", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"uptime":    time.Since(s.stats.StartTime).String(),
		"version":   "1.0.0",
		"engines":   len(s.engines),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(health); err != nil {
		s.logger.Error("Failed to encode health response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// translateMessage translates a message using the specified engine
func (s *Server) translateMessage(engine *TranslationEngine, message *Message) (*Message, error) {
	// Update engine statistics
	engine.Stats.Requests++
	engine.Stats.LastActivity = time.Now()

	// Perform translation
	translatedMessage, err := engine.Translator.Translate(message)
	if err != nil {
		engine.Stats.Errors++
		return nil, err
	}

	// Update statistics
	engine.Stats.Translations++
	engine.Stats.BytesIn += int64(len(fmt.Sprintf("%v", message.Payload)))
	engine.Stats.BytesOut += int64(len(fmt.Sprintf("%v", translatedMessage.Payload)))

	// Update analytics
	s.analyticsMu.Lock()
	s.analytics.RecordTranslation(engine.Name, message.Protocol, translatedMessage.Protocol, true)
	s.analyticsMu.Unlock()

	return translatedMessage, nil
}

// getEngine gets a translation engine by name
func (s *Server) getEngine(name string) (*TranslationEngine, bool) {
	s.enginesMu.RLock()
	defer s.enginesMu.RUnlock()
	engine, exists := s.engines[name]
	return engine, exists
}

// updateStats updates server statistics
func (s *Server) updateStats(engineName string, success bool, bytesTranslated int) {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	s.stats.TotalRequests++
	if success {
		s.stats.TotalTranslations++
		s.stats.BytesTranslated += int64(bytesTranslated)
	} else {
		s.stats.TranslationErrors++
	}

	// Update protocol stats
	if protocolStats, exists := s.stats.ProtocolStats[engineName]; exists {
		protocolStats.Requests++
		if success {
			protocolStats.Translations++
		} else {
			protocolStats.Errors++
		}
		protocolStats.LastActivity = time.Now()
	}
}

// GetAnalytics returns current analytics data
func (s *Server) GetAnalytics() *AnalyticsData {
	s.analyticsMu.RLock()
	defer s.analyticsMu.RUnlock()
	return s.analytics.GetData()
}

// GetStats returns server statistics
func (s *Server) GetStats() *ServerStats {
	s.statsMu.RLock()
	defer s.statsMu.RUnlock()

	// Return a copy
	stats := *s.stats
	return &stats
}

// startBackgroundTasks starts background maintenance tasks
func (s *Server) startBackgroundTasks() {
	// Analytics cleanup task
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanup()
		case <-s.ctx.Done():
			return
		}
	}
}

// cleanup performs periodic cleanup tasks
func (s *Server) cleanup() {
	// Clean up old analytics data
	s.analyticsMu.Lock()
	s.analytics.Cleanup()
	s.analyticsMu.Unlock()

	s.logger.Debug("Translation server cleanup completed")
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() {
	s.logger.Info("Shutting down Protocol Translation server...")
	s.cancel()
	s.logger.Info("Protocol Translation server shutdown complete")
}

// Helper functions

// generateMessageID generates a unique message ID
func generateMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}

// Request/Response structures

// TranslationRequest represents a translation request
type TranslationRequest struct {
	FromProtocol string  `json:"from_protocol"`
	ToProtocol   string  `json:"to_protocol"`
	Message      Message `json:"message"`
}

// TranslationResponse represents a translation response
type TranslationResponse struct {
	Success bool     `json:"success"`
	Message *Message `json:"message,omitempty"`
	Error   string   `json:"error,omitempty"`
	Engine  string   `json:"engine"`
}

// ProtocolTranslationRequest represents a protocol-specific translation request
type ProtocolTranslationRequest struct {
	ToProtocol string                 `json:"to_protocol"`
	Type       string                 `json:"type"`
	Topic      string                 `json:"topic,omitempty"`
	Payload    interface{}            `json:"payload"`
	Headers    map[string]string      `json:"headers,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

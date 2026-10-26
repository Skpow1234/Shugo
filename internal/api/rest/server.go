package rest

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Skpow1234/Shugo/internal/api/analytics"
	"github.com/Skpow1234/Shugo/internal/api/monitoring"
	"github.com/Skpow1234/Shugo/internal/api/rest/endpoints"
	"github.com/Skpow1234/Shugo/internal/api/rest/implementations"
	"github.com/Skpow1234/Shugo/internal/api/rest/ratelimit"
	"github.com/Skpow1234/Shugo/internal/api/rest/versioning"
	"github.com/Skpow1234/Shugo/internal/app/fileserver"
	"github.com/Skpow1234/Shugo/internal/httpsecure"
)

type Server struct {
	config            *Config
	logger            *slog.Logger
	httpServer        *http.Server
	rateLimiter       *ratelimit.RateLimiter
	analyticsService  *analytics.Service
	analyticsHandler  *analytics.Handler
	monitoringService *monitoring.Service
	monitoringHandler *monitoring.Handler
	FileEndpoints     *endpoints.FileEndpoints
	PeerEndpoints     *endpoints.PeerEndpoints
	SystemEndpoints   *endpoints.SystemEndpoints
}

type Config struct {
	Port             string
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	MaxHeaderBytes   int
	AllowedOrigins   []string
	RateLimitPerMin  int
	AuthToken        string
	VersionConfig    *versioning.VersionConfig
	RateLimitConfig  *ratelimit.RateLimitConfig
	AnalyticsConfig  *analytics.Config
	MonitoringConfig *monitoring.MonitoringConfig
}

func DefaultConfig() *Config {
	return &Config{
		Port:             ":8081",
		ReadTimeout:      15 * time.Second,
		WriteTimeout:     15 * time.Second,
		MaxHeaderBytes:   1 << 20,
		AllowedOrigins:   []string{"http://localhost:3000"},
		RateLimitPerMin:  100,
		AuthToken:        os.Getenv("SHUGO_AUTH_TOKEN"),
		VersionConfig:    versioning.NewVersionConfig(),
		RateLimitConfig:  ratelimit.DefaultConfig(),
		AnalyticsConfig:  analytics.DefaultConfig(),
		MonitoringConfig: monitoring.DefaultConfig(),
	}
}

// NewServer creates the REST API server. Pass a fileserver to back file routes;
// if fsServer is nil, upload/delete fail closed (metadata-only stubs are removed).
func NewServer(config *Config, logger *slog.Logger, fsServer *fileserver.Server) *Server {
	fileService := implementations.NewFileService(fsServer)
	peerService := implementations.NewPeerService(fsServer)
	systemService := implementations.NewSystemService(fsServer)

	// Initialize rate limiter
	rateLimiter := ratelimit.NewRateLimiter(config.RateLimitConfig)

	// Initialize analytics
	analyticsStorage := analytics.NewMemoryStorage(config.AnalyticsConfig.MaxEntries, config.AnalyticsConfig.RetentionDays)
	analyticsService := analytics.NewService(analyticsStorage, logger, config.AnalyticsConfig)
	analyticsHandler := analytics.NewHandler(analyticsService, logger)

	// Initialize monitoring
	monitoringService := monitoring.NewService(config.MonitoringConfig, logger)
	monitoringHandler := monitoring.NewHandler(monitoringService, logger)

	// Initialize endpoints
	fileEndpoints := endpoints.NewFileEndpoints(fileService, logger)
	peerEndpoints := endpoints.NewPeerEndpoints(peerService, logger)
	systemEndpoints := endpoints.NewSystemEndpoints(systemService, logger)

	return &Server{
		config:            config,
		logger:            logger,
		rateLimiter:       rateLimiter,
		analyticsService:  analyticsService,
		analyticsHandler:  analyticsHandler,
		monitoringService: monitoringService,
		monitoringHandler: monitoringHandler,
		FileEndpoints:     fileEndpoints,
		PeerEndpoints:     peerEndpoints,
		SystemEndpoints:   systemEndpoints,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Apply middleware (monitoring and analytics should be applied early to capture all requests)
	versionMiddleware := versioning.VersionMiddleware(s.config.VersionConfig)
	rateLimitMiddleware := s.rateLimiter.Middleware()
	analyticsMiddleware := analytics.Middleware(s.analyticsService, s.logger)
	monitoringMiddleware := monitoring.Middleware(s.monitoringService, s.logger)

	handler := s.CORSMiddleware(
		monitoringMiddleware(
			analyticsMiddleware(
				versionMiddleware(
					rateLimitMiddleware(
						s.authMiddleware(
							s.loggingMiddleware(mux)))))))

	// API routes
	api := http.NewServeMux()
	api.HandleFunc("GET /files", s.FileEndpoints.HandleListFiles)
	api.HandleFunc("GET /files/get", s.FileEndpoints.HandleGetFile)
	api.HandleFunc("POST /files", s.FileEndpoints.HandleUploadFile)
	api.HandleFunc("DELETE /files", s.FileEndpoints.HandleDeleteFile)
	api.HandleFunc("PUT /files/metadata", s.FileEndpoints.HandleUpdateFileMetadata)

	api.HandleFunc("GET /peers", s.PeerEndpoints.HandleListPeers)
	api.HandleFunc("GET /peers/get", s.PeerEndpoints.HandleGetPeer)
	api.HandleFunc("POST /peers", s.PeerEndpoints.HandleAddPeer)
	api.HandleFunc("DELETE /peers", s.PeerEndpoints.HandleRemovePeer)

	// Analytics routes
	api.HandleFunc("GET /analytics/summary", s.analyticsHandler.HandleGetSummary)
	api.HandleFunc("GET /analytics/usage", s.analyticsHandler.HandleGetUsageMetrics)
	api.HandleFunc("GET /analytics/endpoint", s.analyticsHandler.HandleGetEndpointStats)
	api.HandleFunc("GET /analytics/user", s.analyticsHandler.HandleGetUserBehavior)
	api.HandleFunc("GET /analytics/trends", s.analyticsHandler.HandleGetUsageTrends)
	api.HandleFunc("GET /analytics/popularity", s.analyticsHandler.HandleGetPopularityMetrics)
	api.HandleFunc("GET /analytics/calls", s.analyticsHandler.HandleQueryAPICalls)
	api.HandleFunc("GET /analytics/dashboard", s.analyticsHandler.HandleGetDashboard)

	// Monitoring routes
	api.HandleFunc("GET /monitoring/snapshot", s.monitoringHandler.HandleGetSnapshot)
	api.HandleFunc("GET /monitoring/realtime", s.monitoringHandler.HandleGetRealTimeMetrics)
	api.HandleFunc("GET /monitoring/response-time", s.monitoringHandler.HandleGetResponseTimeStats)
	api.HandleFunc("GET /monitoring/throughput", s.monitoringHandler.HandleGetThroughputStats)
	api.HandleFunc("GET /monitoring/endpoint", s.monitoringHandler.HandleGetEndpointPerformance)
	api.HandleFunc("GET /monitoring/trend", s.monitoringHandler.HandleGetPerformanceTrend)
	api.HandleFunc("GET /monitoring/report", s.monitoringHandler.HandleGetPerformanceReport)
	api.HandleFunc("GET /monitoring/alerts", s.monitoringHandler.HandleGetAlerts)
	api.HandleFunc("POST /monitoring/alerts/silence", s.monitoringHandler.HandleSilenceAlert)
	api.HandleFunc("POST /monitoring/alerts/resolve", s.monitoringHandler.HandleResolveAlert)
	api.HandleFunc("GET /monitoring/recommendations", s.monitoringHandler.HandleGetRecommendations)
	api.HandleFunc("GET /monitoring/dashboard", s.monitoringHandler.HandleGetDashboard)
	api.HandleFunc("GET /monitoring/health-score", s.monitoringHandler.HandleGetHealthScore)
	api.HandleFunc("GET /monitoring/summary", s.monitoringHandler.HandleGetMetricsSummary)

	// System routes
	mux.HandleFunc("GET /health", s.SystemEndpoints.HandleHealth)
	mux.HandleFunc("GET /metrics", s.SystemEndpoints.HandleMetrics)
	mux.HandleFunc("GET /system", s.SystemEndpoints.HandleSystemInfo)
	mux.HandleFunc("POST /webhook", s.SystemEndpoints.HandleWebhook)
	mux.HandleFunc("GET /api", s.SystemEndpoints.HandleRoot)
	mux.HandleFunc("GET /docs", s.SystemEndpoints.HandleDocs)
	mux.HandleFunc("GET /swagger.json", s.SystemEndpoints.HandleSwaggerJSON)

	// Mount API under /api/v1
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", api))

	s.httpServer = &http.Server{
		Addr:           s.config.Port,
		Handler:        handler,
		ReadTimeout:    s.config.ReadTimeout,
		WriteTimeout:   s.config.WriteTimeout,
		MaxHeaderBytes: s.config.MaxHeaderBytes,
	}

	s.logger.Info("Starting REST API server",
		"port", s.config.Port,
		"analytics_enabled", s.config.AnalyticsConfig.Enabled,
		"monitoring_enabled", s.config.MonitoringConfig.Enabled)
	if s.config.AuthToken == "" {
		return fmt.Errorf("SHUGO_AUTH_TOKEN is required")
	}
	return httpsecure.ListenAndServe(s.httpServer)
}

// GetAnalyticsService exposes the analytics service for tests and integrations.
func (s *Server) GetAnalyticsService() *analytics.Service {
	return s.analyticsService
}

func (s *Server) Stop(ctx context.Context) error {
	// Stop rate limiter
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}

	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpsecure.SetCORS(w, r, s.config.AllowedOrigins)

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health check and docs
		if r.URL.Path == "/health" || r.URL.Path == "/docs" || r.URL.Path == "/swagger.json" || r.URL.Path == "/api" {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if !httpsecure.AuthorizeBearer(authHeader, s.config.AuthToken) {
			http.Error(w, "Invalid authorization token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)

		s.logger.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"duration", duration,
		)
	})
}

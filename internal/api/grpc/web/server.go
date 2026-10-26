package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/improbable-eng/grpc-web/go/grpcweb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	grpcapi "github.com/Skpow1234/Shugo/internal/api/grpc"
	"github.com/Skpow1234/Shugo/internal/httpsecure"
	"github.com/Skpow1234/Shugo/proto/shugo"
)

// WebServer represents a gRPC-Web server
type WebServer struct {
	grpcServer *grpc.Server
	webServer  *grpcweb.WrappedGrpcServer
	config     *Config
	logger     *slog.Logger
}

// Config represents the gRPC-Web server configuration
type Config struct {
	Port           string
	AllowedOrigins []string
	CORSEnabled    bool
	AuthToken      string
}

// DefaultConfig returns the default gRPC-Web server configuration
func DefaultConfig() *Config {
	return &Config{
		Port: ":8080",
		AllowedOrigins: []string{
			"http://localhost:3000",
			"http://localhost:8080",
			"https://localhost:3000",
			"https://localhost:8080",
		},
		CORSEnabled: true,
		AuthToken:   os.Getenv("SHUGO_AUTH_TOKEN"),
	}
}

// NewWebServer creates a new gRPC-Web server instance
func NewWebServer(config *Config, logger *slog.Logger) *WebServer {
	if config == nil {
		config = DefaultConfig()
	}

	if logger == nil {
		logger = slog.Default()
	}

	// Create gRPC server with interceptors
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(unaryAuthInterceptor(config.AuthToken)),
		grpc.StreamInterceptor(streamAuthInterceptor(config.AuthToken)),
	)

	// Register services
	shugo.RegisterShugoServiceServer(grpcServer, &grpcapi.ShugoServiceImpl{})

	// Wrap gRPC server for web
	webServer := grpcweb.WrapServer(grpcServer, grpcweb.WithOriginFunc(func(origin string) bool {
		if !config.CORSEnabled {
			return true
		}
		for _, allowedOrigin := range config.AllowedOrigins {
			if origin == allowedOrigin {
				return true
			}
		}
		return false
	}))

	return &WebServer{
		grpcServer: grpcServer,
		webServer:  webServer,
		config:     config,
		logger:     logger,
	}
}

// Start starts the gRPC-Web server
func (s *WebServer) Start() error {
	if s.config.AuthToken == "" {
		return fmt.Errorf("SHUGO_AUTH_TOKEN is required")
	}
	s.logger.Info("Starting gRPC-Web server", "port", s.config.Port)

	httpServer := &http.Server{
		Addr:    s.config.Port,
		Handler: s.createHTTPHandler(),
	}

	return httpsecure.ListenAndServe(httpServer)
}

// Stop stops the gRPC-Web server gracefully
func (s *WebServer) Stop() error {
	s.logger.Info("Stopping gRPC-Web server")
	s.grpcServer.GracefulStop()
	return nil
}

// createHTTPHandler creates the HTTP handler with CORS and gRPC-Web support
func (s *WebServer) createHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", s.handleHealthCheck)

	// gRPC-Web endpoint
	mux.Handle("/", s.webServer)

	// Add CORS middleware if enabled
	if s.config.CORSEnabled {
		return s.corsMiddleware(mux)
	}

	return mux
}

// corsMiddleware adds CORS headers to responses
func (s *WebServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		allowed := false
		for _, allowedOrigin := range s.config.AllowedOrigins {
			if origin == allowedOrigin {
				allowed = true
				break
			}
		}

		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Grpc-Web, X-User-Agent")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleHealthCheck handles health check requests
func (s *WebServer) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, `{"status":"healthy","service":"grpc-web","timestamp":"%s"}`,
		fmt.Sprintf("%d", r.Context().Value("timestamp"))); err != nil {
		s.logger.Warn("Failed to write health check response", "error", err)
	}
}

// unaryAuthInterceptor provides authentication for unary RPC calls
func unaryAuthInterceptor(authToken string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Extract token from metadata
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		authHeader := md.Get("authorization")
		if len(authHeader) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header")
		}

		// Check if token matches
		tokenHeader := authHeader[0]
		if !httpsecure.AuthorizeBearer(tokenHeader, authToken) {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		return handler(ctx, req)
	}
}

// streamAuthInterceptor provides authentication for streaming RPC calls
func streamAuthInterceptor(authToken string) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Extract token from metadata
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}

		authHeader := md.Get("authorization")
		if len(authHeader) == 0 {
			return status.Error(codes.Unauthenticated, "missing authorization header")
		}

		// Check if token matches
		tokenHeader := authHeader[0]
		if !httpsecure.AuthorizeBearer(tokenHeader, authToken) {
			return status.Error(codes.Unauthenticated, "invalid token")
		}

		return handler(srv, ss)
	}
}

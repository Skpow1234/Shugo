package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Skpow1234/Shugo/internal/httpsecure"
)

// FederationServer represents a GraphQL federation server
type FederationServer struct {
	gateway *FederationGateway
	config  *FederationConfig
	logger  *slog.Logger
}

// NewFederationServer creates a new federation server
func NewFederationServer(gateway *FederationGateway, config *FederationConfig) *FederationServer {
	if config == nil {
		config = DefaultFederationConfig()
	}

	return &FederationServer{
		gateway: gateway,
		config:  config,
		logger:  slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}
}

// Start starts the federation server
func (fs *FederationServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// GraphQL endpoint
	mux.HandleFunc("/graphql", fs.CORSMiddleware(fs.GraphQLHandler))

	// Service management endpoints
	mux.HandleFunc("/services", fs.CORSMiddleware(fs.ServicesHandler))
	mux.HandleFunc("/services/", fs.CORSMiddleware(fs.ServiceHandler))

	// Schema endpoint
	mux.HandleFunc("/schema", fs.CORSMiddleware(fs.SchemaHandler))

	// Health check endpoint
	mux.HandleFunc("/health", fs.HealthHandler)

	// Metrics endpoint
	mux.HandleFunc("/metrics", fs.MetricsHandler)

	// Start health checks
	if fs.config.EnableHealthChecks {
		go fs.gateway.StartHealthChecks(ctx, fs.config.HealthCheckInterval)
	}

	fs.logger.Info("Starting GraphQL Federation Gateway",
		"port", fs.config.GatewayPort,
		"healthChecks", fs.config.EnableHealthChecks,
	)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", fs.config.GatewayPort),
		Handler:           mux,
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return httpsecure.ListenAndServe(server)
}

// CORSMiddleware adds CORS headers to responses
func (fs *FederationServer) CORSMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpsecure.SetCORS(w, r, nil)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	}
}

// GraphQLHandler handles GraphQL requests
func (fs *FederationServer) GraphQLHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GraphQLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Execute the query through the federation gateway
	result, err := fs.gateway.ExecuteQuery(r.Context(), req.Query, req.Variables)
	if err != nil {
		response := GraphQLResponse{
			Errors: []GraphQLError{{
				Message: err.Error(),
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
		return
	}

	response := GraphQLResponse{
		Data:   result.Data,
		Errors: convertFederationErrors(result.Errors),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// ServicesHandler handles service management requests
func (fs *FederationServer) ServicesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		fs.listServices(w, r)
	case "POST":
		fs.registerService(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ServiceHandler handles individual service requests
func (fs *FederationServer) ServiceHandler(w http.ResponseWriter, r *http.Request) {
	// Extract service name from URL path
	serviceName := r.URL.Path[len("/services/"):]
	if serviceName == "" {
		http.Error(w, "Service name required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		fs.getService(w, r, serviceName)
	case "DELETE":
		fs.unregisterService(w, r, serviceName)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listServices lists all registered services
func (fs *FederationServer) listServices(w http.ResponseWriter, _ *http.Request) {
	services := fs.gateway.ListServices()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(services); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// registerService registers a new service
func (fs *FederationServer) registerService(w http.ResponseWriter, r *http.Request) {
	var service FederatedService
	if err := json.NewDecoder(r.Body).Decode(&service); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := fs.gateway.RegisterService(&service); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(service); err != nil {
		http.Error(w, "Failed to encode service", http.StatusInternalServerError)
		return
	}
}

// getService gets a specific service
func (fs *FederationServer) getService(w http.ResponseWriter, _ *http.Request, serviceName string) {
	service, exists := fs.gateway.GetService(serviceName)
	if !exists {
		http.Error(w, "Service not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(service); err != nil {
		http.Error(w, "Failed to encode service", http.StatusInternalServerError)
		return
	}
}

// unregisterService unregisters a service
func (fs *FederationServer) unregisterService(w http.ResponseWriter, _ *http.Request, serviceName string) {
	if err := fs.gateway.UnregisterService(serviceName); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// SchemaHandler returns the federated schema
func (fs *FederationServer) SchemaHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	schema, err := fs.gateway.GetFederatedSchema()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	if _, err := w.Write([]byte(schema)); err != nil {
		http.Error(w, "Failed to write schema response", http.StatusInternalServerError)
		return
	}
}

// HealthHandler handles health check requests
func (fs *FederationServer) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	healthyServices := fs.gateway.GetHealthyServices()
	totalServices := len(fs.gateway.ListServices())

	health := map[string]interface{}{
		"status":          "healthy",
		"timestamp":       time.Now().UTC(),
		"service":         "shugo-federation-gateway",
		"totalServices":   totalServices,
		"healthyServices": len(healthyServices),
	}

	if err := json.NewEncoder(w).Encode(health); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// MetricsHandler handles metrics requests
func (fs *FederationServer) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	metrics := fs.gateway.GetServiceMetrics()
	metrics["timestamp"] = time.Now().UTC()

	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
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

// convertFederationErrors converts federation errors to GraphQL errors
func convertFederationErrors(federationErrors []FederationError) []GraphQLError {
	errors := make([]GraphQLError, len(federationErrors))
	for i, fe := range federationErrors {
		errors[i] = GraphQLError{
			Message: fe.Message,
			Path:    fe.Path,
			Extensions: map[string]interface{}{
				"service": fe.Service,
			},
		}
	}
	return errors
}

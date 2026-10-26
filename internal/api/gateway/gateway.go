package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Skpow1234/Shugo/internal/api/rest/ratelimit"
	"github.com/Skpow1234/Shugo/internal/httpsecure"
)

// Gateway represents an API gateway
type Gateway struct {
	config      *GatewayConfig
	logger      *slog.Logger
	routes      map[string]*Route
	middleware  []MiddlewareFunc
	rateLimiter *ratelimit.RateLimiter
	stopChan    chan struct{}
	mu          sync.RWMutex
}

// GatewayConfig holds the configuration for the API gateway
type GatewayConfig struct {
	Name            string
	ListenAddr      string
	UpstreamTimeout time.Duration
	MaxRetries      int
	RetryDelay      time.Duration
	RateLimitConfig *ratelimit.RateLimitConfig
	Routes          []RouteConfig
}

// RouteConfig defines a route configuration
type RouteConfig struct {
	Path         string            `json:"path"`
	Methods      []string          `json:"methods"`
	UpstreamURL  string            `json:"upstream_url"`
	StripPrefix  string            `json:"strip_prefix,omitempty"`
	AddHeaders   map[string]string `json:"add_headers,omitempty"`
	Transform    *TransformConfig  `json:"transform,omitempty"`
	RewriteRules []RewriteRule     `json:"rewrite_rules,omitempty"`
}

// Route represents a configured route
type Route struct {
	Path         string
	Methods      map[string]bool
	UpstreamURL  *url.URL
	StripPrefix  string
	AddHeaders   map[string]string
	Transform    *TransformConfig
	RewriteRules []RewriteRule
}

// TransformConfig defines request/response transformation rules
type TransformConfig struct {
	Request  *TransformRule `json:"request,omitempty"`
	Response *TransformRule `json:"response,omitempty"`
}

// TransformRule defines a transformation rule
type TransformRule struct {
	AddFields    map[string]interface{} `json:"add_fields,omitempty"`
	RemoveFields []string               `json:"remove_fields,omitempty"`
	RenameFields map[string]string      `json:"rename_fields,omitempty"`
	FilterFields []string               `json:"filter_fields,omitempty"`
}

// RewriteRule defines a URL rewrite rule
type RewriteRule struct {
	Pattern string `json:"pattern"`
	Replace string `json:"replace"`
}

// MiddlewareFunc represents a middleware function
type MiddlewareFunc func(http.Handler) http.Handler

// GatewayStats holds gateway statistics
type GatewayStats struct {
	TotalRequests       int64
	SuccessfulRequests  int64
	FailedRequests      int64
	AverageResponseTime time.Duration
	LastRequestTime     time.Time
	RouteStats          map[string]*RouteStats
}

// RouteStats holds statistics for a specific route
type RouteStats struct {
	RequestCount        int64
	SuccessCount        int64
	ErrorCount          int64
	AverageResponseTime time.Duration
	LastRequestTime     time.Time
}

// NewGateway creates a new API gateway
func NewGateway(config *GatewayConfig, logger *slog.Logger) (*Gateway, error) {
	gw := &Gateway{
		config:     config,
		logger:     logger,
		routes:     make(map[string]*Route),
		middleware: make([]MiddlewareFunc, 0),
		stopChan:   make(chan struct{}),
	}

	// Initialize rate limiter if configured
	if config.RateLimitConfig != nil {
		gw.rateLimiter = ratelimit.NewRateLimiter(config.RateLimitConfig)
	}

	// Configure routes
	for _, routeConfig := range config.Routes {
		route, err := gw.createRoute(routeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create route for %s: %v", routeConfig.Path, err)
		}
		gw.routes[routeConfig.Path] = route
	}

	// Add default middleware
	gw.Use(gw.loggingMiddleware)
	gw.Use(gw.corsMiddleware)
	if gw.rateLimiter != nil {
		gw.Use(gw.rateLimiter.Middleware())
	}

	return gw, nil
}

// createRoute creates a route from configuration
func (gw *Gateway) createRoute(config RouteConfig) (*Route, error) {
	upstreamURL, err := url.Parse(config.UpstreamURL)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream URL: %v", err)
	}

	methods := make(map[string]bool)
	for _, method := range config.Methods {
		methods[strings.ToUpper(method)] = true
	}

	route := &Route{
		Path:         config.Path,
		Methods:      methods,
		UpstreamURL:  upstreamURL,
		StripPrefix:  config.StripPrefix,
		AddHeaders:   config.AddHeaders,
		Transform:    config.Transform,
		RewriteRules: config.RewriteRules,
	}

	return route, nil
}

// Use adds middleware to the gateway
func (gw *Gateway) Use(middleware MiddlewareFunc) {
	gw.middleware = append(gw.middleware, middleware)
}

// Start starts the API gateway
func (gw *Gateway) Start() error {
	mux := http.NewServeMux()

	// Apply middleware
	handler := gw.applyMiddleware(mux)

	// Add routes
	for path, route := range gw.routes {
		mux.HandleFunc(path, gw.createRouteHandler(route))
	}

	server := &http.Server{
		Addr:    gw.config.ListenAddr,
		Handler: handler,
	}

	gw.logger.Info("Starting API gateway", "addr", gw.config.ListenAddr)
	return httpsecure.ListenAndServe(server)
}

// Stop stops the API gateway
func (gw *Gateway) Stop(ctx context.Context) error {
	if gw.rateLimiter != nil {
		gw.rateLimiter.Stop()
	}
	close(gw.stopChan)
	return nil
}

// applyMiddleware applies all middleware to a handler
func (gw *Gateway) applyMiddleware(handler http.Handler) http.Handler {
	for i := len(gw.middleware) - 1; i >= 0; i-- {
		handler = gw.middleware[i](handler)
	}
	return handler
}

// createRouteHandler creates a handler for a specific route
func (gw *Gateway) createRouteHandler(route *Route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Check if method is allowed
		if !route.Methods[r.Method] && len(route.Methods) > 0 {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Apply transformations
		if err := gw.applyRequestTransform(r, route.Transform); err != nil {
			gw.logger.Error("Request transformation failed", "error", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Rewrite URL
		targetURL := gw.rewriteURL(r.URL, route)

		// Create upstream request
		upstreamReq := r.Clone(r.Context())
		upstreamReq.URL = targetURL
		upstreamReq.Host = targetURL.Host

		// Add headers
		for key, value := range route.AddHeaders {
			upstreamReq.Header.Set(key, value)
		}

		// Execute request with retries
		var resp *http.Response
		var err error
		for attempt := 0; attempt <= gw.config.MaxRetries; attempt++ {
			resp, err = gw.executeRequest(upstreamReq)
			if err == nil && resp.StatusCode < 500 {
				break
			}
			if attempt < gw.config.MaxRetries {
				time.Sleep(gw.config.RetryDelay)
			}
		}

		if err != nil {
			gw.logger.Error("Upstream request failed", "error", err, "attempts", gw.config.MaxRetries+1)
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			return
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				gw.logger.Warn("Failed to close response body", "error", err)
			}
		}()

		// Apply response transformation
		if err := gw.applyResponseTransform(resp, route.Transform); err != nil {
			gw.logger.Error("Response transformation failed", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Copy response
		gw.copyResponse(w, resp)

		duration := time.Since(start)
		gw.logger.Info("Request completed",
			"path", r.URL.Path,
			"method", r.Method,
			"status", resp.StatusCode,
			"duration", duration)
	}
}

// rewriteURL rewrites the URL according to route rules
func (gw *Gateway) rewriteURL(originalURL *url.URL, route *Route) *url.URL {
	newURL := *originalURL
	newURL.Scheme = route.UpstreamURL.Scheme
	newURL.Host = route.UpstreamURL.Host

	// Apply rewrite rules
	path := originalURL.Path
	for _, rule := range route.RewriteRules {
		path = strings.ReplaceAll(path, rule.Pattern, rule.Replace)
	}

	// Strip prefix if configured
	if route.StripPrefix != "" && strings.HasPrefix(path, route.StripPrefix) {
		path = strings.TrimPrefix(path, route.StripPrefix)
	}

	newURL.Path = path
	return &newURL
}

// executeRequest executes an HTTP request with timeout
func (gw *Gateway) executeRequest(req *http.Request) (*http.Response, error) {
	client := &http.Client{
		Timeout: gw.config.UpstreamTimeout,
	}

	// Clear RequestURI as it's not allowed in client requests
	req.RequestURI = ""

	return client.Do(req)
}

// applyRequestTransform applies request transformations
func (gw *Gateway) applyRequestTransform(r *http.Request, transform *TransformConfig) error {
	if transform == nil || transform.Request == nil {
		return nil
	}

	// For JSON requests, we could transform the body
	if r.Header.Get("Content-Type") == "application/json" && r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return err
		}

		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			return err
		}

		// Apply transformations
		gw.applyFieldTransformations(data, transform.Request)

		// Write back
		newBody, err := json.Marshal(data)
		if err != nil {
			return err
		}

		r.Body = io.NopCloser(bytes.NewReader(newBody))
		r.ContentLength = int64(len(newBody))
	}

	return nil
}

// applyResponseTransform applies response transformations
func (gw *Gateway) applyResponseTransform(resp *http.Response, transform *TransformConfig) error {
	if transform == nil || transform.Response == nil {
		return nil
	}

	// For JSON responses, we could transform the body
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			return err
		}

		// Apply transformations
		gw.applyFieldTransformations(data, transform.Response)

		// Write back
		newBody, err := json.Marshal(data)
		if err != nil {
			return err
		}

		resp.Body = io.NopCloser(bytes.NewReader(newBody))
		resp.ContentLength = int64(len(newBody))
	}

	return nil
}

// applyFieldTransformations applies field transformations to JSON data
func (gw *Gateway) applyFieldTransformations(data map[string]interface{}, rule *TransformRule) {
	// Add fields
	for key, value := range rule.AddFields {
		data[key] = value
	}

	// Remove fields
	for _, field := range rule.RemoveFields {
		delete(data, field)
	}

	// Rename fields
	for oldKey, newKey := range rule.RenameFields {
		if value, exists := data[oldKey]; exists {
			data[newKey] = value
			delete(data, oldKey)
		}
	}

	// Filter fields (keep only specified fields)
	if len(rule.FilterFields) > 0 {
		filtered := make(map[string]interface{})
		for _, field := range rule.FilterFields {
			if value, exists := data[field]; exists {
				filtered[field] = value
			}
		}
		// Clear original and copy filtered
		for k := range data {
			delete(data, k)
		}
		for k, v := range filtered {
			data[k] = v
		}
	}
}

// copyResponse copies the response from upstream to client
func (gw *Gateway) copyResponse(w http.ResponseWriter, resp *http.Response) {
	// Copy headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set status code
	w.WriteHeader(resp.StatusCode)

	// Copy body
	if _, err := io.Copy(w, resp.Body); err != nil {
		gw.logger.Error("Failed to copy response body", "error", err)
	}
}

// loggingMiddleware logs requests
func (gw *Gateway) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gw.logger.Info("Gateway request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.Header.Get("User-Agent"))
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds CORS headers
func (gw *Gateway) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpsecure.SetCORS(w, r, nil)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetStats returns gateway statistics
func (gw *Gateway) GetStats() *GatewayStats {
	return &GatewayStats{
		RouteStats: make(map[string]*RouteStats),
	}
}

// AddRoute adds a new route to the gateway
func (gw *Gateway) AddRoute(config RouteConfig) error {
	gw.mu.Lock()
	defer gw.mu.Unlock()

	route, err := gw.createRoute(config)
	if err != nil {
		return err
	}

	gw.routes[config.Path] = route
	return nil
}

// RemoveRoute removes a route from the gateway
func (gw *Gateway) RemoveRoute(path string) {
	gw.mu.Lock()
	defer gw.mu.Unlock()

	delete(gw.routes, path)
}

// ListRoutes returns all configured routes
func (gw *Gateway) ListRoutes() map[string]*Route {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	routes := make(map[string]*Route)
	for path, route := range gw.routes {
		routes[path] = route
	}

	return routes
}

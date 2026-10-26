package mocking

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/yaml.v3"

	"github.com/Skpow1234/Shugo/internal/httpsecure"
)

// MockServer provides API mocking capabilities
type MockServer struct {
	config    *MockConfig
	router    *mux.Router
	logger    *slog.Logger
	scenarios map[string]*Scenario
	analytics *MockAnalytics
	server    *http.Server
}

// MockConfig holds configuration for the mock server
type MockConfig struct {
	Port            int               `yaml:"port"`
	Host            string            `yaml:"host"`
	OpenAPISpec     string            `yaml:"openapi_spec"`
	MockDataDir     string            `yaml:"mock_data_dir"`
	ResponseDelay   time.Duration     `yaml:"response_delay"`
	EnableAnalytics bool              `yaml:"enable_analytics"`
	Scenarios       map[string]string `yaml:"scenarios"`
	Headers         map[string]string `yaml:"headers"`
}

// Scenario defines a mock scenario
type Scenario struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Conditions  []Condition            `json:"conditions"`
	Response    *MockResponse          `json:"response"`
	Variables   map[string]interface{} `json:"variables"`
	Enabled     bool                   `json:"enabled"`
}

// Condition defines when a scenario should be triggered
type Condition struct {
	Type     string      `json:"type"` // "header", "query", "body", "path"
	Key      string      `json:"key"`
	Value    interface{} `json:"value"`
	Operator string      `json:"operator"` // "equals", "contains", "regex"
	Required bool        `json:"required"`
}

// MockResponse defines the response for a scenario
type MockResponse struct {
	StatusCode int                    `json:"status_code"`
	Headers    map[string]string      `json:"headers"`
	Body       interface{}            `json:"body"`
	Delay      time.Duration          `json:"delay"`
	Template   string                 `json:"template"`
	Variables  map[string]interface{} `json:"variables"`
}

// MockAnalytics tracks mock server usage
type MockAnalytics struct {
	Requests     map[string]int64     `json:"requests"`
	Scenarios    map[string]int64     `json:"scenarios"`
	Errors       map[string]int64     `json:"errors"`
	ResponseTime map[string][]float64 `json:"response_time"`
	StartTime    time.Time            `json:"start_time"`
}

// NewMockServer creates a new mock server
func NewMockServer(config *MockConfig, logger *slog.Logger) *MockServer {
	if config == nil {
		config = DefaultMockConfig()
	}

	return &MockServer{
		config:    config,
		router:    mux.NewRouter(),
		logger:    logger,
		scenarios: make(map[string]*Scenario),
		analytics: &MockAnalytics{
			Requests:     make(map[string]int64),
			Scenarios:    make(map[string]int64),
			Errors:       make(map[string]int64),
			ResponseTime: make(map[string][]float64),
			StartTime:    time.Now(),
		},
	}
}

// DefaultMockConfig returns the default mock server configuration
func DefaultMockConfig() *MockConfig {
	return &MockConfig{
		Port:            3001,
		Host:            "localhost",
		OpenAPISpec:     "docs/api/shugo-rest-api.yaml",
		MockDataDir:     "tests/api/mock-data",
		ResponseDelay:   100 * time.Millisecond,
		EnableAnalytics: true,
		Scenarios:       make(map[string]string),
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// Start starts the mock server
func (ms *MockServer) Start(ctx context.Context) error {
	ms.logger.Info("Starting mock server", "port", ms.config.Port, "host", ms.config.Host)

	// Load scenarios
	if err := ms.loadScenarios(); err != nil {
		return fmt.Errorf("failed to load scenarios: %w", err)
	}

	// Setup routes
	ms.setupRoutes()

	// Setup server
	ms.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", ms.config.Host, ms.config.Port),
		Handler:      ms.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server
	go func() {
		if err := httpsecure.ListenAndServe(ms.server); err != nil && err != http.ErrServerClosed {
			ms.logger.Error("Mock server error", "error", err)
		}
	}()

	ms.logger.Info("Mock server started successfully")
	return nil
}

// Stop stops the mock server
func (ms *MockServer) Stop(ctx context.Context) error {
	ms.logger.Info("Stopping mock server")
	return ms.server.Shutdown(ctx)
}

// loadScenarios loads mock scenarios from configuration
func (ms *MockServer) loadScenarios() error {
	// Load scenarios from files
	if ms.config.MockDataDir != "" {
		if err := ms.loadScenariosFromDir(ms.config.MockDataDir); err != nil {
			ms.logger.Warn("Failed to load scenarios from directory", "error", err)
		}
	}

	// Load scenarios from OpenAPI spec
	if ms.config.OpenAPISpec != "" {
		if err := ms.loadScenariosFromOpenAPI(ms.config.OpenAPISpec); err != nil {
			ms.logger.Warn("Failed to load scenarios from OpenAPI spec", "error", err)
		}
	}

	ms.logger.Info("Loaded scenarios", "count", len(ms.scenarios))
	return nil
}

// loadScenariosFromDir loads scenarios from a directory
func (ms *MockServer) loadScenariosFromDir(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !strings.HasSuffix(path, ".json") && !strings.HasSuffix(path, ".yaml") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var scenarios []Scenario
		if strings.HasSuffix(path, ".yaml") {
			err = yaml.Unmarshal(data, &scenarios)
		} else {
			err = json.Unmarshal(data, &scenarios)
		}

		if err != nil {
			return err
		}

		for _, scenario := range scenarios {
			ms.scenarios[scenario.Name] = &scenario
		}

		return nil
	})
}

// loadScenariosFromOpenAPI loads scenarios from OpenAPI specification
func (ms *MockServer) loadScenariosFromOpenAPI(specPath string) error {
	// This would parse OpenAPI spec and generate scenarios
	// For now, we'll create some basic scenarios
	ms.scenarios["health-check"] = &Scenario{
		Name:        "health-check",
		Description: "Health check endpoint",
		Response: &MockResponse{
			StatusCode: 200,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: map[string]interface{}{
				"status":    "healthy",
				"timestamp": time.Now().UTC(),
			},
		},
		Enabled: true,
	}

	return nil
}

// setupRoutes sets up the mock server routes
func (ms *MockServer) setupRoutes() {
	// Health check
	ms.router.HandleFunc("/health", ms.healthHandler).Methods("GET")

	// Analytics endpoint
	ms.router.HandleFunc("/analytics", ms.analyticsHandler).Methods("GET")

	// Catch-all handler for mock responses
	ms.router.PathPrefix("/").HandlerFunc(ms.mockHandler)
}

// healthHandler handles health check requests
func (ms *MockServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"service":   "shugo-mock-server",
		"timestamp": time.Now().UTC(),
	}); err != nil {
		ms.logger.Error("Failed to encode health response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// analyticsHandler returns mock server analytics
func (ms *MockServer) analyticsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(ms.analytics); err != nil {
		ms.logger.Error("Failed to encode analytics response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// mockHandler handles all mock requests
func (ms *MockServer) mockHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	path := r.URL.Path
	method := r.Method

	// Find matching scenario
	scenario := ms.findMatchingScenario(r)
	if scenario == nil {
		ms.logger.Warn("No matching scenario found", "path", path, "method", method)
		ms.analytics.Errors["no_scenario"]++
		http.NotFound(w, r)
		return
	}

	// Apply response delay
	if scenario.Response.Delay > 0 {
		time.Sleep(scenario.Response.Delay)
	} else if ms.config.ResponseDelay > 0 {
		time.Sleep(ms.config.ResponseDelay)
	}

	// Set response headers
	for key, value := range scenario.Response.Headers {
		w.Header().Set(key, value)
	}

	// Set default headers
	for key, value := range ms.config.Headers {
		if w.Header().Get(key) == "" {
			w.Header().Set(key, value)
		}
	}

	// Set status code
	w.WriteHeader(scenario.Response.StatusCode)

	// Send response body
	if scenario.Response.Body != nil {
		if err := json.NewEncoder(w).Encode(scenario.Response.Body); err != nil {
			ms.logger.Error("Failed to encode mock response", "error", err, "scenario", scenario.Name)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	// Update analytics
	ms.analytics.Requests[fmt.Sprintf("%s %s", method, path)]++
	ms.analytics.Scenarios[scenario.Name]++
	responseTime := time.Since(start).Seconds()
	ms.analytics.ResponseTime[scenario.Name] = append(ms.analytics.ResponseTime[scenario.Name], responseTime)

	ms.logger.Debug("Mock response sent",
		"scenario", scenario.Name,
		"path", path,
		"method", method,
		"status", scenario.Response.StatusCode,
		"response_time", responseTime)
}

// findMatchingScenario finds a scenario that matches the request
func (ms *MockServer) findMatchingScenario(r *http.Request) *Scenario {
	for _, scenario := range ms.scenarios {
		if !scenario.Enabled {
			continue
		}

		if ms.matchesConditions(r, scenario.Conditions) {
			return scenario
		}
	}

	return nil
}

// matchesConditions checks if a request matches scenario conditions
func (ms *MockServer) matchesConditions(r *http.Request, conditions []Condition) bool {
	for _, condition := range conditions {
		if !ms.matchesCondition(r, condition) {
			return false
		}
	}
	return true
}

// matchesCondition checks if a request matches a single condition
func (ms *MockServer) matchesCondition(r *http.Request, condition Condition) bool {
	var actualValue string

	switch condition.Type {
	case "header":
		actualValue = r.Header.Get(condition.Key)
	case "query":
		actualValue = r.URL.Query().Get(condition.Key)
	case "path":
		// Extract path parameter
		vars := mux.Vars(r)
		actualValue = vars[condition.Key]
	default:
		return false
	}

	expectedValue := fmt.Sprintf("%v", condition.Value)

	switch condition.Operator {
	case "equals":
		return actualValue == expectedValue
	case "contains":
		return strings.Contains(actualValue, expectedValue)
	case "regex":
		// Simple regex matching (in production, use proper regex)
		return strings.Contains(actualValue, expectedValue)
	default:
		return actualValue == expectedValue
	}
}

// AddScenario adds a new scenario to the mock server
func (ms *MockServer) AddScenario(scenario *Scenario) {
	ms.scenarios[scenario.Name] = scenario
	ms.logger.Info("Added scenario", "name", scenario.Name)
}

// RemoveScenario removes a scenario from the mock server
func (ms *MockServer) RemoveScenario(name string) {
	delete(ms.scenarios, name)
	ms.logger.Info("Removed scenario", "name", name)
}

// GetAnalytics returns mock server analytics
func (ms *MockServer) GetAnalytics() *MockAnalytics {
	return ms.analytics
}

// GetScenarios returns all scenarios
func (ms *MockServer) GetScenarios() map[string]*Scenario {
	return ms.scenarios
}

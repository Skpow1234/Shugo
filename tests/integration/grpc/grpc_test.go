package grpc_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Skpow1234/Shugo/internal/api/grpc"
)

func TestGRPCServerCreation(t *testing.T) {
	config := grpc.DefaultConfig()
	config.Port = ":0" // Use port 0 for testing

	server := grpc.NewServer(config, nil, nil)
	assert.NotNil(t, server)
	// Note: config field is private, so we can't access it directly
}

func TestGRPCHealthCheck(t *testing.T) {
	config := grpc.DefaultConfig()
	config.Port = ":0"

	server := grpc.NewServer(config, nil, nil)
	require.NotNil(t, server)

	// Test the health check handler directly
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.HandleHealthCheck(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), "healthy")
}

func TestGRPCSystemInfo(t *testing.T) {
	config := grpc.DefaultConfig()
	config.Port = ":0"

	server := grpc.NewServer(config, nil, nil)
	require.NotNil(t, server)

	// Test the system info handler directly
	req := httptest.NewRequest("GET", "/system/info", nil)
	w := httptest.NewRecorder()

	server.HandleSystemInfo(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), "version")
}

func TestGRPCMetrics(t *testing.T) {
	config := grpc.DefaultConfig()
	config.Port = ":0"

	server := grpc.NewServer(config, nil, nil)
	require.NotNil(t, server)

	// Test the metrics handler directly
	req := httptest.NewRequest("GET", "/system/metrics", nil)
	w := httptest.NewRecorder()

	server.HandleMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), "requests_total")
}

func TestGRPCConfig(t *testing.T) {
	config := grpc.DefaultConfig()
	assert.NotNil(t, config)
	assert.Equal(t, ":50051", config.Port)
	assert.Equal(t, os.Getenv("SHUGO_AUTH_TOKEN"), config.AuthToken)

	// Test custom config
	customConfig := &grpc.Config{
		Port:      ":8080",
		AuthToken: "custom-token",
	}
	assert.Equal(t, ":8080", customConfig.Port)
	assert.Equal(t, "custom-token", customConfig.AuthToken)
}

func TestGRPCServerStartStop(t *testing.T) {
	config := grpc.DefaultConfig()
	config.Port = ":0" // Use port 0 for testing

	server := grpc.NewServer(config, nil, nil)
	require.NotNil(t, server)

	// Test that we can stop the server without panicking
	// (even if it's not running)
	err := server.Stop()
	assert.NoError(t, err)
}

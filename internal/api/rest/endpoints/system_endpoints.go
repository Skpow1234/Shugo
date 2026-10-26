package endpoints

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/Skpow1234/Shugo/internal/api/rest/services"
	"github.com/Skpow1234/Shugo/internal/api/rest/types"
	"github.com/Skpow1234/Shugo/internal/api/rest/types/requests"
	"github.com/Skpow1234/Shugo/internal/api/rest/types/responses"
)

type SystemEndpoints struct {
	systemService services.SystemService
	logger        *slog.Logger
}

func NewSystemEndpoints(systemService services.SystemService, logger *slog.Logger) *SystemEndpoints {
	return &SystemEndpoints{
		systemService: systemService,
		logger:        logger,
	}
}

func (e *SystemEndpoints) HandleHealth(w http.ResponseWriter, r *http.Request) {
	healthy, err := e.systemService.HealthCheck(r.Context())
	if err != nil {
		e.logger.Error("Health check failed", "error", err)
		http.Error(w, "Health check failed", http.StatusInternalServerError)
		return
	}

	status := "healthy"
	if !healthy {
		status = "unhealthy"
	}

	response := responses.HealthResponse{
		Status:    status,
		Timestamp: time.Now(),
		Version:   "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (e *SystemEndpoints) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := e.systemService.GetMetrics(r.Context())
	if err != nil {
		e.logger.Error("Failed to get metrics", "error", err)
		http.Error(w, "Failed to get metrics", http.StatusInternalServerError)
		return
	}

	response := types.MetricsToResponse(metrics)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (e *SystemEndpoints) HandleSystemInfo(w http.ResponseWriter, r *http.Request) {
	info, err := e.systemService.GetSystemInfo(r.Context())
	if err != nil {
		e.logger.Error("Failed to get system info", "error", err)
		http.Error(w, "Failed to get system info", http.StatusInternalServerError)
		return
	}

	response := types.SystemInfoToResponse(info)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (e *SystemEndpoints) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	var request requests.WebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	e.logger.Info("Webhook received", "event", request.Event, "source", request.Source)
	w.WriteHeader(http.StatusOK)
}

func (e *SystemEndpoints) HandleRoot(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"message": "Shugo REST API",
		"version": "1.0.0",
		"endpoints": map[string]string{
			"health":     "/health",
			"metrics":    "/metrics",
			"system":     "/system",
			"files":      "/api/v1/files",
			"peers":      "/api/v1/peers",
			"analytics":  "/api/v1/analytics",
			"monitoring": "/api/v1/monitoring",
			"docs":       "/docs",
			"swagger":    "/swagger.json",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (e *SystemEndpoints) HandleDocs(w http.ResponseWriter, r *http.Request) {
	swaggerHTML := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8" />
    <title>Shugo REST API</title>
</head>
<body>
    <h1>Shugo REST API</h1>
    <p>The OpenAPI document is at <a href="/swagger.json">/swagger.json</a>.</p>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	if _, err := w.Write([]byte(swaggerHTML)); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func (e *SystemEndpoints) HandleSwaggerJSON(w http.ResponseWriter, r *http.Request) {
	swaggerSpec := map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       "Shugo REST API",
			"description": "Shugo is a distributed, encrypted file storage system with peer-to-peer replication.",
			"version":     "1.0.0",
		},
		"paths": map[string]interface{}{
			"/health": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Health Check",
					"description": "Check the health status of the API",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Health status",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/HealthResponse",
									},
								},
							},
						},
					},
				},
			},
			"/metrics": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get Metrics",
					"description": "Get system metrics and performance data",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "System metrics",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/MetricsResponse",
									},
								},
							},
						},
					},
				},
			},
		},
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"HealthResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"status": map[string]interface{}{
							"type": "string",
						},
						"timestamp": map[string]interface{}{
							"type":   "string",
							"format": "date-time",
						},
						"version": map[string]interface{}{
							"type": "string",
						},
					},
				},
				"MetricsResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"requests_total": map[string]interface{}{
							"type": "integer",
						},
						"requests_per_minute": map[string]interface{}{
							"type": "number",
						},
						"active_connections": map[string]interface{}{
							"type": "integer",
						},
						"storage_usage_percent": map[string]interface{}{
							"type": "number",
						},
						"last_updated": map[string]interface{}{
							"type":   "string",
							"format": "date-time",
						},
					},
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(swaggerSpec); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Skpow1234/Shugo/internal/httpsecure"
)

// AnalyticsDashboard provides a web-based analytics dashboard
type AnalyticsDashboard struct {
	queryAnalytics     *QueryAnalytics
	performanceMonitor *PerformanceMonitor
	config             *DashboardConfig
	logger             *slog.Logger
}

// DashboardConfig holds configuration for the analytics dashboard
type DashboardConfig struct {
	Port            int           `json:"port"`
	EnableDashboard bool          `json:"enableDashboard"`
	RefreshInterval time.Duration `json:"refreshInterval"`
	EnableExport    bool          `json:"enableExport"`
	EnableRealTime  bool          `json:"enableRealTime"`
}

// DefaultDashboardConfig returns the default dashboard configuration
func DefaultDashboardConfig() *DashboardConfig {
	return &DashboardConfig{
		Port:            8082,
		EnableDashboard: true,
		RefreshInterval: 5 * time.Second,
		EnableExport:    true,
		EnableRealTime:  true,
	}
}

// NewAnalyticsDashboard creates a new analytics dashboard
func NewAnalyticsDashboard(queryAnalytics *QueryAnalytics, performanceMonitor *PerformanceMonitor, config *DashboardConfig, logger *slog.Logger) *AnalyticsDashboard {
	if config == nil {
		config = DefaultDashboardConfig()
	}

	return &AnalyticsDashboard{
		queryAnalytics:     queryAnalytics,
		performanceMonitor: performanceMonitor,
		config:             config,
		logger:             logger,
	}
}

// Start starts the analytics dashboard server
func (ad *AnalyticsDashboard) Start(ctx context.Context) error {
	if !ad.config.EnableDashboard {
		ad.logger.Info("Analytics dashboard is disabled")
		return nil
	}

	mux := http.NewServeMux()

	// Dashboard endpoints
	mux.HandleFunc("/", ad.CORSMiddleware(ad.DashboardHandler))
	mux.HandleFunc("/api/metrics", ad.CORSMiddleware(ad.MetricsHandler))
	mux.HandleFunc("/api/queries", ad.CORSMiddleware(ad.QueriesHandler))
	mux.HandleFunc("/api/performance", ad.CORSMiddleware(ad.PerformanceHandler))
	mux.HandleFunc("/api/insights", ad.CORSMiddleware(ad.InsightsHandler))
	mux.HandleFunc("/api/export", ad.CORSMiddleware(ad.ExportHandler))
	mux.HandleFunc("/api/health", ad.HealthHandler)

	// WebSocket endpoint for real-time updates
	if ad.config.EnableRealTime {
		mux.HandleFunc("/ws", ad.CORSMiddleware(ad.WebSocketHandler))
	}

	ad.logger.Info("Starting analytics dashboard", "port", ad.config.Port)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", ad.config.Port),
		Handler:           mux,
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return httpsecure.ListenAndServe(server)
}

// CORSMiddleware adds CORS headers to responses
func (ad *AnalyticsDashboard) CORSMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpsecure.SetCORS(w, r, nil)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	}
}

// DashboardHandler serves the main dashboard page
func (ad *AnalyticsDashboard) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dashboardHTML := ad.generateDashboardHTML()
	w.Header().Set("Content-Type", "text/html")
	if _, err := w.Write([]byte(dashboardHTML)); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

// MetricsHandler returns comprehensive metrics
func (ad *AnalyticsDashboard) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics := map[string]interface{}{
		"queryStats":       ad.queryAnalytics.GetQueryStats(),
		"performanceStats": ad.performanceMonitor.GetMetrics(),
		"timestamp":        time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		http.Error(w, "Failed to encode metrics", http.StatusInternalServerError)
		return
	}
}

// QueriesHandler returns query analytics data
func (ad *AnalyticsDashboard) QueriesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get query parameters
	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil || parsedLimit != 1 {
			limit = 10
		}
	}

	queryType := r.URL.Query().Get("type")
	if queryType == "" {
		queryType = "top"
	}

	var queries interface{}
	switch queryType {
	case "top":
		queries = ad.queryAnalytics.GetTopQueries(limit)
	case "slowest":
		queries = ad.queryAnalytics.GetSlowestQueries(limit)
	case "complex":
		queries = ad.queryAnalytics.GetMostComplexQueries(limit)
	case "errors":
		queries = ad.queryAnalytics.GetQueriesWithErrors(limit)
	default:
		queries = ad.queryAnalytics.GetTopQueries(limit)
	}

	response := map[string]interface{}{
		"queries":   queries,
		"type":      queryType,
		"limit":     limit,
		"timestamp": time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// PerformanceHandler returns performance analytics data
func (ad *AnalyticsDashboard) PerformanceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	performance := ad.performanceMonitor.GetPerformanceReport()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(performance); err != nil {
		http.Error(w, "Failed to encode performance data", http.StatusInternalServerError)
		return
	}
}

// InsightsHandler returns insights and recommendations
func (ad *AnalyticsDashboard) InsightsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	insights := map[string]interface{}{
		"queryInsights":       ad.queryAnalytics.GetQueryInsights(),
		"performanceInsights": ad.performanceMonitor.GetPerformanceInsights(),
		"timestamp":           time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(insights); err != nil {
		http.Error(w, "Failed to encode insights", http.StatusInternalServerError)
		return
	}
}

// ExportHandler exports analytics data
func (ad *AnalyticsDashboard) ExportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !ad.config.EnableExport {
		http.Error(w, "Export is disabled", http.StatusForbidden)
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	var data []byte
	var err error

	switch format {
	case "json":
		data, err = ad.exportJSON()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=analytics.json")
	case "csv":
		data, err = ad.exportCSV()
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=analytics.csv")
	default:
		http.Error(w, "Unsupported format", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, "Export failed", http.StatusInternalServerError)
		return
	}

	if _, err := w.Write(data); err != nil {
		http.Error(w, "Failed to write export data", http.StatusInternalServerError)
		return
	}
}

// WebSocketHandler handles WebSocket connections for real-time updates
func (ad *AnalyticsDashboard) WebSocketHandler(w http.ResponseWriter, r *http.Request) {
	// This would implement WebSocket functionality for real-time updates
	// For now, return a simple response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"message": "WebSocket endpoint - implementation pending",
	}); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// HealthHandler handles health check requests
func (ad *AnalyticsDashboard) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"service":   "shugo-analytics-dashboard",
	}

	if err := json.NewEncoder(w).Encode(health); err != nil {
		http.Error(w, "Failed to encode health status", http.StatusInternalServerError)
		return
	}
}

// generateDashboardHTML generates the HTML for the analytics dashboard
func (ad *AnalyticsDashboard) generateDashboardHTML() string {
	return `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Shugo GraphQL Analytics Dashboard</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            margin: 0;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
        }
        .header {
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            margin-bottom: 20px;
        }
        .grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 20px;
        }
        .card {
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .metric {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 10px 0;
            border-bottom: 1px solid #eee;
        }
        .metric:last-child {
            border-bottom: none;
        }
        .metric-label {
            font-weight: 500;
            color: #666;
        }
        .metric-value {
            font-weight: 600;
            color: #333;
        }
        .chart-container {
            position: relative;
            height: 300px;
            margin-top: 20px;
        }
        .insight {
            padding: 15px;
            margin: 10px 0;
            border-radius: 6px;
            border-left: 4px solid #007bff;
        }
        .insight.warning {
            border-left-color: #ffc107;
            background-color: #fff3cd;
        }
        .insight.critical {
            border-left-color: #dc3545;
            background-color: #f8d7da;
        }
        .refresh-btn {
            background: #007bff;
            color: white;
            border: none;
            padding: 10px 20px;
            border-radius: 4px;
            cursor: pointer;
            margin-left: 10px;
        }
        .refresh-btn:hover {
            background: #0056b3;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Shugo GraphQL Analytics Dashboard</h1>
            <p>Real-time monitoring and analytics for GraphQL queries and performance</p>
            <button class="refresh-btn" onclick="refreshData()">Refresh</button>
        </div>

        <div class="grid">
            <div class="card">
                <h3>Query Statistics</h3>
                <div id="query-stats">
                    <div class="metric">
                        <span class="metric-label">Total Queries</span>
                        <span class="metric-value" id="total-queries">-</span>
                    </div>
                    <div class="metric">
                        <span class="metric-label">Average Response Time</span>
                        <span class="metric-value" id="avg-response-time">-</span>
                    </div>
                    <div class="metric">
                        <span class="metric-label">Error Rate</span>
                        <span class="metric-value" id="error-rate">-</span>
                    </div>
                </div>
            </div>

            <div class="card">
                <h3>Performance Metrics</h3>
                <div id="performance-stats">
                    <div class="metric">
                        <span class="metric-label">Requests/Second</span>
                        <span class="metric-value" id="rps">-</span>
                    </div>
                    <div class="metric">
                        <span class="metric-label">Memory Usage</span>
                        <span class="metric-value" id="memory-usage">-</span>
                    </div>
                    <div class="metric">
                        <span class="metric-label">CPU Usage</span>
                        <span class="metric-value" id="cpu-usage">-</span>
                    </div>
                </div>
            </div>

            <div class="card">
                <h3>Top Queries</h3>
                <div id="top-queries">
                    <p>Loading...</p>
                </div>
            </div>

            <div class="card">
                <h3>Performance Insights</h3>
                <div id="insights">
                    <p>Loading...</p>
                </div>
            </div>
        </div>

        <div class="card">
            <h3>Response Time Distribution</h3>
            <div class="chart-container">
                <canvas id="response-time-chart"></canvas>
            </div>
        </div>
    </div>

    <script>
        let responseTimeChart;

        async function fetchData() {
            try {
                const [metricsResponse, queriesResponse, insightsResponse] = await Promise.all([
                    fetch('/api/metrics'),
                    fetch('/api/queries?type=top&limit=5'),
                    fetch('/api/insights')
                ]);

                const metrics = await metricsResponse.json();
                const queries = await queriesResponse.json();
                const insights = await insightsResponse.json();

                updateMetrics(metrics);
                updateQueries(queries);
                updateInsights(insights);
            } catch (error) {
                console.error('Failed to fetch data:', error);
            }
        }

        function updateMetrics(metrics) {
            const queryStats = metrics.queryStats;
            const performanceStats = metrics.performanceStats;

            document.getElementById('total-queries').textContent = queryStats.totalExecutions || 0;
            document.getElementById('avg-response-time').textContent = 
                queryStats.averageTime ? Math.round(queryStats.averageTime / 1000000) + 'ms' : '-';
            document.getElementById('error-rate').textContent = 
                queryStats.errorRate ? (queryStats.errorRate * 100).toFixed(2) + '%' : '-';

            document.getElementById('rps').textContent = 
                performanceStats.requestsPerSecond ? performanceStats.requestsPerSecond.toFixed(2) : '-';
            document.getElementById('memory-usage').textContent = 
                performanceStats.memoryUsage ? Math.round(performanceStats.memoryUsage / 1024 / 1024) + 'MB' : '-';
            document.getElementById('cpu-usage').textContent = 
                performanceStats.cpuUsage ? performanceStats.cpuUsage.toFixed(1) + '%' : '-';
        }

        function updateQueries(queries) {
            const container = document.getElementById('top-queries');
            if (queries.queries && queries.queries.length > 0) {
                container.innerHTML = queries.queries.map(query => 
                    '<div class="metric">' +
                        '<span class="metric-label">' + query.query.substring(0, 50) + '...</span>' +
                        '<span class="metric-value">' + query.count + ' times</span>' +
                    '</div>'
                ).join('');
            } else {
                container.innerHTML = '<p>No query data available</p>';
            }
        }

        function updateInsights(insights) {
            const container = document.getElementById('insights');
            const allInsights = [...(insights.queryInsights || []), ...(insights.performanceInsights || [])];
            
            if (allInsights.length > 0) {
                container.innerHTML = allInsights.map(insight => 
                    '<div class="insight ' + insight.severity + '">' +
                        '<strong>' + insight.title + '</strong>' +
                        '<p>' + insight.description + '</p>' +
                        '<p><em>Recommendation: ' + insight.recommendation + '</em></p>' +
                    '</div>'
                ).join('');
            } else {
                container.innerHTML = '<p>No insights available</p>';
            }
        }

        function refreshData() {
            fetchData();
        }

        // Initialize chart
        function initChart() {
            const ctx = document.getElementById('response-time-chart').getContext('2d');
            responseTimeChart = new Chart(ctx, {
                type: 'line',
                data: {
                    labels: [],
                    datasets: [{
                        label: 'Response Time (ms)',
                        data: [],
                        borderColor: 'rgb(75, 192, 192)',
                        tension: 0.1
                    }]
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    scales: {
                        y: {
                            beginAtZero: true
                        }
                    }
                }
            });
        }

        // Initialize dashboard
        document.addEventListener('DOMContentLoaded', function() {
            initChart();
            fetchData();
            
            // Auto-refresh every 30 seconds
            setInterval(fetchData, 30000);
        });
    </script>
</body>
</html>
`
}

// exportJSON exports analytics data in JSON format
func (ad *AnalyticsDashboard) exportJSON() ([]byte, error) {
	data := map[string]interface{}{
		"queryStats":       ad.queryAnalytics.GetQueryStats(),
		"performanceStats": ad.performanceMonitor.GetMetrics(),
		"topQueries":       ad.queryAnalytics.GetTopQueries(100),
		"slowestQueries":   ad.queryAnalytics.GetSlowestQueries(100),
		"insights":         ad.queryAnalytics.GetQueryInsights(),
		"exportedAt":       time.Now(),
	}

	return json.MarshalIndent(data, "", "  ")
}

// exportCSV exports analytics data in CSV format
func (ad *AnalyticsDashboard) exportCSV() ([]byte, error) {
	// This would implement CSV export functionality
	// For now, return a simple CSV header
	csv := "metric,value\n"
	csv += "total_queries," + fmt.Sprintf("%d", ad.queryAnalytics.GetQueryStats()["totalExecutions"]) + "\n"
	csv += "error_rate," + fmt.Sprintf("%.4f", ad.queryAnalytics.GetQueryStats()["errorRate"]) + "\n"
	csv += "average_response_time," + fmt.Sprintf("%v", ad.queryAnalytics.GetQueryStats()["averageTime"]) + "\n"

	return []byte(csv), nil
}

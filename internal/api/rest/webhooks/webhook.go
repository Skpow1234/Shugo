package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// WebhookEvent represents a webhook event
type WebhookEvent struct {
	ID        string                 `json:"id"`
	Event     string                 `json:"event"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Source    string                 `json:"source"`
}

// WebhookConfig holds webhook configuration
type WebhookConfig struct {
	URL         string            `json:"url"`
	Secret      string            `json:"secret"`
	Events      []string          `json:"events"`
	Headers     map[string]string `json:"headers,omitempty"`
	Active      bool              `json:"active"`
	RetryConfig *RetryConfig      `json:"retry_config,omitempty"`
}

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxRetries    int           `json:"max_retries"`
	InitialDelay  time.Duration `json:"initial_delay"`
	MaxDelay      time.Duration `json:"max_delay"`
	BackoffFactor float64       `json:"backoff_factor"`
}

// WebhookDelivery represents a webhook delivery attempt
type WebhookDelivery struct {
	ID         string        `json:"id"`
	WebhookID  string        `json:"webhook_id"`
	EventID    string        `json:"event_id"`
	URL        string        `json:"url"`
	Payload    []byte        `json:"payload"`
	StatusCode int           `json:"status_code"`
	Response   string        `json:"response"`
	Attempt    int           `json:"attempt"`
	Succeeded  bool          `json:"succeeded"`
	Error      string        `json:"error,omitempty"`
	Timestamp  time.Time     `json:"timestamp"`
	Duration   time.Duration `json:"duration"`
}

// WebhookManager manages webhooks
type WebhookManager struct {
	config     *ManagerConfig
	logger     *slog.Logger
	webhooks   map[string]*WebhookConfig
	deliveries map[string][]*WebhookDelivery
	eventQueue chan *WebhookEvent
	stopChan   chan struct{}
	wg         sync.WaitGroup
	mu         sync.RWMutex
}

// ManagerConfig holds manager configuration
type ManagerConfig struct {
	MaxWorkers      int
	QueueSize       int
	DeliveryTimeout time.Duration
	SignatureHeader string
	RetryConfig     *RetryConfig
	AllowLoopback   bool
}

// NewManager creates a new webhook manager
func NewManager(config *ManagerConfig, logger *slog.Logger) *WebhookManager {
	if config.RetryConfig == nil {
		config.RetryConfig = &RetryConfig{
			MaxRetries:    3,
			InitialDelay:  1 * time.Second,
			MaxDelay:      30 * time.Second,
			BackoffFactor: 2.0,
		}
	}

	wm := &WebhookManager{
		config:     config,
		logger:     logger,
		webhooks:   make(map[string]*WebhookConfig),
		deliveries: make(map[string][]*WebhookDelivery),
		eventQueue: make(chan *WebhookEvent, config.QueueSize),
		stopChan:   make(chan struct{}),
	}

	// Start workers
	for i := 0; i < config.MaxWorkers; i++ {
		wm.wg.Add(1)
		go wm.worker()
	}

	return wm
}

// RegisterWebhook registers a new webhook
func (wm *WebhookManager) RegisterWebhook(id string, config *WebhookConfig) error {
	if err := validateWebhookURL(config.URL, wm.config.AllowLoopback); err != nil {
		return err
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	if config.RetryConfig == nil {
		config.RetryConfig = wm.config.RetryConfig
	}

	wm.webhooks[id] = config
	wm.deliveries[id] = make([]*WebhookDelivery, 0)

	wm.logger.Info("Registered webhook", "id", id, "url", config.URL, "events", config.Events)
	return nil
}

// UnregisterWebhook unregisters a webhook
func (wm *WebhookManager) UnregisterWebhook(id string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	delete(wm.webhooks, id)
	delete(wm.deliveries, id)

	wm.logger.Info("Unregistered webhook", "id", id)
}

// PublishEvent publishes an event to all matching webhooks
func (wm *WebhookManager) PublishEvent(event *WebhookEvent) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	for id, webhook := range wm.webhooks {
		if !webhook.Active {
			continue
		}

		if wm.matchesEvent(webhook, event) {
			select {
			case wm.eventQueue <- event:
				wm.logger.Debug("Queued event for webhook", "event", event.Event, "webhook", id)
			default:
				wm.logger.Warn("Event queue full, dropping event", "event", event.Event, "webhook", id)
			}
		}
	}
}

// matchesEvent checks if a webhook should receive an event
func (wm *WebhookManager) matchesEvent(webhook *WebhookConfig, event *WebhookEvent) bool {
	if len(webhook.Events) == 0 {
		return true // No filter means all events
	}

	for _, webhookEvent := range webhook.Events {
		if webhookEvent == event.Event || webhookEvent == "*" {
			return true
		}
	}

	return false
}

// worker processes webhook deliveries
func (wm *WebhookManager) worker() {
	defer wm.wg.Done()

	for {
		select {
		case event := <-wm.eventQueue:
			wm.processEvent(event)
		case <-wm.stopChan:
			return
		}
	}
}

// processEvent processes a single event for all matching webhooks
func (wm *WebhookManager) processEvent(event *WebhookEvent) {
	wm.mu.RLock()
	webhooks := make(map[string]*WebhookConfig)
	for id, webhook := range wm.webhooks {
		if webhook.Active && wm.matchesEvent(webhook, event) {
			webhooks[id] = webhook
		}
	}
	wm.mu.RUnlock()

	for id, webhook := range webhooks {
		wm.deliverWebhook(id, webhook, event)
	}
}

// deliverWebhook delivers an event to a specific webhook
func (wm *WebhookManager) deliverWebhook(webhookID string, webhook *WebhookConfig, event *WebhookEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		wm.logger.Error("Failed to marshal event", "error", err, "event", event.ID)
		return
	}

	deliveryID := fmt.Sprintf("%s-%d", event.ID, time.Now().UnixNano())

	for attempt := 1; attempt <= webhook.RetryConfig.MaxRetries+1; attempt++ {
		delivery := wm.attemptDelivery(deliveryID, webhookID, webhook, event, payload, attempt)

		wm.mu.Lock()
		wm.deliveries[webhookID] = append(wm.deliveries[webhookID], delivery)
		wm.mu.Unlock()

		if delivery.Succeeded {
			wm.logger.Info("Webhook delivery succeeded",
				"webhook", webhookID,
				"event", event.Event,
				"attempt", attempt,
				"duration", delivery.Duration)
			return
		}

		if attempt <= webhook.RetryConfig.MaxRetries {
			delay := wm.calculateDelay(webhook.RetryConfig, attempt)
			wm.logger.Warn("Webhook delivery failed, retrying",
				"webhook", webhookID,
				"event", event.Event,
				"attempt", attempt,
				"delay", delay,
				"error", delivery.Error)

			select {
			case <-time.After(delay):
			case <-wm.stopChan:
				return
			}
		}
	}

	wm.logger.Error("Webhook delivery failed permanently",
		"webhook", webhookID,
		"event", event.Event,
		"attempts", webhook.RetryConfig.MaxRetries+1)
}

// attemptDelivery attempts to deliver a webhook
func (wm *WebhookManager) attemptDelivery(deliveryID, webhookID string, webhook *WebhookConfig, event *WebhookEvent, payload []byte, attempt int) *WebhookDelivery {
	start := time.Now()

	req, err := http.NewRequest("POST", webhook.URL, bytes.NewReader(payload))
	if err != nil {
		return &WebhookDelivery{
			ID:        deliveryID,
			WebhookID: webhookID,
			EventID:   event.ID,
			URL:       webhook.URL,
			Payload:   payload,
			Attempt:   attempt,
			Succeeded: false,
			Error:     err.Error(),
			Timestamp: start,
			Duration:  time.Since(start),
		}
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Shugo-Webhook/1.0")

	// Add custom headers
	for key, value := range webhook.Headers {
		if !safeWebhookHeader(key, value) {
			continue
		}
		req.Header.Set(key, value)
	}

	// Add signature if secret is provided
	if webhook.Secret != "" {
		signature := wm.generateSignature(payload, webhook.Secret)
		req.Header.Set(wm.config.SignatureHeader, signature)
	}

	// Execute request
	client := newWebhookClient(wm.config.DeliveryTimeout, wm.config.AllowLoopback)
	resp, err := client.Do(req)
	duration := time.Since(start)

	if err != nil {
		return &WebhookDelivery{
			ID:        deliveryID,
			WebhookID: webhookID,
			EventID:   event.ID,
			URL:       webhook.URL,
			Payload:   payload,
			Attempt:   attempt,
			Succeeded: false,
			Error:     err.Error(),
			Timestamp: start,
			Duration:  duration,
		}
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			wm.logger.Warn("Failed to close response body", "error", err)
		}
	}()

	responseBody := ""
	if resp.Body != nil {
		buf := make([]byte, 1024)
		n, _ := resp.Body.Read(buf)
		responseBody = string(buf[:n])
	}

	succeeded := resp.StatusCode >= 200 && resp.StatusCode < 300

	return &WebhookDelivery{
		ID:         deliveryID,
		WebhookID:  webhookID,
		EventID:    event.ID,
		URL:        webhook.URL,
		Payload:    payload,
		StatusCode: resp.StatusCode,
		Response:   responseBody,
		Attempt:    attempt,
		Succeeded:  succeeded,
		Timestamp:  start,
		Duration:   duration,
	}
}

// generateSignature generates an HMAC signature for the payload
func (wm *WebhookManager) generateSignature(payload []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(payload)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

// calculateDelay calculates the delay for the next retry attempt
func (wm *WebhookManager) calculateDelay(config *RetryConfig, attempt int) time.Duration {
	delay := float64(config.InitialDelay) * pow(config.BackoffFactor, float64(attempt-1))
	if delay > float64(config.MaxDelay) {
		delay = float64(config.MaxDelay)
	}
	return time.Duration(delay)
}

// pow is a simple power function
func pow(base, exp float64) float64 {
	result := 1.0
	for i := 0; i < int(exp); i++ {
		result *= base
	}
	return result
}

// GetDeliveries returns delivery history for a webhook
func (wm *WebhookManager) GetDeliveries(webhookID string) []*WebhookDelivery {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	if deliveries, exists := wm.deliveries[webhookID]; exists {
		// Return a copy to avoid race conditions
		result := make([]*WebhookDelivery, len(deliveries))
		copy(result, deliveries)
		return result
	}

	return []*WebhookDelivery{}
}

// GetStats returns webhook statistics
func (wm *WebhookManager) GetStats() map[string]interface{} {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	totalDeliveries := 0
	successfulDeliveries := 0
	failedDeliveries := 0

	for _, deliveries := range wm.deliveries {
		for _, delivery := range deliveries {
			totalDeliveries++
			if delivery.Succeeded {
				successfulDeliveries++
			} else {
				failedDeliveries++
			}
		}
	}

	return map[string]interface{}{
		"total_webhooks":        len(wm.webhooks),
		"total_deliveries":      totalDeliveries,
		"successful_deliveries": successfulDeliveries,
		"failed_deliveries":     failedDeliveries,
		"success_rate":          wm.calculateSuccessRate(successfulDeliveries, totalDeliveries),
		"queue_size":            len(wm.eventQueue),
	}
}

// calculateSuccessRate calculates the success rate
func (wm *WebhookManager) calculateSuccessRate(successful, total int) float64 {
	if total == 0 {
		return 0.0
	}
	return float64(successful) / float64(total) * 100.0
}

// Stop stops the webhook manager
func (wm *WebhookManager) Stop(ctx context.Context) error {
	close(wm.stopChan)
	wm.wg.Wait()
	return nil
}

// ListWebhooks returns all registered webhooks
func (wm *WebhookManager) ListWebhooks() map[string]*WebhookConfig {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	webhooks := make(map[string]*WebhookConfig)
	for id, webhook := range wm.webhooks {
		webhooks[id] = webhook
	}

	return webhooks
}

// ValidateWebhookSignature validates a webhook signature
func (wm *WebhookManager) ValidateWebhookSignature(webhookID string, payload []byte, signature string) bool {
	wm.mu.RLock()
	webhook, exists := wm.webhooks[webhookID]
	wm.mu.RUnlock()

	if !exists || webhook.Secret == "" {
		return false
	}

	expectedSignature := wm.generateSignature(payload, webhook.Secret)
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

func validateWebhookURL(raw string, allowLoopback bool) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("invalid webhook url")
	}
	if parsed.User != nil {
		return fmt.Errorf("webhook url must not include credentials")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return nil
	case "http":
		if allowLoopback && isLoopbackHost(parsed.Hostname()) {
			return nil
		}
		return fmt.Errorf("webhook url must use https")
	default:
		return fmt.Errorf("webhook url scheme is not allowed")
	}
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func blockedWebhookIP(ip net.IP, allowLoopback bool) bool {
	if ip.IsLoopback() {
		return !allowLoopback
	}
	return ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

func safeWebhookHeader(key, value string) bool {
	if key == "" || strings.ContainsAny(key, "\r\n") || strings.ContainsAny(value, "\r\n") {
		return false
	}
	switch strings.ToLower(key) {
	case "host", "authorization", "cookie", "content-length", "transfer-encoding", "connection", "x-forwarded-for", "x-forwarded-host", "x-real-ip":
		return false
	default:
		return true
	}
}

func newWebhookClient(timeout time.Duration, allowLoopback bool) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("webhook host has no addresses")
			}
			for _, ip := range ips {
				if blockedWebhookIP(ip.IP, allowLoopback) {
					return nil, fmt.Errorf("webhook target address is not allowed")
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

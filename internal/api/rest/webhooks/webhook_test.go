package webhooks

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func createTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

func TestWebhookManagerBasic(t *testing.T) {
	config := &ManagerConfig{
		MaxWorkers:      2,
		QueueSize:       10,
		DeliveryTimeout: 5 * time.Second,
		SignatureHeader: "X-Hub-Signature-256",
		AllowLoopback:   true,
	}

	wm := NewManager(config, createTestLogger())
	defer func() {
		if err := wm.Stop(context.Background()); err != nil {
			t.Errorf("Failed to stop webhook manager: %v", err)
		}
	}()

	// Test registering a webhook
	webhookConfig := &WebhookConfig{
		URL:    "https://example.com/webhook",
		Events: []string{"file.uploaded", "file.deleted"},
		Active: true,
	}

	err := wm.RegisterWebhook("test-webhook", webhookConfig)
	if err != nil {
		t.Fatalf("Failed to register webhook: %v", err)
	}

	// Test listing webhooks
	webhooks := wm.ListWebhooks()
	if len(webhooks) != 1 {
		t.Errorf("Expected 1 webhook, got %d", len(webhooks))
	}

	if _, exists := webhooks["test-webhook"]; !exists {
		t.Error("test-webhook should exist")
	}

	// Test unregistering webhook
	wm.UnregisterWebhook("test-webhook")
	webhooks = wm.ListWebhooks()
	if len(webhooks) != 0 {
		t.Errorf("Expected 0 webhooks after unregister, got %d", len(webhooks))
	}
}

func TestWebhookDelivery(t *testing.T) {
	// Create a test server to receive webhooks
	receivedEvents := make(chan *WebhookEvent, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
			return
		}

		var event WebhookEvent
		if err := json.Unmarshal(body, &event); err != nil {
			t.Errorf("Failed to unmarshal event: %v", err)
			return
		}

		receivedEvents <- &event
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	config := &ManagerConfig{
		MaxWorkers:      1,
		QueueSize:       10,
		DeliveryTimeout: 5 * time.Second,
		SignatureHeader: "X-Hub-Signature-256",
		AllowLoopback:   true,
	}

	wm := NewManager(config, createTestLogger())
	defer func() {
		if err := wm.Stop(context.Background()); err != nil {
			t.Errorf("Failed to stop webhook manager: %v", err)
		}
	}()

	// Register webhook
	webhookConfig := &WebhookConfig{
		URL:    server.URL,
		Events: []string{"file.uploaded"},
		Active: true,
	}

	err := wm.RegisterWebhook("test-webhook", webhookConfig)
	if err != nil {
		t.Fatalf("Failed to register webhook: %v", err)
	}

	// Publish event
	event := &WebhookEvent{
		ID:        "test-event-123",
		Event:     "file.uploaded",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"file_key": "file_123",
			"name":     "test.pdf",
		},
		Source: "test",
	}

	wm.PublishEvent(event)

	// Wait for delivery
	select {
	case received := <-receivedEvents:
		if received.ID != event.ID {
			t.Errorf("Expected event ID %s, got %s", event.ID, received.ID)
		}
		if received.Event != event.Event {
			t.Errorf("Expected event type %s, got %s", event.Event, received.Event)
		}
	case <-time.After(2 * time.Second):
		t.Error("Webhook delivery timed out")
	}

	// Wait a bit more for the delivery to be recorded
	time.Sleep(100 * time.Millisecond)

	// Check delivery history
	deliveries := wm.GetDeliveries("test-webhook")
	if len(deliveries) != 1 {
		t.Errorf("Expected 1 delivery, got %d", len(deliveries))
		return // Avoid panic if no deliveries
	}

	delivery := deliveries[0]
	if !delivery.Succeeded {
		t.Errorf("Delivery should have succeeded, got error: %s", delivery.Error)
	}
	if delivery.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", delivery.StatusCode)
	}
}

func TestWebhookRetry(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &ManagerConfig{
		MaxWorkers:      1,
		QueueSize:       10,
		DeliveryTimeout: 5 * time.Second,
		SignatureHeader: "X-Hub-Signature-256",
		AllowLoopback:   true,
		RetryConfig: &RetryConfig{
			MaxRetries:    2,
			InitialDelay:  10 * time.Millisecond,
			MaxDelay:      100 * time.Millisecond,
			BackoffFactor: 2.0,
		},
	}

	wm := NewManager(config, createTestLogger())
	defer func() {
		if err := wm.Stop(context.Background()); err != nil {
			t.Errorf("Failed to stop webhook manager: %v", err)
		}
	}()

	// Register webhook
	webhookConfig := &WebhookConfig{
		URL:    server.URL,
		Events: []string{"file.uploaded"},
		Active: true,
	}

	err := wm.RegisterWebhook("test-webhook", webhookConfig)
	if err != nil {
		t.Fatalf("Failed to register webhook: %v", err)
	}

	// Publish event
	event := &WebhookEvent{
		ID:        "test-event-retry",
		Event:     "file.uploaded",
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"test": true},
		Source:    "test",
	}

	wm.PublishEvent(event)

	// Wait for completion
	time.Sleep(500 * time.Millisecond)

	// Check delivery history
	deliveries := wm.GetDeliveries("test-webhook")
	if len(deliveries) != 3 { // Initial attempt + 2 retries
		t.Errorf("Expected 3 deliveries, got %d", len(deliveries))
	}

	// Check that final delivery succeeded
	finalDelivery := deliveries[len(deliveries)-1]
	if !finalDelivery.Succeeded {
		t.Errorf("Final delivery should have succeeded")
	}
}

func TestWebhookSignature(t *testing.T) {
	config := &ManagerConfig{
		MaxWorkers:      1,
		QueueSize:       10,
		DeliveryTimeout: 5 * time.Second,
		SignatureHeader: "X-Hub-Signature-256",
		AllowLoopback:   true,
	}

	wm := NewManager(config, createTestLogger())
	defer func() {
		if err := wm.Stop(context.Background()); err != nil {
			t.Errorf("Failed to stop webhook manager: %v", err)
		}
	}()

	// Register webhook with secret
	webhookConfig := &WebhookConfig{
		URL:    "https://example.com",
		Secret: "test-secret",
		Events: []string{"file.uploaded"},
		Active: true,
	}

	err := wm.RegisterWebhook("test-webhook", webhookConfig)
	if err != nil {
		t.Fatalf("Failed to register webhook: %v", err)
	}

	// Test signature validation
	payload := []byte(`{"test": "data"}`)
	validSignature := wm.generateSignature(payload, "test-secret")

	if !wm.ValidateWebhookSignature("test-webhook", payload, validSignature) {
		t.Error("Valid signature should be accepted")
	}

	if wm.ValidateWebhookSignature("test-webhook", payload, "invalid-signature") {
		t.Error("Invalid signature should be rejected")
	}

	if wm.ValidateWebhookSignature("nonexistent-webhook", payload, validSignature) {
		t.Error("Signature for nonexistent webhook should be rejected")
	}
}

func TestWebhookEventFiltering(t *testing.T) {
	eventsReceived := make(chan string, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var event WebhookEvent
		if err := json.Unmarshal(body, &event); err != nil {
			t.Errorf("Failed to unmarshal webhook event: %v", err)
			return
		}
		eventsReceived <- event.Event
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &ManagerConfig{
		MaxWorkers:      1,
		QueueSize:       10,
		DeliveryTimeout: 5 * time.Second,
		SignatureHeader: "X-Hub-Signature-256",
		AllowLoopback:   true,
	}

	wm := NewManager(config, createTestLogger())
	defer func() {
		if err := wm.Stop(context.Background()); err != nil {
			t.Errorf("Failed to stop webhook manager: %v", err)
		}
	}()

	// Register webhook that only listens to file.uploaded events
	webhookConfig := &WebhookConfig{
		URL:    server.URL,
		Events: []string{"file.uploaded"},
		Active: true,
	}

	err := wm.RegisterWebhook("test-webhook", webhookConfig)
	if err != nil {
		t.Fatalf("Failed to register webhook: %v", err)
	}

	// Publish different events
	events := []*WebhookEvent{
		{ID: "1", Event: "file.uploaded", Timestamp: time.Now()},
		{ID: "2", Event: "file.deleted", Timestamp: time.Now()},
		{ID: "3", Event: "peer.joined", Timestamp: time.Now()},
	}

	for _, event := range events {
		wm.PublishEvent(event)
	}

	// Wait a bit for processing
	time.Sleep(100 * time.Millisecond)

	// Check received events
	receivedCount := 0
	timeout := time.After(1 * time.Second)

	for receivedCount < 1 {
		select {
		case eventType := <-eventsReceived:
			if eventType != "file.uploaded" {
				t.Errorf("Should only receive file.uploaded events, got %s", eventType)
			}
			receivedCount++
		case <-timeout:
			t.Error("Timed out waiting for webhook delivery")
			return
		}
	}

	if receivedCount != 1 {
		t.Errorf("Expected 1 event, got %d", receivedCount)
	}
}

func TestWebhookStats(t *testing.T) {
	config := &ManagerConfig{
		MaxWorkers:      1,
		QueueSize:       10,
		DeliveryTimeout: 5 * time.Second,
		SignatureHeader: "X-Hub-Signature-256",
		AllowLoopback:   true,
	}

	wm := NewManager(config, createTestLogger())
	defer func() {
		if err := wm.Stop(context.Background()); err != nil {
			t.Errorf("Failed to stop webhook manager: %v", err)
		}
	}()

	// Register a webhook
	webhookConfig := &WebhookConfig{
		URL:    "https://example.com",
		Events: []string{"file.uploaded"},
		Active: true,
	}

	err := wm.RegisterWebhook("test-webhook", webhookConfig)
	if err != nil {
		t.Fatalf("Failed to register webhook: %v", err)
	}

	// Get initial stats
	stats := wm.GetStats()

	if stats["total_webhooks"] != 1 {
		t.Errorf("Expected 1 total webhook, got %v", stats["total_webhooks"])
	}

	if stats["total_deliveries"] != 0 {
		t.Errorf("Expected 0 total deliveries initially, got %v", stats["total_deliveries"])
	}
}

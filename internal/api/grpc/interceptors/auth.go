package interceptors

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Context key types to avoid collisions
type contextKey string

const (
	userIDKey      contextKey = "user_id"
	userRolesKey   contextKey = "user_roles"
	tokenExpiryKey contextKey = "token_expiry"
)

// AuthConfig represents authentication configuration
type AuthConfig struct {
	SecretKey      string
	TokenExpiry    time.Duration
	Issuer         string
	Audience       string
	RequireAuth    bool
	SkipMethods    []string
	RateLimitRPS   int
	RateLimitBurst int
}

// DefaultAuthConfig returns the default authentication configuration
func DefaultAuthConfig() *AuthConfig {
	return &AuthConfig{
		SecretKey:      authSecretFromEnv(),
		TokenExpiry:    24 * time.Hour,
		Issuer:         "shugo",
		Audience:       "shugo-api",
		RequireAuth:    true,
		SkipMethods:    []string{"/shugo.ShugoService/HealthCheck"},
		RateLimitRPS:   100,
		RateLimitBurst: 200,
	}
}

// AuthInterceptor provides authentication for gRPC services
type AuthInterceptor struct {
	config      *AuthConfig
	logger      *slog.Logger
	RequireAuth bool
}

// NewAuthInterceptor creates a new authentication interceptor
func NewAuthInterceptor(config *AuthConfig, logger *slog.Logger) *AuthInterceptor {
	if config == nil {
		config = DefaultAuthConfig()
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &AuthInterceptor{
		config:      config,
		logger:      logger,
		RequireAuth: config.RequireAuth,
	}
}

// UnaryAuthInterceptor returns a unary server interceptor for authentication
func (ai *AuthInterceptor) UnaryAuthInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Check if method should skip authentication
		if ai.shouldSkipAuth(info.FullMethod) {
			return handler(ctx, req)
		}

		// Extract and validate token
		claims, err := ai.validateToken(ctx)
		if err != nil {
			ai.logger.Error("Authentication failed", "method", info.FullMethod, "error", err)
			return nil, status.Error(codes.Unauthenticated, "authentication failed")
		}

		// Add claims to context
		ctx = context.WithValue(ctx, userIDKey, claims.UserID)
		ctx = context.WithValue(ctx, userRolesKey, claims.Roles)
		ctx = context.WithValue(ctx, tokenExpiryKey, claims.ExpiresAt)

		// Log successful authentication
		ai.logger.Info("Authentication successful", "method", info.FullMethod, "user_id", claims.UserID)

		return handler(ctx, req)
	}
}

// StreamAuthInterceptor returns a stream server interceptor for authentication
func (ai *AuthInterceptor) StreamAuthInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Check if method should skip authentication
		if ai.shouldSkipAuth(info.FullMethod) {
			return handler(srv, ss)
		}

		// Extract and validate token
		claims, err := ai.validateToken(ss.Context())
		if err != nil {
			ai.logger.Error("Stream authentication failed", "method", info.FullMethod, "error", err)
			return status.Error(codes.Unauthenticated, "authentication failed")
		}

		// Add claims to context
		ctx := context.WithValue(ss.Context(), userIDKey, claims.UserID)
		ctx = context.WithValue(ctx, userRolesKey, claims.Roles)
		ctx = context.WithValue(ctx, tokenExpiryKey, claims.ExpiresAt)

		// Create new stream with updated context
		wrappedStream := &wrappedServerStream{
			ServerStream: ss,
			ctx:          ctx,
		}

		// Log successful authentication
		ai.logger.Info("Stream authentication successful", "method", info.FullMethod, "user_id", claims.UserID)

		return handler(srv, wrappedStream)
	}
}

// shouldSkipAuth checks if the method should skip authentication
func (ai *AuthInterceptor) shouldSkipAuth(method string) bool {
	if !ai.config.RequireAuth {
		return true
	}

	for _, skipMethod := range ai.config.SkipMethods {
		if method == skipMethod {
			return true
		}
	}

	return false
}

// validateToken validates the authentication token
func (ai *AuthInterceptor) validateToken(ctx context.Context) (*TokenClaims, error) {
	// Extract metadata
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing metadata")
	}

	// Extract authorization header
	authHeaders := md.Get("authorization")
	if len(authHeaders) == 0 {
		return nil, fmt.Errorf("missing authorization header")
	}

	authHeader := authHeaders[0]
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, fmt.Errorf("invalid authorization header format")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")

	// Validate token
	claims, err := ai.parseToken(token)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	// Check token expiry
	if time.Now().After(claims.ExpiresAt) {
		return nil, fmt.Errorf("token expired")
	}

	// Check token issuer
	if claims.Issuer != ai.config.Issuer {
		return nil, fmt.Errorf("invalid token issuer")
	}

	// Check token audience
	if claims.Audience != ai.config.Audience {
		return nil, fmt.Errorf("invalid token audience")
	}

	return claims, nil
}

// parseToken parses and validates a JWT-like token
func (ai *AuthInterceptor) parseToken(token string) (*TokenClaims, error) {
	// Split token into parts
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	// Decode header and payload
	_, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid token header: %w", err)
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid token payload: %w", err)
	}

	if ai.config.SecretKey == "" {
		return nil, fmt.Errorf("auth secret is not configured")
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid token signature: %w", err)
	}
	expectedSignature := ai.signatureBytes(parts[0] + "." + parts[1])
	if subtle.ConstantTimeCompare(signature, expectedSignature) != 1 {
		return nil, fmt.Errorf("invalid token signature")
	}

	// Parse claims from payload
	claims, err := parseClaims(payload)
	if err != nil {
		return nil, fmt.Errorf("invalid token claims: %w", err)
	}

	return claims, nil
}

func (ai *AuthInterceptor) signatureBytes(data string) []byte {
	h := hmac.New(sha256.New, []byte(ai.config.SecretKey))
	h.Write([]byte(data))
	return h.Sum(nil)
}

func authSecretFromEnv() string {
	if secret := os.Getenv("SHUGO_AUTH_SECRET"); secret != "" {
		return secret
	}
	return os.Getenv("SHUGO_AUTH_TOKEN")
}

// TokenClaims represents the claims in a token
type TokenClaims struct {
	UserID    string    `json:"user_id"`
	Roles     []string  `json:"roles"`
	Issuer    string    `json:"iss"`
	Audience  string    `json:"aud"`
	IssuedAt  time.Time `json:"iat"`
	ExpiresAt time.Time `json:"exp"`
}

func parseClaims(payload []byte) (*TokenClaims, error) {
	var claims TokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	if claims.UserID == "" {
		return nil, fmt.Errorf("missing user id")
	}
	return &claims, nil
}

// wrappedServerStream wraps a ServerStream with a custom context
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the custom context
func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}

// GenerateToken generates a new authentication token
func (ai *AuthInterceptor) GenerateToken(userID string, roles []string) (string, error) {
	now := time.Now()
	claims := &TokenClaims{
		UserID:    userID,
		Roles:     roles,
		Issuer:    ai.config.Issuer,
		Audience:  ai.config.Audience,
		IssuedAt:  now,
		ExpiresAt: now.Add(ai.config.TokenExpiry),
	}

	if ai.config.SecretKey == "" {
		return "", fmt.Errorf("auth secret is not configured")
	}

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode token claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signature := base64.RawURLEncoding.EncodeToString(ai.signatureBytes(header + "." + payload))
	return header + "." + payload + "." + signature, nil
}

// ValidateUserRole validates if a user has a specific role
func (ai *AuthInterceptor) ValidateUserRole(ctx context.Context, requiredRole string) bool {
	roles, ok := ctx.Value(userRolesKey).([]string)
	if !ok {
		return false
	}

	for _, role := range roles {
		if role == requiredRole {
			return true
		}
	}

	return false
}

// GetUserID extracts the user ID from the context
func (ai *AuthInterceptor) GetUserID(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDKey).(string)
	return userID, ok
}

// GetUserRoles extracts the user roles from the context
func (ai *AuthInterceptor) GetUserRoles(ctx context.Context) ([]string, bool) {
	roles, ok := ctx.Value(userRolesKey).([]string)
	return roles, ok
}

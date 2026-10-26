package security

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// AuthManager manages authentication and authorization
type AuthManager struct {
	configDir string
	users     map[string]*User
	roles     map[string]*Role
	sessions  map[string]*Session
}

// User represents a system user
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"password_hash"`
	MFAEnabled   bool      `json:"mfa_enabled"`
	MFASecret    string    `json:"mfa_secret,omitempty"`
	Roles        []string  `json:"roles"`
	CreatedAt    time.Time `json:"created_at"`
	LastLogin    time.Time `json:"last_login"`
	Active       bool      `json:"active"`
}

// Role represents a user role with permissions
type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
}

// Session represents an active user session
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	Active    bool      `json:"active"`
}

// Permission represents a system permission
type Permission struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
}

// New creates a new authentication manager
func New(configDir string) *AuthManager {
	am := &AuthManager{
		configDir: configDir,
		users:     make(map[string]*User),
		roles:     make(map[string]*Role),
		sessions:  make(map[string]*Session),
	}

	am.initializeDefaultRoles()
	_ = am.loadUsers() // Ignore error for initialization
	_ = am.loadRoles() // Ignore error for initialization
	return am
}

// initializeDefaultRoles creates default system roles
func (am *AuthManager) initializeDefaultRoles() {
	// Admin role
	am.roles["admin"] = &Role{
		ID:          "admin",
		Name:        "Administrator",
		Description: "Full system access",
		Permissions: []string{
			"user.create", "user.read", "user.update", "user.delete",
			"file.create", "file.read", "file.update", "file.delete",
			"system.config", "system.monitor", "system.backup",
			"security.audit", "security.certificates",
		},
		CreatedAt: time.Now(),
	}

	// User role
	am.roles["user"] = &Role{
		ID:          "user",
		Name:        "User",
		Description: "Standard user access",
		Permissions: []string{
			"file.create", "file.read", "file.update", "file.delete",
		},
		CreatedAt: time.Now(),
	}

	// Read-only role
	am.roles["readonly"] = &Role{
		ID:          "readonly",
		Name:        "Read Only",
		Description: "Read-only access",
		Permissions: []string{
			"file.read",
		},
		CreatedAt: time.Now(),
	}
}

// CreateUser creates a new user
func (am *AuthManager) CreateUser(username, email, password string, roles []string) (*User, error) {
	// Check if user already exists
	for _, user := range am.users {
		if user.Username == username || user.Email == email {
			return nil, fmt.Errorf("user with username or email already exists")
		}
	}

	// Validate roles
	for _, roleName := range roles {
		if _, exists := am.roles[roleName]; !exists {
			return nil, fmt.Errorf("role '%s' does not exist", roleName)
		}
	}

	// Hash password
	passwordHash, err := am.hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Generate user ID
	userID := am.generateID()

	user := &User{
		ID:           userID,
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		MFAEnabled:   false,
		Roles:        roles,
		CreatedAt:    time.Now(),
		Active:       true,
	}

	am.users[userID] = user
	_ = am.saveUsers() // Ignore error for demo purposes

	return user, nil
}

// Authenticate authenticates a user with username and password
func (am *AuthManager) Authenticate(username, password string) (*User, error) {
	var user *User
	for _, u := range am.users {
		if u.Username == username && u.Active {
			user = u
			break
		}
	}

	if user == nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	if !am.verifyPassword(password, user.PasswordHash) {
		return nil, fmt.Errorf("invalid credentials")
	}

	user.LastLogin = time.Now()
	_ = am.saveUsers() // Ignore error for demo purposes

	return user, nil
}

// EnableMFA enables MFA for a user
func (am *AuthManager) EnableMFA(userID string) (string, error) {
	user, exists := am.users[userID]
	if !exists {
		return "", fmt.Errorf("user not found")
	}

	// Generate MFA secret (simplified - in real implementation, use proper TOTP library)
	secret := am.generateID()

	user.MFAEnabled = true
	user.MFASecret = secret
	_ = am.saveUsers() // Ignore error for demo purposes

	// Return a placeholder URL - in real implementation, this would be a proper QR code URL
	qrURL := fmt.Sprintf("otpauth://totp/Shugo:%s?secret=%s&issuer=Shugo", user.Username, secret)

	return qrURL, nil
}

// VerifyMFA verifies MFA token
func (am *AuthManager) VerifyMFA(userID, token string) error {
	user, exists := am.users[userID]
	if !exists {
		return fmt.Errorf("user not found")
	}

	if !user.MFAEnabled {
		return fmt.Errorf("MFA not enabled for user")
	}

	// Simplified MFA verification - in real implementation, use proper TOTP validation
	// For demo purposes, accept any 6-digit token
	if len(token) != 6 {
		return fmt.Errorf("invalid MFA token format")
	}

	// In a real implementation, you would validate the TOTP token here
	// For now, we'll just check if it's a 6-digit number
	for _, char := range token {
		if char < '0' || char > '9' {
			return fmt.Errorf("invalid MFA token")
		}
	}

	return nil
}

// CreateSession creates a new user session
func (am *AuthManager) CreateSession(userID, ipAddress, userAgent string) (*Session, error) {
	user, exists := am.users[userID]
	if !exists {
		return nil, fmt.Errorf("user not found")
	}

	if !user.Active {
		return nil, fmt.Errorf("user account is inactive")
	}

	// Generate session token
	token := am.generateToken()
	sessionID := am.generateID()

	session := &Session{
		ID:        sessionID,
		UserID:    userID,
		Token:     token,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour), // 24 hour expiry
		IPAddress: ipAddress,
		UserAgent: userAgent,
		Active:    true,
	}

	am.sessions[sessionID] = session
	_ = am.saveSessions() // Ignore error for demo purposes

	return session, nil
}

// ValidateSession validates a session token
func (am *AuthManager) ValidateSession(token string) (*Session, *User, error) {
	var session *Session
	for _, s := range am.sessions {
		if s.Token == token && s.Active && s.ExpiresAt.After(time.Now()) {
			session = s
			break
		}
	}

	if session == nil {
		return nil, nil, fmt.Errorf("invalid or expired session")
	}

	user, exists := am.users[session.UserID]
	if !exists {
		return nil, nil, fmt.Errorf("user not found")
	}

	return session, user, nil
}

// CheckPermission checks if a user has a specific permission
func (am *AuthManager) CheckPermission(userID, permission string) bool {
	user, exists := am.users[userID]
	if !exists {
		return false
	}

	for _, roleName := range user.Roles {
		role, exists := am.roles[roleName]
		if !exists {
			continue
		}

		for _, perm := range role.Permissions {
			if perm == permission {
				return true
			}
		}
	}

	return false
}

// GetUserPermissions returns all permissions for a user
func (am *AuthManager) GetUserPermissions(userID string) []string {
	user, exists := am.users[userID]
	if !exists {
		return nil
	}

	var permissions []string
	permissionSet := make(map[string]bool)

	for _, roleName := range user.Roles {
		role, exists := am.roles[roleName]
		if !exists {
			continue
		}

		for _, perm := range role.Permissions {
			if !permissionSet[perm] {
				permissions = append(permissions, perm)
				permissionSet[perm] = true
			}
		}
	}

	return permissions
}

// ListUsers returns all users
func (am *AuthManager) ListUsers() []*User {
	var users []*User
	for _, user := range am.users {
		users = append(users, user)
	}
	return users
}

// ListRoles returns all roles
func (am *AuthManager) ListRoles() []*Role {
	var roles []*Role
	for _, role := range am.roles {
		roles = append(roles, role)
	}
	return roles
}

// UpdateUserRoles updates user roles
func (am *AuthManager) UpdateUserRoles(userID string, roles []string) error {
	user, exists := am.users[userID]
	if !exists {
		return fmt.Errorf("user not found")
	}

	// Validate roles
	for _, roleName := range roles {
		if _, exists := am.roles[roleName]; !exists {
			return fmt.Errorf("role '%s' does not exist", roleName)
		}
	}

	user.Roles = roles
	_ = am.saveUsers() // Ignore error for demo purposes

	return nil
}

// DeactivateUser deactivates a user account
func (am *AuthManager) DeactivateUser(userID string) error {
	user, exists := am.users[userID]
	if !exists {
		return fmt.Errorf("user not found")
	}

	user.Active = false

	// Deactivate all sessions for this user
	for _, session := range am.sessions {
		if session.UserID == userID {
			session.Active = false
		}
	}

	_ = am.saveUsers()    // Ignore error for demo purposes
	_ = am.saveSessions() // Ignore error for demo purposes

	return nil
}

// Utility functions
func (am *AuthManager) hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt: %w", err)
	}
	return string(hash), nil
}

func (am *AuthManager) verifyPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (am *AuthManager) generateID() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes) // Ignore error for demo purposes
	return fmt.Sprintf("%x", bytes)
}

func (am *AuthManager) generateToken() string {
	bytes := make([]byte, 32)
	_, _ = rand.Read(bytes) // Ignore error for demo purposes
	return base64.URLEncoding.EncodeToString(bytes)
}

// File operations
func (am *AuthManager) loadUsers() error {
	usersFile := filepath.Join(am.configDir, "users.json")
	if _, err := os.Stat(usersFile); os.IsNotExist(err) {
		return nil // File doesn't exist, start with empty users
	}

	data, err := os.ReadFile(usersFile)
	if err != nil {
		return fmt.Errorf("failed to read users file: %w", err)
	}

	var users []*User
	if err := json.Unmarshal(data, &users); err != nil {
		return fmt.Errorf("failed to unmarshal users: %w", err)
	}

	am.users = make(map[string]*User)
	for _, user := range users {
		am.users[user.ID] = user
	}

	return nil
}

func (am *AuthManager) saveUsers() error {
	usersFile := filepath.Join(am.configDir, "users.json")

	var users []*User
	for _, user := range am.users {
		users = append(users, user)
	}

	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal users: %w", err)
	}

	return os.WriteFile(usersFile, data, 0600)
}

func (am *AuthManager) loadRoles() error {
	rolesFile := filepath.Join(am.configDir, "roles.json")
	if _, err := os.Stat(rolesFile); os.IsNotExist(err) {
		return nil // File doesn't exist, use default roles
	}

	data, err := os.ReadFile(rolesFile)
	if err != nil {
		return fmt.Errorf("failed to read roles file: %w", err)
	}

	var roles []*Role
	if err := json.Unmarshal(data, &roles); err != nil {
		return fmt.Errorf("failed to unmarshal roles: %w", err)
	}

	am.roles = make(map[string]*Role)
	for _, role := range roles {
		am.roles[role.ID] = role
	}

	return nil
}

// saveRoles saves roles to file
// func (am *AuthManager) saveRoles() error {
// 	rolesFile := filepath.Join(am.configDir, "roles.json")
//
// 	var roles []*Role
// 	for _, role := range am.roles {
// 		roles = append(roles, role)
// 	}
//
// 	data, err := json.MarshalIndent(roles, "", "  ")
// 	if err != nil {
// 		return fmt.Errorf("failed to marshal roles: %w", err)
// 	}
//
// 	return os.WriteFile(rolesFile, data, 0600)
// }

func (am *AuthManager) saveSessions() error {
	sessionsFile := filepath.Join(am.configDir, "sessions.json")

	var sessions []*Session
	for _, session := range am.sessions {
		sessions = append(sessions, session)
	}

	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal sessions: %w", err)
	}

	return os.WriteFile(sessionsFile, data, 0600)
}

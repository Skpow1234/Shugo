package httpsecure

import (
	"crypto/subtle"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

const (
	EnvTLSCert = "SHUGO_TLS_CERT"
	EnvTLSKey  = "SHUGO_TLS_KEY"
)

// AuthorizeBearer reports whether header is "Bearer " plus token.
// An empty token never matches.
func AuthorizeBearer(header, token string) bool {
	if token == "" {
		return false
	}
	const prefix = "Bearer "
	if len(header) != len(prefix)+len(token) || !strings.HasPrefix(header, prefix) {
		return false
	}
	got := header[len(prefix):]
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// TLSFiles returns certificate and key paths from the environment.
func TLSFiles() (certFile, keyFile string) {
	return os.Getenv(EnvTLSCert), os.Getenv(EnvTLSKey)
}

// ConfigureAddr returns the address to bind. Without TLS, unspecified and
// wildcard hosts are rewritten to loopback. A non-loopback host without TLS
// is rejected.
func ConfigureAddr(addr, certFile, keyFile string) (string, bool, error) {
	useTLS := certFile != "" && keyFile != ""
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", false, fmt.Errorf("listen address %q: %w", addr, err)
	}
	if useTLS {
		return addr, true, nil
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return net.JoinHostPort("127.0.0.1", port), false, nil
	}
	if !isLoopback(host) {
		return "", false, fmt.Errorf("refusing cleartext listen on %s; set %s and %s", addr, EnvTLSCert, EnvTLSKey)
	}
	return addr, false, nil
}

// ListenAndServe binds srv. Without TLS material in the environment, the
// listen address is restricted to loopback.
func ListenAndServe(srv *http.Server) error {
	certFile, keyFile := TLSFiles()
	addr, useTLS, err := ConfigureAddr(srv.Addr, certFile, keyFile)
	if err != nil {
		return err
	}
	srv.Addr = addr
	if useTLS {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		return srv.ListenAndServeTLS(certFile, keyFile)
	}
	return srv.ListenAndServe()
}

// SetCORS writes a browser CORS policy. A "*" allow-list echoes "*" and
// does not allow credentials. An explicit origin match may allow credentials.
func SetCORS(w http.ResponseWriter, r *http.Request, allowed []string) {
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type")
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}
	for _, allowedOrigin := range allowed {
		if allowedOrigin == "*" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			return
		}
	}
	for _, allowedOrigin := range allowed {
		if origin == allowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			return
		}
	}
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

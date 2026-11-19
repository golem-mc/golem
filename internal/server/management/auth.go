package management

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
)

const (
	bearerPrefix      = "Bearer "
	subprotocolPrefix = subprotocol + ","
)

// authenticate checks a handshake's key: a Bearer header first, else the key
// in Sec-WebSocket-Protocol from an allowed Origin. reason is empty when the
// request passes; subprotocol reports that the 101 must name minecraft-v1.
// vanilla: AuthenticationHandler.performSecurityChecks
func (s *Server) authenticate(r *http.Request) (subprotocol bool, reason string) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, bearerPrefix) {
		if !s.validKey(strings.TrimSpace(h[len(bearerPrefix):])) {
			return false, "Invalid API key"
		}
		return false, ""
	}
	if h := r.Header.Get("Sec-WebSocket-Protocol"); strings.HasPrefix(h, subprotocolPrefix) {
		if origin := r.Header.Get("Origin"); origin == "" || !s.origins[origin] {
			return false, "Origin Not Allowed"
		}
		if !s.validKey(strings.TrimSpace(h[len(subprotocolPrefix):])) {
			return false, "Invalid API key"
		}
		return true, ""
	}
	return false, "Missing API key"
}

// validKey compares key with the secret in constant time; an empty key never
// matches. vanilla: AuthenticationHandler.isValidApiKey
func (s *Server) validKey(key string) bool {
	return key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(s.opts.Secret)) == 1
}

// unauthorized answers a refused handshake.
// vanilla: AuthenticationHandler.sendUnauthorizedResponse
func unauthorized(w http.ResponseWriter, reason string) {
	body := `{"error":"Unauthorized","message":"` + reason + `"}`
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	h.Set("Connection", "close")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(body))
}

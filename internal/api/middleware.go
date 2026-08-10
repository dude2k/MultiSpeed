package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type contextKey string

const requestIDKey contextKey = "request-id"

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID(id) {
			var data [12]byte
			_, _ = rand.Read(data[:])
			id = hex.EncodeToString(data[:])
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}
func requestIDFrom(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}
func validRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r != '-' && r != '_' && r != '.' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("HTTP panic recovered", "request_id", requestIDFrom(r.Context()), "panic", recovered)
				writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		headers.Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &responseRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(recorder, r)
		s.logger.Info("HTTP request", "request_id", requestIDFrom(r.Context()), "method", r.Method, "path", r.URL.Path, "status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

func (s *Server) trustedHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hosts.allows(r.Host) {
			writeError(w, r, http.StatusMisdirectedRequest, "HOST_REJECTED", "The request Host is not trusted by this MultiSpeed instance.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(parsed.Host, r.Host) || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				writeError(w, r, http.StatusForbidden, "ORIGIN_REJECTED", "Cross-origin mutations are not allowed.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) contentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) && r.ContentLength != 0 {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			binaryUpload := r.Method == http.MethodPost && r.URL.Path == "/api/v1/providers/ookla/binary"
			allowed := err == nil && strings.EqualFold(mediaType, "application/json")
			if binaryUpload {
				allowed = err == nil && strings.EqualFold(mediaType, "application/octet-stream")
			}
			if !allowed {
				message := "Use Content-Type: application/json."
				if binaryUpload {
					message = "Use Content-Type: application/octet-stream for the Ookla executable."
				}
				writeError(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_CONTENT_TYPE", message)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type rateGate struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clients map[string][]time.Time
}

func newRateGate(limit int, window time.Duration) *rateGate {
	return &rateGate{limit: limit, window: window, clients: map[string][]time.Time{}}
}
func (g *rateGate) Allow(request *http.Request) bool {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	now := time.Now()
	cutoff := now.Add(-g.window)
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.clients) > 1024 {
		for key, timestamps := range g.clients {
			if len(timestamps) == 0 || !timestamps[len(timestamps)-1].After(cutoff) {
				delete(g.clients, key)
			}
		}
	}
	values := g.clients[host]
	kept := values[:0]
	for _, value := range values {
		if value.After(cutoff) {
			kept = append(kept, value)
		}
	}
	if len(kept) >= g.limit {
		g.clients[host] = kept
		return false
	}
	g.clients[host] = append(kept, now)
	return true
}

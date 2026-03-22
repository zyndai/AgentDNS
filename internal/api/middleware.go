// Package api implements the REST API gateway for Agent DNS registry nodes.
package api

import (
	"bufio"
	"crypto/subtle"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements a simple per-IP token bucket rate limiter.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    int // requests per minute
	cleanup time.Duration
}

type bucket struct {
	tokens    int
	lastReset time.Time
}

// NewRateLimiter creates a rate limiter with the given requests per minute.
func NewRateLimiter(ratePerMinute int) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		rate:    ratePerMinute,
		cleanup: 5 * time.Minute,
	}
	go rl.cleanupLoop()
	return rl
}

// Allow checks if a request from the given key is allowed.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &bucket{
			tokens:    rl.rate - 1,
			lastReset: time.Now(),
		}
		return true
	}

	// Reset if minute has passed
	if time.Since(b.lastReset) > time.Minute {
		b.tokens = rl.rate - 1
		b.lastReset = time.Now()
		return true
	}

	if b.tokens > 0 {
		b.tokens--
		return true
	}
	return false
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		for key, b := range rl.buckets {
			if time.Since(b.lastReset) > 2*time.Minute {
				delete(rl.buckets, key)
			}
		}
		rl.mu.Unlock()
	}
}

// LoggingMiddleware logs request method, path, status, and duration.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, wrapped.statusCode, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.statusCode = code
	sr.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker so WebSocket upgrades work through this middleware.
func (sr *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := sr.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
	}
	return h.Hijack()
}

// CORSMiddleware adds CORS headers.
func CORSMiddleware(origins []string) func(http.Handler) http.Handler {
	allowOrigin := "*"
	if len(origins) > 0 && origins[0] != "*" {
		allowOrigin = strings.Join(origins, ", ")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitMiddleware wraps a handler with rate limiting.
func RateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)
			if !rl.Allow(ip) {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WebhookAuthMiddleware verifies that the request contains the correct webhook secret
// in the Authorization header. Used to authenticate org website → registry calls.
func WebhookAuthMiddleware(secret string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing Authorization header"}`, http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader {
				http.Error(w, `{"error":"Authorization header must use Bearer scheme"}`, http.StatusUnauthorized)
				return
			}

			// Constant-time comparison to prevent timing attacks
			if !hmacEqual(token, secret) {
				http.Error(w, `{"error":"invalid webhook secret"}`, http.StatusUnauthorized)
				return
			}

			next(w, r)
		}
	}
}

// hmacEqual performs constant-time string comparison.
func hmacEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func extractIP(r *http.Request) string {
	// Check X-Forwarded-For first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to remote addr
	parts := strings.Split(r.RemoteAddr, ":")
	if len(parts) > 0 {
		return parts[0]
	}
	return r.RemoteAddr
}

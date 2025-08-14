package main

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter
type RateLimiter struct {
	rate       time.Duration
	capacity   int
	tokens     map[string]*TokenBucket
	mutex      sync.RWMutex
	cleanupTtl time.Duration
}

// TokenBucket represents a token bucket for a specific client
type TokenBucket struct {
	tokens     int
	lastRefill time.Time
	mutex      sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(requestsPerMinute int, burstCapacity int) *RateLimiter {
	rate := time.Minute / time.Duration(requestsPerMinute)
	return &RateLimiter{
		rate:       rate,
		capacity:   burstCapacity,
		tokens:     make(map[string]*TokenBucket),
		cleanupTtl: 10 * time.Minute, // Clean up unused buckets after 10 minutes
	}
}

// Allow checks if a request from the given IP should be allowed
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mutex.RLock()
	bucket, exists := rl.tokens[ip]
	rl.mutex.RUnlock()

	if !exists {
		bucket = &TokenBucket{
			tokens:     rl.capacity,
			lastRefill: time.Now(),
		}
		rl.mutex.Lock()
		rl.tokens[ip] = bucket
		rl.mutex.Unlock()
	}

	return bucket.takeToken(rl.rate, rl.capacity)
}

// takeToken attempts to take a token from the bucket
func (tb *TokenBucket) takeToken(refillRate time.Duration, capacity int) bool {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)

	// Add tokens based on elapsed time
	tokensToAdd := int(elapsed / refillRate)
	if tokensToAdd > 0 {
		tb.tokens += tokensToAdd
		if tb.tokens > capacity {
			tb.tokens = capacity
		}
		tb.lastRefill = now
	}

	// Try to take a token
	if tb.tokens > 0 {
		tb.tokens--
		return true
	}

	return false
}

// Cleanup removes old token buckets to prevent memory leaks
func (rl *RateLimiter) Cleanup() {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	for ip, bucket := range rl.tokens {
		bucket.mutex.Lock()
		if now.Sub(bucket.lastRefill) > rl.cleanupTtl {
			delete(rl.tokens, ip)
		}
		bucket.mutex.Unlock()
	}
}

// StartCleanupRoutine starts a background routine to clean up old token buckets
func (rl *RateLimiter) StartCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			rl.cleanup()
		}
	}()
}

func (rl *RateLimiter) cleanup() {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	for ip, bucket := range rl.tokens {
		bucket.mutex.Lock()
		lastActivity := bucket.lastRefill
		bucket.mutex.Unlock()

		if now.Sub(lastActivity) > rl.cleanupTtl {
			delete(rl.tokens, ip)
		}
	}
}

// RateLimitMiddleware creates HTTP middleware for rate limiting
func (app *App) RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract IP address
			ip := getRealIP(r)
			
			if !limiter.Allow(ip) {
				AppLogger.WithFields(map[string]interface{}{
					"ip":     ip,
					"method": r.Method,
					"path":   r.URL.Path,
				}).Warn("Rate limit exceeded")
				
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error": "Rate limit exceeded. Please try again later."}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getRealIP extracts the real IP address from the request
func getRealIP(r *http.Request) string {
	// Check X-Real-IP header (nginx)
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}

	// Check X-Forwarded-For header (load balancers, proxies)
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		if ip := parseForwardedFor(forwarded); ip != "" {
			return ip
		}
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func parseForwardedFor(forwarded string) string {
	// Split by comma and take the first IP
	ips := []rune(forwarded)
	var result []rune
	
	for _, char := range ips {
		if char == ',' {
			break
		}
		if char != ' ' {
			result = append(result, char)
		}
	}
	
	return string(result)
}

// SpecialRateLimitMiddleware provides different rate limits for different endpoints
func (app *App) SpecialRateLimitMiddleware() func(http.Handler) http.Handler {
	// Different rate limiters for different types of requests
	authLimiter := NewRateLimiter(5, 10)      // 5 requests per minute for auth endpoints
	apiLimiter := NewRateLimiter(60, 120)     // 60 requests per minute for API endpoints
	generalLimiter := NewRateLimiter(30, 60)  // 30 requests per minute for general endpoints

	// Start cleanup routines
	authLimiter.StartCleanupRoutine()
	apiLimiter.StartCleanupRoutine()
	generalLimiter.StartCleanupRoutine()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var limiter *RateLimiter
			
			// Choose limiter based on path
			switch {
			case r.URL.Path == "/login" || r.URL.Path == "/auth/callback":
				limiter = authLimiter
			case r.URL.Path == "/api/corrections" || r.URL.Path == "/api/add-row" || r.URL.Path == "/api/delete-row":
				limiter = apiLimiter
			default:
				limiter = generalLimiter
			}

			// Extract IP address
			ip := getRealIP(r)
			
			if !limiter.Allow(ip) {
				AppLogger.WithFields(map[string]interface{}{
					"ip":       ip,
					"method":   r.Method,
					"path":     r.URL.Path,
					"category": getLimiterCategory(r.URL.Path),
				}).Warn("Rate limit exceeded")
				
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error": "Rate limit exceeded. Please try again later."}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func getLimiterCategory(path string) string {
	switch {
	case path == "/login" || path == "/auth/callback":
		return "auth"
	case path == "/api/corrections" || path == "/api/add-row" || path == "/api/delete-row":
		return "api"
	default:
		return "general"
	}
}
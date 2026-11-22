package main

import (
	"net/http"
	"sync"
)

// SSEConnectionLimiter bounds the number of active SSE requests.
type SSEConnectionLimiter struct {
	slots chan struct{}
}

func NewSSEConnectionLimiter(maxConnections int) *SSEConnectionLimiter {
	if maxConnections <= 0 {
		panic("SSE connection limit must be positive")
	}

	return &SSEConnectionLimiter{
		slots: make(chan struct{}, maxConnections),
	}
}

func (l *SSEConnectionLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case l.slots <- struct{}{}:
			// Release when the SSE handler exits, including on
			// disconnect or after delivering an order.
			defer func() {
				<-l.slots
			}()

			next.ServeHTTP(w, r)

		default:
			w.Header().Set("Retry-After", "5")
			http.Error(
				w,
				"Too many active SSE connections",
				http.StatusServiceUnavailable,
			)
		}
	})
}

type SSEPerIPLimiter struct {
	mu            sync.Mutex
	active        map[string]int
	maxPerIP      int
	trustFlyProxy bool
}

func NewSSEPerIPLimiter(
	maxPerIP int,
	trustFlyProxy bool,
) *SSEPerIPLimiter {
	if maxPerIP <= 0 {
		panic("per-IP SSE limit must be positive")
	}

	return &SSEPerIPLimiter{
		active:        make(map[string]int),
		maxPerIP:      maxPerIP,
		trustFlyProxy: trustFlyProxy,
	}
}

func (l *SSEPerIPLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, err := resolveClientIP(r, l.trustFlyProxy)
		if err != nil {
			http.Error(w, "invalid client IP", http.StatusBadRequest)
			return
		}

		l.mu.Lock()

		if l.active[ip] >= l.maxPerIP {
			l.mu.Unlock()

			w.Header().Set("Retry-After", "5")
			http.Error(
				w,
				"Too many active SSE connections from this client",
				http.StatusTooManyRequests,
			)
			return
		}

		l.active[ip]++
		l.mu.Unlock()

		defer func() {
			l.mu.Lock()
			l.active[ip]--

			if l.active[ip] == 0 {
				delete(l.active, ip)
			}

			l.mu.Unlock()
		}()

		next.ServeHTTP(w, r)
	})
}

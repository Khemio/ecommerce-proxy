package main

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type clientBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type IPRateLimiter struct {
	mu            sync.Mutex
	clients       map[string]*clientBucket
	interval      time.Duration
	burst         int
	maxClients    int
	lastCleanup   time.Time
	trustFlyProxy bool
}

func NewIPRateLimiter(interval time.Duration, burst, maxClients int) *IPRateLimiter {
	return &IPRateLimiter{
		clients:    make(map[string]*clientBucket),
		interval:   interval,
		burst:      burst,
		maxClients: maxClients,
	}
}

func (l *IPRateLimiter) allow(ip string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Periodically reclaim inactive clients.
	if now.Sub(l.lastCleanup) >= time.Minute {
		for ip, entry := range l.clients {
			if now.Sub(entry.lastSeen) > 30*time.Minute {
				delete(l.clients, ip)
			}
		}
		l.lastCleanup = now
	}

	entry, exists := l.clients[ip]
	if !exists {
		// Bound the memory consumed by client entries.
		if len(l.clients) >= l.maxClients {
			return false
		}

		entry = &clientBucket{
			limiter: rate.NewLimiter(
				rate.Every(l.interval),
				l.burst,
			),
		}
		l.clients[ip] = entry
	}

	entry.lastSeen = now
	return entry.limiter.Allow()
}

func (l *IPRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ip, _, err := net.SplitHostPort(r.RemoteAddr)
		// if err != nil {
		// 	http.Error(w, "invalid remote address", http.StatusBadRequest)
		// 	return
		// }

		ip, err := resolveClientIP(r, l.trustFlyProxy)
		if err != nil {
			http.Error(w, "invalid client address", http.StatusBadRequest)
			return
		}

		if !l.allow(ip) {
			retrySeconds := int(
				(l.interval + time.Second - 1) / time.Second,
			)

			w.Header().Set(
				"Retry-After",
				strconv.Itoa(retrySeconds),
			)
			http.Error(
				w,
				"Too many requests",
				http.StatusTooManyRequests,
			)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func resolveClientIP(r *http.Request, trustFlyProxy bool) (string, error) {
	var rawIP string

	if trustFlyProxy {
		rawIP = r.Header.Get("Fly-Client-IP")
	} else {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return "", fmt.Errorf("invalid remote address: %w", err)
		}
		rawIP = host
	}

	ip, err := netip.ParseAddr(strings.TrimSpace(rawIP))
	if err != nil {
		return "", fmt.Errorf("invalid client IP: %w", err)
	}

	return ip.Unmap().String(), nil
}

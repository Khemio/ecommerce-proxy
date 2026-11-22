package main

import (
	"context"
	"time"
)

const (
	sessionCleanupInterval = 10 * time.Minute
	sessionTTL             = 2 * time.Hour
)

func (s *MarketplaceServer) StartSessionCleanup(
	ctx context.Context,
) {
	ticker := time.NewTicker(sessionCleanupInterval)

	// go func() {
	defer ticker.Stop()

	for {
		select {

		case <-ticker.C:
			s.cleanupSessions()

		case <-ctx.Done():
			s.logger.Println("Session cleanup stopped.")
			return
		}
	}
	// }()
}

func (s *MarketplaceServer) cleanupSessions() {
	cutoff := time.Now().Add(-sessionTTL)

	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	for sessionID, session := range s.sessions {

		if session.CreatedAt.Before(cutoff) {

			delete(
				s.sessions,
				sessionID,
			)

			s.logger.Printf(
				"removed expired session: %s",
				sessionID,
			)
		}
	}
}

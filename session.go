package main

func (s *MarketplaceServer) getSession(sessionID string) (*Session, bool) {

	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()

	session, exists := s.sessions[sessionID]

	return session, exists
}

func (s *MarketplaceServer) saveSession(session *Session) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	if old, exists := s.sessions[session.ID]; exists {
		close(old.Events)
	}

	s.sessions[session.ID] = session
}

func (s *MarketplaceServer) deleteSession(sessionID string) {
	s.sessionsMu.Lock()

	session, exists := s.sessions[sessionID]

	if exists {
		delete(s.sessions, sessionID)
	}

	s.sessionsMu.Unlock()

	if exists {
		// Don't close Events here if there is any chance
		// another goroutine will send to it.
		//
		// For this simple one-event lifecycle, leaving the
		// channel for GC is safer than creating a
		// send-on-closed-channel race.
		_ = session
	}
}

// func (s *MarketplaceServer) deleteSession(
// 	sessionID string,
// ) {
// 	s.sessionsMu.Lock()
// 	defer s.sessionsMu.Unlock()

// 	delete(s.sessions, sessionID)
// }

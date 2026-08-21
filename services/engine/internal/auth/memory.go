package auth

import (
	"context"
	"sync"
	"time"
)

type MemoryStore struct {
	mu         sync.Mutex
	challenges map[string]Challenge
	sessions   map[string]Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{challenges: map[string]Challenge{}, sessions: map[string]Session{}}
}
func (s *MemoryStore) SaveChallenge(_ context.Context, c Challenge) (Challenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.challenges {
		if existing.WalletAddress == c.WalletAddress && existing.Domain == c.Domain && existing.ChainID == c.ChainID && existing.Purpose == c.Purpose && existing.Version == c.Version && c.IssuedAt.Before(existing.ExpiresAt) {
			return existing, nil
		}
	}
	s.challenges[c.Nonce] = c
	return c, nil
}
func (s *MemoryStore) ConsumeChallenge(_ context.Context, c Challenge, tokenHash string, session Session) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.challenges[c.Nonce]
	if !ok {
		return Session{}, ErrNonceConsumed
	}
	if stored.Message != c.Message {
		return Session{}, ErrInvalidChallenge
	}
	delete(s.challenges, c.Nonce)
	s.sessions[tokenHash] = session
	return session, nil
}
func (s *MemoryStore) ReadSession(_ context.Context, tokenHash string, now time.Time) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[tokenHash]
	if !ok || !now.Before(session.ExpiresAt) {
		return Session{}, ErrInvalidChallenge
	}
	session.Token = ""
	return session, nil
}
func (s *MemoryStore) RevokeSession(_ context.Context, tokenHash string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[tokenHash]; !ok {
		return ErrInvalidChallenge
	}
	delete(s.sessions, tokenHash)
	return nil
}

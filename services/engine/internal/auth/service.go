package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidChallenge = errors.New("invalid authentication challenge")
	ErrInvalidSignature = errors.New("invalid wallet signature")
	ErrNonceConsumed    = errors.New("authentication nonce already consumed")
	ErrRateLimited      = errors.New("authentication challenge rate limited")
)

type Challenge struct {
	WalletAddress string    `json:"walletAddress"`
	Domain        string    `json:"domain"`
	ChainID       string    `json:"chainId"`
	Purpose       string    `json:"purpose"`
	Version       string    `json:"version"`
	Nonce         string    `json:"nonce"`
	IssuedAt      time.Time `json:"issuedAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Message       string    `json:"message"`
}

type Session struct {
	Token     string    `json:"token,omitempty"`
	SessionID string    `json:"sessionId"`
	UserID    string    `json:"userId"`
	Wallet    string    `json:"walletAddress"`
	Roles     []string  `json:"roles"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type VerifyRequest struct {
	Message   string
	Signature string
}

type Store interface {
	SaveChallenge(context.Context, Challenge) (Challenge, error)
	ConsumeChallenge(context.Context, Challenge, string, Session) (Session, error)
	ReadSession(context.Context, string, time.Time) (Session, error)
	RevokeSession(context.Context, string, time.Time) error
}

type SignatureVerifier interface {
	Verify(message, signature, expectedAddress string) error
}

type Config struct {
	Domain       string
	ChainID      string
	Purpose      string
	Version      string
	ChallengeTTL time.Duration
	SessionTTL   time.Duration
	DefaultRole  string
	Now          func() time.Time
}

type Service struct {
	store    Store
	verifier SignatureVerifier
	config   Config
}

func NewService(store Store, verifier SignatureVerifier, config Config) (*Service, error) {
	if store == nil || verifier == nil || config.Domain == "" || config.ChainID == "" || config.Purpose == "" {
		return nil, errors.New("authentication store, verifier, domain, chain, and purpose are required")
	}
	if config.ChallengeTTL <= 0 {
		config.ChallengeTTL = 5 * time.Minute
	}
	if config.SessionTTL <= 0 {
		config.SessionTTL = 24 * time.Hour
	}
	if config.DefaultRole == "" {
		config.DefaultRole = "publisher"
	}
	if config.Version == "" {
		config.Version = "1"
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{store: store, verifier: verifier, config: config}, nil
}

func (s *Service) Issue(ctx context.Context, wallet string) (Challenge, error) {
	wallet = strings.ToLower(wallet)
	if !IsWalletAddress(wallet) {
		return Challenge{}, ErrInvalidChallenge
	}
	now := s.config.Now().UTC()
	nonce, err := randomHex(24)
	if err != nil {
		return Challenge{}, fmt.Errorf("generate nonce: %w", err)
	}
	challenge := Challenge{WalletAddress: wallet, Domain: s.config.Domain, ChainID: s.config.ChainID, Purpose: s.config.Purpose, Version: s.config.Version, Nonce: nonce, IssuedAt: now, ExpiresAt: now.Add(s.config.ChallengeTTL)}
	challenge.Message = FormatMessage(challenge)
	challenge, err = s.store.SaveChallenge(ctx, challenge)
	if err != nil {
		return Challenge{}, err
	}
	return challenge, nil
}

func (s *Service) Verify(ctx context.Context, request VerifyRequest) (Session, error) {
	challenge, err := ParseMessage(request.Message)
	now := s.config.Now().UTC()
	if err != nil || challenge.Domain != s.config.Domain || challenge.ChainID != s.config.ChainID || challenge.Purpose != s.config.Purpose || challenge.Version != s.config.Version || challenge.Message != request.Message || now.Before(challenge.IssuedAt) || !now.Before(challenge.ExpiresAt) {
		return Session{}, ErrInvalidChallenge
	}
	if err := s.verifier.Verify(request.Message, request.Signature, challenge.WalletAddress); err != nil {
		return Session{}, ErrInvalidSignature
	}
	token, err := randomHex(32)
	if err != nil {
		return Session{}, fmt.Errorf("generate session: %w", err)
	}
	session := Session{Token: token, SessionID: hash("session:" + token), UserID: hash("user:" + challenge.WalletAddress), Wallet: challenge.WalletAddress, Roles: []string{s.config.DefaultRole}, ExpiresAt: s.config.Now().UTC().Add(s.config.SessionTTL)}
	return s.store.ConsumeChallenge(ctx, challenge, hash(token), session)
}

func (s *Service) Session(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrInvalidChallenge
	}
	return s.store.ReadSession(ctx, hash(token), s.config.Now().UTC())
}

func (s *Service) Revoke(ctx context.Context, token string) error {
	if token == "" {
		return ErrInvalidChallenge
	}
	return s.store.RevokeSession(ctx, hash(token), s.config.Now().UTC())
}

func FormatMessage(c Challenge) string {
	return fmt.Sprintf("%s wants you to sign in to Agent Platform.\n\nWallet: %s\nChain ID: %s\nPurpose: %s\nVersion: %s\nNonce: %s\nIssued At: %s\nExpiration Time: %s", c.Domain, strings.ToLower(c.WalletAddress), c.ChainID, c.Purpose, c.Version, c.Nonce, c.IssuedAt.UTC().Format(time.RFC3339Nano), c.ExpiresAt.UTC().Format(time.RFC3339Nano))
}

func ParseMessage(message string) (Challenge, error) {
	lines := strings.Split(message, "\n")
	if len(lines) != 9 || !strings.HasSuffix(lines[0], " wants you to sign in to Agent Platform.") || lines[1] != "" {
		return Challenge{}, ErrInvalidChallenge
	}
	c := Challenge{Domain: strings.TrimSuffix(lines[0], " wants you to sign in to Agent Platform."), WalletAddress: strings.TrimPrefix(lines[2], "Wallet: "), ChainID: strings.TrimPrefix(lines[3], "Chain ID: "), Purpose: strings.TrimPrefix(lines[4], "Purpose: "), Version: strings.TrimPrefix(lines[5], "Version: "), Nonce: strings.TrimPrefix(lines[6], "Nonce: ")}
	issuedAt, issuedErr := time.Parse(time.RFC3339Nano, strings.TrimPrefix(lines[7], "Issued At: "))
	expiresAt, expiresErr := time.Parse(time.RFC3339Nano, strings.TrimPrefix(lines[8], "Expiration Time: "))
	if issuedErr != nil || expiresErr != nil || !IsWalletAddress(c.WalletAddress) || c.Nonce == "" {
		return Challenge{}, ErrInvalidChallenge
	}
	c.IssuedAt, c.ExpiresAt = issuedAt, expiresAt
	c.WalletAddress = strings.ToLower(c.WalletAddress)
	c.Message = FormatMessage(c)
	return c, nil
}

func IsWalletAddress(address string) bool {
	if len(address) != 42 || !strings.HasPrefix(address, "0x") {
		return false
	}
	_, err := hex.DecodeString(address[2:])
	return err == nil
}

func randomHex(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

package chatsecurity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const serviceKeysURL = "https://api.minecraftservices.com/publickeys"

// KeyProvider supplies immutable PROFILE_KEY trust roots. Fetching must happen
// outside tick and connection-state locks; a failed refresh can return old keys.
type KeyProvider interface {
	Keys(context.Context) (*KeySet, error)
}

// ServiceKeys caches Mojang's service keys and coalesces concurrent fetches.
// The owning server's shutdown context cancels requests, including requests
// whose original caller has already stopped waiting.
type ServiceKeys struct {
	ctx      context.Context
	client   *http.Client
	now      func() time.Time
	mu       sync.Mutex
	keys     *KeySet
	err      error
	next     time.Time
	failures int
	fetching chan struct{}
}

// NewServiceKeys binds the cache to the server lifetime. A nil client uses the
// default transport; tests inject an HTTP client, never a different trust URL.
func NewServiceKeys(shutdown context.Context, client *http.Client) *ServiceKeys {
	if client == nil {
		client = &http.Client{}
	}
	// Do not follow redirects to another trust endpoint or downgrade to HTTP.
	copied := *client
	copied.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &ServiceKeys{ctx: shutdown, client: &copied, now: time.Now}
}

// Keys fetches on first use and after the refresh/retry deadline. Cancellation
// of one waiter does not cancel the shared request for other connections.
func (s *ServiceKeys) Keys(ctx context.Context) (*KeySet, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		s.mu.Lock()
		if s.fetching == nil && s.now().Before(s.next) {
			keys, err := s.keys, s.err
			s.mu.Unlock()
			return keys, err
		}
		if s.fetching == nil {
			s.fetching = make(chan struct{})
			go s.refresh(s.fetching)
		}
		done := s.fetching
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		case <-done:
		}
	}
}

func (s *ServiceKeys) refresh(done chan struct{}) {
	keys, err := s.fetch()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.keys = keys
		s.failures = 0
		s.next = s.now().Add(24 * time.Hour)
	} else {
		// Authlib YggdrasilServicesKeyInfo$1: the first failure retries in five
		// minutes; each subsequent failure doubles it, capped at 320 minutes.
		s.next = s.now().Add(5 * time.Minute * time.Duration(1<<min(s.failures, 6)))
		s.failures = min(s.failures+1, 6)
	}
	s.err = err
	s.fetching = nil
	close(done)
}

func (s *ServiceKeys) fetch() (*KeySet, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceKeysURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("service keys: HTTP %d", resp.StatusCode)
	}
	const limit = 1 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("service keys exceed %d bytes", limit)
	}
	var response struct {
		PlayerCertificateKeys []struct {
			PublicKey []byte `json:"publicKey"`
		} `json:"playerCertificateKeys"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	encoded := make([][]byte, 0, len(response.PlayerCertificateKeys))
	for _, key := range response.PlayerCertificateKeys {
		encoded = append(encoded, key.PublicKey)
	}
	if len(encoded) == 0 {
		return nil, fmt.Errorf("service keys: no player certificate keys")
	}
	return ParseKeys(encoded...)
}

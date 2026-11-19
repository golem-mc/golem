// Package chatsecurity implements secure-chat verification without packet routing.
package chatsecurity

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Failure is a vanilla translation key; callers choose disconnect or red chat.
type Failure string

func (f Failure) Error() string { return string(f) }

const (
	InvalidPublicKey  Failure = "multiplayer.disconnect.invalid_public_key_signature"
	OlderPublicKey    Failure = "multiplayer.disconnect.expired_public_key"
	MissingProfileKey Failure = "chat.disabled.missingProfileKey"
	ExpiredProfileKey Failure = "chat.disabled.expiredProfileKey"
	ChainBroken       Failure = "chat.disabled.chain_broken"
	InvalidSignature  Failure = "chat.disabled.invalid_signature"
	OutOfOrderChat    Failure = "chat.disabled.out_of_order_chat"
)

// SessionData is untrusted wire data. Validation takes its own immutable copy.
// vanilla: RemoteChatSession.Data, ProfilePublicKey.Data
type SessionData struct {
	ID                     uuid.UUID
	ExpiresAt              int64 // epoch milliseconds
	PublicKey, Certificate []byte
}

// Session is an immutable, certificate-validated remote chat session.
type Session struct {
	profile uuid.UUID
	data    SessionData
	key     *rsa.PublicKey
}

// Data returns a copy suitable for INITIALIZE_CHAT encoding.
func (s *Session) Data() SessionData {
	d := s.data
	d.PublicKey = slices.Clone(d.PublicKey)
	d.Certificate = slices.Clone(d.Certificate)
	return d
}

// KeySet holds immutable PROFILE_KEY service keys, never profile-property keys.
type KeySet struct{ keys []*rsa.PublicKey }

// ParseKeys decodes service keys as DER SubjectPublicKeyInfo, taking ownership
// of no caller buffers. An empty set cannot validate certificates.
func ParseKeys(encoded ...[]byte) (*KeySet, error) {
	set := &KeySet{}
	for _, der := range encoded {
		key, err := parseRSA(der)
		if err != nil {
			return nil, err
		}
		set.keys = append(set.keys, key)
	}
	return set, nil
}

func parseRSA(der []byte) (*rsa.PublicKey, error) {
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("chat key is not RSA")
	}
	return rsaKey, nil
}

// ValidateSession applies renewal ordering and certificate verification. A nil
// result with nil error means no trusted keys; retain the installed session.
// The same key data is a no-op even with a different session UUID.
// vanilla: ServerGamePacketListenerImpl.handleChatSessionUpdate
func ValidateSession(profile uuid.UUID, data SessionData, old *Session, trusted *KeySet) (*Session, error) {
	if len(data.PublicKey) > 512 || len(data.Certificate) > 4096 {
		return nil, InvalidPublicKey
	}
	key, err := parseRSA(data.PublicKey)
	if err != nil {
		return nil, InvalidPublicKey
	}
	if old != nil {
		if old.data.ExpiresAt == data.ExpiresAt && bytes.Equal(old.data.PublicKey, data.PublicKey) && bytes.Equal(old.data.Certificate, data.Certificate) {
			return old, nil
		}
		if data.ExpiresAt < old.data.ExpiresAt {
			return nil, OlderPublicKey
		}
	}
	if trusted == nil || len(trusted.keys) == 0 {
		return nil, nil
	}
	payload := append([]byte{}, profile[:]...)
	payload = binary.BigEndian.AppendUint64(payload, uint64(data.ExpiresAt))
	payload = append(payload, data.PublicKey...)
	// Authlib YggdrasilServicesKeyInfo.signature uses SHA1withRSA, whereas
	// ProfilePublicKey.createSignatureValidator uses SHA256withRSA for chat.
	digest := sha1.Sum(payload)
	for _, serviceKey := range trusted.keys {
		if rsa.VerifyPKCS1v15(serviceKey, crypto.SHA1, digest[:], data.Certificate) == nil {
			data.PublicKey = slices.Clone(data.PublicKey)
			data.Certificate = slices.Clone(data.Certificate)
			return &Session{profile: profile, data: data, key: key}, nil
		}
	}
	return nil, InvalidPublicKey
}

func (s *Session) expired(now time.Time) bool {
	return time.UnixMilli(s.data.ExpiresAt).Before(now)
}

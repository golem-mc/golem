package chatsecurity

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func fixture(t *testing.T, profiles ...uuid.UUID) (*rsa.PrivateKey, SessionData, uuid.UUID, *KeySet) {
	t.Helper()
	service, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	player, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&player.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	serviceDER, err := x509.MarshalPKIXPublicKey(&service.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := ParseKeys(serviceDER)
	if err != nil {
		t.Fatal(err)
	}
	profile := uuid.New()
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	data := SessionData{ID: uuid.New(), ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), PublicKey: der}
	payload := append([]byte{}, profile[:]...)
	payload = binary.BigEndian.AppendUint64(payload, uint64(data.ExpiresAt))
	payload = append(payload, der...)
	digest := sha1.Sum(payload)
	data.Certificate, err = rsa.SignPKCS1v15(rand.Reader, service, crypto.SHA1, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return player, data, profile, trust
}

func sign(t *testing.T, key *rsa.PrivateKey, profile, session uuid.UUID, index int32, body Body) *Signature {
	t.Helper()
	digest := sha256.Sum256(SigningBytes(profile, session, index, body))
	raw, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return (*Signature)(raw)
}

func TestSessionCertificatesAndRenewal(t *testing.T) {
	_, data, profile, trust := fixture(t)
	session, err := ValidateSession(profile, data, nil, trust)
	if err != nil || session == nil {
		t.Fatalf("validate: %v", err)
	}
	otherID := data
	otherID.ID = uuid.New()
	if got, err := ValidateSession(profile, otherID, session, nil); got != session || err != nil {
		t.Fatalf("same key no-op: %v", err)
	}
	older := data
	older.ExpiresAt--
	if _, err := ValidateSession(profile, older, session, trust); err != OlderPublicKey {
		t.Fatalf("older: %v", err)
	}
	newer := data
	newer.ExpiresAt++
	if _, err := ValidateSession(profile, newer, nil, trust); err != InvalidPublicKey {
		t.Fatalf("expiry certificate binding: %v", err)
	}
	if got, err := ValidateSession(profile, data, nil, nil); got != nil || err != nil {
		t.Fatalf("absent trust: %v", err)
	}
	for name, bad := range map[string]SessionData{
		"DER":               {ID: data.ID, ExpiresAt: data.ExpiresAt, PublicKey: []byte("bad"), Certificate: data.Certificate},
		"key limit":         {PublicKey: make([]byte, 513)},
		"certificate limit": {PublicKey: data.PublicKey, Certificate: make([]byte, 4097)},
		"certificate":       {PublicKey: data.PublicKey, ExpiresAt: data.ExpiresAt, Certificate: make([]byte, 256)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateSession(profile, bad, nil, trust); err != InvalidPublicKey {
				t.Fatalf("accepted bad data: %v", err)
			}
		})
	}
	if _, err := ValidateSession(uuid.New(), data, nil, trust); err != InvalidPublicKey {
		t.Fatalf("profile binding: %v", err)
	}
	// A certificate is valid independently of current expiry; only decoding
	// checks wall time, without the client's eight-hour grace.
	data.PublicKey[0] ^= 1
	data.Certificate[0] ^= 1
	exported := session.Data()
	exported.PublicKey[0] ^= 1
	exported.Certificate[0] ^= 1
	if _, err := ValidateSession(profile, session.Data(), nil, trust); err != nil {
		t.Fatalf("mutable session: %v", err)
	}
	_, replacement, _, replacementTrust := fixture(t, profile)
	renewed, err := ValidateSession(profile, replacement, session, replacementTrust)
	if err != nil || renewed == nil || renewed == session {
		t.Fatalf("replacement: %v", err)
	}
	combined := &KeySet{keys: append(trust.keys, replacementTrust.keys...)}
	if _, err := ValidateSession(profile, replacement, session, combined); err != nil {
		t.Fatalf("multiple roots: %v", err)
	}
	if _, err := ValidateSession(profile, replacement, session, trust); err != InvalidPublicKey {
		t.Fatalf("untrusted replacement: %v", err)
	}
}

func TestDecoderChain(t *testing.T) {
	key, data, profile, trust := fixture(t)
	session, err := ValidateSession(profile, data, nil, trust)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	body := NewBody("héllo  世界", now.UnixMilli(), 87, []Signature{{0: 255}})
	signature := sign(t, key, profile, data.ID, 0, body)
	decoder := NewDecoder(profile, session, false)
	message, err := decoder.Decode(signature, body, now)
	if err != nil {
		t.Fatal(err)
	}
	if message.Index() != 0 || message.Sender() != profile || message.SessionID() != data.ID || message.Body().Content() != body.Content() || !slices.Equal(message.Body().LastSeen(), body.LastSeen()) {
		t.Fatal("message changed")
	}
	signature[0] ^= 1
	copySignature := message.Signature()
	copySignature[0] ^= 1
	seen := message.Body().LastSeen()
	seen[0][0] = 0
	if message.Signature()[0] != signature[0]^1 || message.Body().LastSeen()[0][0] != 255 {
		t.Fatal("message mutated")
	}
	second := sign(t, key, profile, data.ID, 1, body)
	if _, err := decoder.Decode(second, body, now); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.Decode(second, body, now); err != InvalidSignature {
		t.Fatalf("replay: %v", err)
	}
	if _, err := decoder.Decode(second, body, now); err != ChainBroken {
		t.Fatalf("broken: %v", err)
	}
	for _, kind := range []string{"signature", "content", "salt", "lastSeen", "timestamp"} {
		t.Run(kind, func(t *testing.T) {
			decoder := NewDecoder(profile, session, true)
			modified := body
			sig := sign(t, key, profile, data.ID, 0, body)
			switch kind {
			case "signature":
				sig[0] ^= 1
			case "content":
				modified.content += "!"
			case "salt":
				modified.salt++
			case "lastSeen":
				modified.lastSeen = nil
			case "timestamp":
				modified.timestamp += 1000
			}
			if _, err := decoder.Decode(sig, modified, now); err != InvalidSignature {
				t.Fatalf("tampered %s: %v", kind, err)
			}
		})
	}
	decoder = NewDecoder(profile, session, true)
	if _, err := decoder.Decode(nil, body, now); err != MissingProfileKey {
		t.Fatalf("missing: %v", err)
	}
	if _, err := decoder.Decode(second, body, time.UnixMilli(data.ExpiresAt).Add(time.Nanosecond)); err != ExpiredProfileKey {
		t.Fatalf("expiry: %v", err)
	}
	sig := sign(t, key, profile, data.ID, 0, body)
	if _, err := decoder.Decode(sig, body, now); err != nil {
		t.Fatalf("missing/expired broke chain: %v", err)
	}
	earlier := NewBody(body.content, body.timestamp-1, body.salt, body.lastSeen)
	if _, err := decoder.Decode(second, earlier, now); err != OutOfOrderChat {
		t.Fatalf("order: %v", err)
	}
	if _, err := decoder.Decode(second, body, now); err != ChainBroken {
		t.Fatalf("order did not break: %v", err)
	}
	decoder = NewDecoder(profile, session, true)
	decoder.index = math.MaxInt32
	sig = sign(t, key, profile, data.ID, math.MaxInt32, body)
	if _, err := decoder.Decode(sig, body, now); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.Decode(sig, body, now); err != ChainBroken {
		t.Fatalf("exhaustion: %v", err)
	}
	old := NewBody("old", now.Add(-6*time.Minute).UnixMilli(), 1, nil)
	sig = sign(t, key, profile, data.ID, 0, old)
	message, err = NewDecoder(profile, session, true).Decode(sig, old, now)
	if err != nil || !message.ExpiredServer(now) {
		t.Fatalf("old message must warn only: %v", err)
	}
}

func TestUnsignedDecoder(t *testing.T) {
	profile := uuid.New()
	now := time.Now()
	body := NewBody("unsigned", 123, 789, []Signature{{}})
	if _, err := NewDecoder(profile, nil, true).Decode(nil, body, now); err != MissingProfileKey {
		t.Fatalf("enforcement: %v", err)
	}
	message, err := NewDecoder(profile, nil, false).Decode(&Signature{}, body, now)
	if err != nil || message.Signature() != nil || message.Index() != 0 || message.Body().Timestamp() != now.UnixMilli() || message.Body().Salt() != 0 || len(message.Body().LastSeen()) != 0 {
		t.Fatalf("unsigned fallback: %+v %v", message, err)
	}
}

func TestLastSeenWindow(t *testing.T) {
	var v Validator
	var expected []Signature
	for i := range 45 {
		sig := Signature{0: byte(i), 1: 255}
		if err := v.AddPending(sig); err != nil {
			t.Fatal(err)
		}
		if err := v.AddPending(sig); err != nil {
			t.Fatal(err)
		}
		if v.TrackedCount() != 21 {
			t.Fatalf("duplicate tracked: %d", v.TrackedCount())
		}
		expected = append(expected, sig)
		if len(expected) > WindowSize {
			expected = expected[1:]
		}
		// Standalone offset and inline offset have identical window movement.
		offset := 1
		if i%2 == 0 {
			if err := v.ApplyOffset(1); err != nil {
				t.Fatal(err)
			}
			offset = 0
		}
		bits := uint32((1<<len(expected))-1) << (WindowSize - len(expected))
		got, err := v.ApplyUpdate(offset, bits, Checksum(expected))
		if err != nil || !slices.Equal(got, expected) {
			t.Fatalf("update %d: %v", i, err)
		}
	}
}

func TestJavaSignedFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/signed.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Service, Player, Certificate, Payload, Signature []byte
		Checksum                                         byte
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	trust, err := ParseKeys(f.Service)
	if err != nil {
		t.Fatal(err)
	}
	profile := uuid.MustParse("00112233-4455-6677-8899-aabbccddeeff")
	id := uuid.MustParse("ffeeddcc-bbaa-9988-7766-554433221100")
	const timestamp = int64(1900000000123)
	session, err := ValidateSession(profile, SessionData{id, timestamp + 3600000, f.Player, f.Certificate}, nil, trust)
	if err != nil {
		t.Fatal(err)
	}
	body := NewBody("héllo  世界", timestamp, -123456789, []Signature{{0: 255}})
	if !slices.Equal(SigningBytes(profile, id, 0, body), f.Payload) {
		t.Fatal("signing payload differs from Java")
	}
	if _, err := NewDecoder(profile, session, true).Decode((*Signature)(f.Signature), body, time.UnixMilli(timestamp)); err != nil {
		t.Fatalf("Java signature: %v", err)
	}
	if Checksum(body.LastSeen()) != f.Checksum {
		t.Fatal("Java checksum differs")
	}
	negative := SigningBytes(profile, id, 0, NewBody("", -1, 0, nil))
	if int64(binary.BigEndian.Uint64(negative[48:56])) != -1 {
		t.Fatal("epoch seconds must floor")
	}
}

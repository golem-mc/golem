package chatsecurity

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Signature is the fixed-size RSA signature used by signed chat.
type Signature [256]byte

// Body holds the exact signed input, including wire timestamp milliseconds.
// NewBody copies lastSeen; accessors never expose its storage.
type Body struct {
	content         string
	timestamp, salt int64
	lastSeen        []Signature
}

// NewBody constructs an immutable signed body.
func NewBody(content string, timestamp, salt int64, lastSeen []Signature) Body {
	return Body{content, timestamp, salt, slices.Clone(lastSeen)}
}

func (b Body) Content() string       { return b.content }
func (b Body) Timestamp() int64      { return b.timestamp }
func (b Body) Salt() int64           { return b.salt }
func (b Body) LastSeen() []Signature { return slices.Clone(b.lastSeen) }

// Message holds a validated link and body, or an unsigned fallback message.
type Message struct {
	sender, session uuid.UUID
	index           int32
	signature       *Signature
	body            Body
}

func (m Message) Sender() uuid.UUID    { return m.sender }
func (m Message) SessionID() uuid.UUID { return m.session }
func (m Message) Index() int32         { return m.index }
func (m Message) Body() Body           { return m.body }

// Signature returns a copy; nil denotes an unsigned message.
func (m Message) Signature() *Signature {
	if m.signature == nil {
		return nil
	}
	s := *m.signature
	return &s
}

// ExpiredServer reports vanilla's warning threshold, never a decode refusal.
func (m Message) ExpiredServer(now time.Time) bool {
	return now.After(time.UnixMilli(m.body.timestamp).Add(5 * time.Minute))
}

// SigningBytes encodes PlayerChatMessage.updateSignature. Signed timestamps
// use epoch seconds (flooring before 1970); packet timestamps use milliseconds.
func SigningBytes(sender, session uuid.UUID, index int32, body Body) []byte {
	b := binary.BigEndian.AppendUint32(nil, 1)
	b = append(b, sender[:]...)
	b = append(b, session[:]...)
	b = binary.BigEndian.AppendUint32(b, uint32(index))
	b = binary.BigEndian.AppendUint64(b, uint64(body.salt))
	b = binary.BigEndian.AppendUint64(b, uint64(time.UnixMilli(body.timestamp).Unix()))
	b = binary.BigEndian.AppendUint32(b, uint32(len(body.content)))
	b = append(b, body.content...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(body.lastSeen)))
	for _, signature := range body.lastSeen {
		b = append(b, signature[:]...)
	}
	return b
}

// Decoder owns a sender's sequence shared by typed chat and command arguments.
// Callers serialize Decode and BreakChain; replacing a session creates a new
// decoder without resetting recipients' acknowledgement windows.
type Decoder struct {
	profile       uuid.UUID
	session       *Session
	enforce       bool
	index         int32
	broken        bool
	lastTimestamp int64 // vanilla starts at Instant.EPOCH
}

// NewDecoder also implements vanilla's unsigned fallback when session is nil.
func NewDecoder(profile uuid.UUID, session *Session, enforce bool) *Decoder {
	// A validated key is bound to its authenticated UUID, including when a
	// command changes the displayed executor.
	if session != nil {
		profile = session.profile
	}
	return &Decoder{profile: profile, session: session, enforce: enforce}
}

// BreakChain refuses further signed messages until a validated replacement.
func (d *Decoder) BreakChain() { d.broken = true }

// Decode verifies one link. Missing signatures and expired keys do not break
// the chain; timestamp regression and invalid signatures do.
// vanilla: SignedMessageChain.decoder
func (d *Decoder) Decode(signature *Signature, body Body, now time.Time) (Message, error) {
	if d.session == nil {
		if d.enforce {
			return Message{}, MissingProfileKey
		}
		return Message{sender: d.profile, body: NewBody(body.content, now.UnixMilli(), 0, nil)}, nil
	}
	if signature == nil {
		return Message{}, MissingProfileKey
	}
	if d.session.expired(now) {
		return Message{}, ExpiredProfileKey
	}
	if d.broken {
		return Message{}, ChainBroken
	}
	if body.timestamp < d.lastTimestamp {
		d.BreakChain()
		return Message{}, OutOfOrderChat
	}
	d.lastTimestamp = body.timestamp
	digest := sha256.Sum256(SigningBytes(d.profile, d.session.data.ID, d.index, body))
	if rsa.VerifyPKCS1v15(d.session.key, crypto.SHA256, digest[:], signature[:]) != nil {
		d.BreakChain()
		return Message{}, InvalidSignature
	}
	copied := *signature
	message := Message{d.profile, d.session.data.ID, d.index, &copied, body}
	if d.index == math.MaxInt32 {
		d.BreakChain()
	} else {
		d.index++
	}
	return message, nil
}

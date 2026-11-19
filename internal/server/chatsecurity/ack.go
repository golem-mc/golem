package chatsecurity

import "errors"

// WindowSize is vanilla's fixed last-seen window, initially twenty empty slots.
const WindowSize = 20

// ErrInvalidAck identifies an invalid offset, bitset, transition or checksum.
var ErrInvalidAck = errors.New("invalid chat acknowledgement")

// ErrTooManyPending identifies vanilla's 4096 tracked-slot limit.
var ErrTooManyPending = errors.New("too many pending chats")

type trackedEntry struct {
	signature        Signature
	pending, present bool
}

// Validator reconstructs a recipient's last-seen list. The recipient's chat
// lock must cover packet queueing, AddPending, ApplyOffset and ApplyUpdate.
type Validator struct {
	tracked     []trackedEntry
	lastPending *Signature
}

func (v *Validator) init() {
	if v.tracked == nil {
		v.tracked = make([]trackedEntry, WindowSize)
	}
}

// AddPending tracks only signatures actually sent and suppresses consecutive
// duplicate signatures (including repeated whisper echoes).
// vanilla: LastSeenMessagesValidator.addPending
func (v *Validator) AddPending(signature Signature) error {
	v.init()
	if v.lastPending == nil || *v.lastPending != signature {
		v.tracked = append(v.tracked, trackedEntry{signature, true, true})
		v.lastPending = &signature
	}
	if len(v.tracked) > 4096 {
		return ErrTooManyPending
	}
	return nil
}

// TrackedCount includes the twenty window slots.
func (v *Validator) TrackedCount() int { v.init(); return len(v.tracked) }

// ApplyOffset applies a standalone acknowledgement without changing slot flags.
func (v *Validator) ApplyOffset(offset int) error {
	v.init()
	if offset < 0 || offset > len(v.tracked)-WindowSize {
		return ErrInvalidAck
	}
	copy(v.tracked, v.tracked[offset:])
	clear(v.tracked[len(v.tracked)-offset:])
	v.tracked = v.tracked[:len(v.tracked)-offset]
	return nil
}

// ApplyUpdate advances the window, reconstructs signatures in slot order and
// checks Java's signed-byte checksum. After failure the caller must disconnect.
// vanilla: LastSeenMessagesValidator.applyUpdate
func (v *Validator) ApplyUpdate(offset int, acknowledged uint32, checksum byte) ([]Signature, error) {
	if err := v.ApplyOffset(offset); err != nil {
		return nil, err
	}
	if acknowledged>>WindowSize != 0 {
		return nil, ErrInvalidAck
	}
	lastSeen := make([]Signature, 0, WindowSize)
	for i := range WindowSize {
		entry := &v.tracked[i]
		if acknowledged&(1<<i) != 0 {
			if !entry.present {
				return nil, ErrInvalidAck
			}
			entry.pending = false
			lastSeen = append(lastSeen, entry.signature)
		} else {
			if entry.present && !entry.pending {
				return nil, ErrInvalidAck
			}
			*entry = trackedEntry{}
		}
	}
	if checksum != 0 && checksum != Checksum(lastSeen) {
		return nil, ErrInvalidAck
	}
	return lastSeen, nil
}

// Checksum ports LastSeenMessages.computeChecksum and Arrays.hashCode(byte[]).
func Checksum(signatures []Signature) byte {
	checksum := int32(1)
	for _, signature := range signatures {
		hash := int32(1)
		for _, b := range signature {
			hash = 31*hash + int32(int8(b))
		}
		checksum = 31*checksum + hash
	}
	result := byte(checksum)
	if result == 0 {
		return 1
	}
	return result
}

package chatsecurity

import (
	"slices"
	"testing"
	"time"
)

func TestCollectArgumentsPacketOrder(t *testing.T) {
	key, data, profile, trust := fixture(t)
	session, err := ValidateSession(profile, data, nil, trust)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	const salt = int64(78123)
	seen := []Signature{{0: 255}}
	parsed := []Argument{{"first", "héllo  世界 "}, {"second", "@a waves"}}
	decoder := NewDecoder(profile, session, true)
	// A typed message and both command arguments share the index sequence.
	body := NewBody("typed", now.UnixMilli(), salt, seen)
	if _, err := decoder.Decode(sign(t, key, profile, data.ID, 0, body), body, now); err != nil {
		t.Fatal(err)
	}
	entries := make([]ArgumentSignature, 0, 2)
	for i, argument := range []Argument{parsed[1], parsed[0]} {
		body := NewBody(argument.Value, now.UnixMilli(), salt, seen)
		entries = append(entries, ArgumentSignature{argument.Name, *sign(t, key, profile, data.ID, int32(i+1), body)})
	}
	arguments, err := decoder.CollectArguments(parsed, entries, now.UnixMilli(), salt, seen, now)
	if err != nil {
		t.Fatal(err)
	}
	for i, entry := range entries {
		message, ok := arguments.Message(entry.Name)
		if !ok || message.Index() != int32(i+1) || message.Sender() != profile || message.SessionID() != data.ID || *message.Signature() != entry.Signature {
			t.Fatalf("argument link lost: %+v", message)
		}
		body := message.Body()
		if body.Timestamp() != now.UnixMilli() || body.Salt() != salt || !slices.Equal(body.LastSeen(), seen) {
			t.Fatal("argument body changed")
		}
		// Repeated whisper deliveries and execution forks reuse the same link.
		copyContext := arguments
		again, _ := copyContext.Message(entry.Name)
		if again.Index() != message.Index() || *again.Signature() != *message.Signature() {
			t.Fatal("lookup advanced the chain")
		}
	}
	if _, ok := arguments.Message("unknown"); ok {
		t.Fatal("unknown argument found")
	}
	if message, _ := arguments.Message("first"); message.Body().Content() != parsed[0].Value {
		t.Fatal("signed whitespace or Unicode changed")
	}
	entries[0].Signature[0] ^= 1
	seen[0][0] = 0
	parsed[0].Value = "changed"
	if message, _ := arguments.Message("second"); message.Signature()[0] != entries[0].Signature[0]^1 || message.Body().LastSeen()[0][0] != 255 {
		t.Fatal("context aliases input storage")
	}
	body = NewBody("next typed", now.UnixMilli(), salt, nil)
	if _, err := decoder.Decode(sign(t, key, profile, data.ID, 3, body), body, now); err != nil {
		t.Fatalf("next typed message: %v", err)
	}
	var empty Arguments
	if _, ok := empty.Message("first"); ok {
		t.Fatal("zero context inherited earlier arguments")
	}
}

func TestCollectArgumentsFailures(t *testing.T) {
	key, data, profile, trust := fixture(t)
	session, err := ValidateSession(profile, data, nil, trust)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	parsed := []Argument{{"message", "hello"}, {"action", "waves"}}
	body := NewBody("hello", now.UnixMilli(), 12, nil)
	entry := ArgumentSignature{"message", *sign(t, key, profile, data.ID, 0, body)}
	t.Run("missing name preserves chain", func(t *testing.T) {
		decoder := NewDecoder(profile, session, true)
		got, err := decoder.CollectArguments(parsed, []ArgumentSignature{entry}, now.UnixMilli(), 12, nil, now)
		if err != InvalidCommandSignature || got.messages != nil {
			t.Fatalf("missing argument: %v", err)
		}
		if _, err := decoder.Decode(sign(t, key, profile, data.ID, 1, body), body, now); err != nil {
			t.Fatalf("missing name broke chain: %v", err)
		}
	})
	t.Run("unknown name breaks chain", func(t *testing.T) {
		decoder := NewDecoder(profile, session, false)
		unknown := entry
		unknown.Name = "unknown"
		if _, err := decoder.CollectArguments(parsed, []ArgumentSignature{unknown}, now.UnixMilli(), 12, nil, now); err != InvalidCommandSignature {
			t.Fatalf("unknown argument: %v", err)
		}
		if _, err := decoder.Decode(&entry.Signature, body, now); err != ChainBroken {
			t.Fatalf("unknown name did not break chain: %v", err)
		}
	})
	t.Run("tampering returns no partial context", func(t *testing.T) {
		decoder := NewDecoder(profile, session, true)
		bad := ArgumentSignature{"action", Signature{}}
		got, err := decoder.CollectArguments(parsed, []ArgumentSignature{entry, bad}, now.UnixMilli(), 12, nil, now)
		if err != InvalidSignature || got.messages != nil {
			t.Fatalf("tampered argument: %v", err)
		}
	})
	t.Run("duplicate names consume links", func(t *testing.T) {
		decoder := NewDecoder(profile, session, true)
		second := ArgumentSignature{"message", *sign(t, key, profile, data.ID, 1, body)}
		got, err := decoder.CollectArguments(parsed[:1], []ArgumentSignature{entry, second}, now.UnixMilli(), 12, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		message, _ := got.Message("message")
		if message.Index() != 1 || decoder.index != 2 {
			t.Fatal("duplicate entry did not replace message and consume link")
		}
	})
}

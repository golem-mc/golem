package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/google/uuid"
)

func BenchmarkPlayerChatEncode(b *testing.B) {
	name := protocol.Text{Text: "Player"}
	id := uuid.MustParse("00112233-4455-6677-8899-aabbccddeeff")
	message := protocol.Text{Text: "héllo  世界"}
	for b.Loop() {
		_ = playerChatPayload(42, name, id, chatType, message.Text, 1900000000123, 0, message, nil)
	}
}

func BenchmarkSignedPlayerChatEncode(b *testing.B) {
	name := protocol.Text{Text: "Player"}
	id := uuid.MustParse("00112233-4455-6677-8899-aabbccddeeff")
	resolved := protocol.Text{Text: "héllo  世界"}
	body := chatsecurity.NewBody(resolved.Text, 1900000000123, 17, make([]chatsecurity.Signature, 20))
	signature := &chatsecurity.Signature{}
	for b.Loop() {
		_ = chatMessagePayload(42, name, id, chatType, 31, signature, body, resolved, nil)
	}
}

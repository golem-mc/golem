package chat

import (
	"bytes"
	"encoding/json"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// onlineChatClient logs "Testificate" in against a stub session server and
// returns the client in the world.
func onlineChatClient(t *testing.T, cfg *config.Config, setup ...func(*server.Server)) *servertest.Client {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("username")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         strings.ReplaceAll(protocol.OfflineUUID(name).String(), "-", ""),
			"name":       name,
			"properties": []any{},
		})
	}))
	t.Cleanup(stub.Close)

	cfg.OnlineMode = true
	srv, c := servertest.Dial(t, cfg, setup...)
	srv.SetSessionServer(stub.URL)
	servertest.CompleteOnlineChatLogin(c, "Testificate")
	return c
}

// TestEnforceSecureProfileRefusesChatWithoutSession checks that with the
// property on in online mode a player without a chat session gets the red
// chat.disabled.missingProfileKey line and no broadcast.
// vanilla: SignedMessageChain.Decoder.unsigned
func TestEnforceSecureProfileRefusesChatWithoutSession(t *testing.T) {
	t.Parallel()
	c := onlineChatClient(t, config.Default())

	c.Chat("refused")
	if data := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Contains(data, []byte("chat.disabled.missingProfileKey")) {
		t.Errorf("system chat = % X, want chat.disabled.missingProfileKey", data)
	}

}

// TestEnforceSecureProfileOffAllowsChatWithoutSession checks the property
// off leaves an online-mode player's unsigned chat alone.
func TestEnforceSecureProfileOffAllowsChatWithoutSession(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.EnforceSecureProfile = false
	c := onlineChatClient(t, cfg)

	c.Chat("hello")
	c.ExpectChat("Testificate", "hello")
}

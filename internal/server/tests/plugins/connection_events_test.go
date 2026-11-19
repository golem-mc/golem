package plugins_test

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: ServerLoginPacketListenerImpl.verifyLoginAndFinishConnectionSetup, PlayerList.canPlayerLogin
func TestPluginPreLoginDenial(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "admission", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerPreLoginEvent) {
			if e.Profile.Name != "Denied" || e.Profile.ID != protocol.OfflineUUID("Denied") || e.Address == nil {
				t.Error("pre-login identity")
			}
			e.Deny, e.Reason = true, plugin.Plain("before admission")
		})
		return nil
	}}}})
	servertest.HoldTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.SendLoginStart("Denied")
	id, data, err := c.Read()
	if err != nil || id != protocol.IDLoginDisconnect {
		t.Fatalf("pre-login denial = %x, %v", id, err)
	}
	if got := protocol.NewReader(data).ReadString(32767); !strings.Contains(got, "before admission") {
		t.Fatalf("reason = %s", got)
	}
}

func TestPluginLoginResults(t *testing.T) {
	for _, result := range []plugin.LoginResult{plugin.LoginAllowed, plugin.LoginFull, plugin.LoginBanned, plugin.LoginWhitelist} {
		t.Run(fmt.Sprint(result), func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			cfg.MaxPlayers = 0
			if result != plugin.LoginFull {
				cfg.MaxPlayers = 20
			}
			cfg.WhiteList = result == plugin.LoginWhitelist
			var saved plugin.Player
			calls := atomic.Int32{}
			srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "login", Plugin: &testPlugin{enable: func(s plugin.Server) error {
				plugin.Listen(s, func(e *plugin.PlayerLoginEvent) {
					calls.Add(1)
					if e.Result != result || e.Address == nil || e.Player.Name() != "Login" || e.Player.UUID() != protocol.OfflineUUID("Login") || !e.Player.IsValid() {
						t.Errorf("login inputs: %v, %v", e.Result, e.Player.Name())
					}
					_ = s.Players() // callbacks must not hold the player-list lock
					saved = e.Player
					if result == plugin.LoginAllowed {
						e.Result, e.Reason = plugin.LoginOther, plugin.Plain("listener refused")
					} else {
						e.Result = plugin.LoginAllowed
					}
				})
				return nil
			}}}})
			if result == plugin.LoginBanned {
				path := filepath.Join(t.TempDir(), "banned-players.json")
				servertest.WriteJSONList(t, path, []servertest.BanRecord{{UUID: protocol.OfflineUUID("Login").String(), Name: "Login", Reason: "fixture"}})
				srv.SetBannedPlayersFile(path)
			}
			c := servertest.DialServer(t, srv)
			c.ViewAsk = config.MinDistance
			if result == plugin.LoginAllowed {
				c.SendLoginStart("Login")
				if got := protocol.NewReader(c.Expect(protocol.IDLoginDisconnect)).ReadString(32767); !strings.Contains(got, "listener refused") {
					t.Fatalf("login reason = %s", got)
				}
			} else {
				c.Join("Login") // the override must survive the final configuration checks
				servertest.HoldTicks(t, srv)
				if !saved.IsValid() || saved.Name() != "Login" {
					t.Fatal("connection handle did not survive joining")
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("login events = %d", calls.Load())
			}
		})
	}
}

// vanilla: ServerConfigurationPacketListenerImpl.handleConfigurationFinished, PlayerList.canPlayerLogin
func TestPluginLoginOverrideKeepsUnrelatedConfigurationRefusals(t *testing.T) {
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "admission", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerLoginEvent) {
			if e.Result != plugin.LoginBanned {
				t.Errorf("initial login result = %v, want banned", e.Result)
			}
			e.Result = plugin.LoginAllowed
		})
		plugin.Listen(s, func(*plugin.PlayerConfigureEvent) {
			if _, err := s.Dispatch(s.Console(), "whitelist on"); err != nil {
				t.Error(err)
			}
		})
		return nil
	}}}})
	path := filepath.Join(t.TempDir(), "banned-players.json")
	servertest.WriteJSONList(t, path, []servertest.BanRecord{{UUID: protocol.OfflineUUID("Override").String(), Name: "Override", Reason: "fixture"}})
	srv.SetBannedPlayersFile(path)
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Login("Override")
	c.Configure()
	reason, ok := protocol.ReadText(protocol.NewReader(c.Expect(protocol.IDPlayDisconnect)))
	if !ok || reason.Translate != "multiplayer.disconnect.not_whitelisted" {
		t.Fatalf("configuration refusal = %+v, want whitelist refusal", reason)
	}
}

// vanilla: ServerCommonPacketListenerImpl.handleCookieResponse, handleResourcePackResponse
func TestPluginConfigureConnectionOperations(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	key := plugin.Key{Namespace: "setup", Path: "cookie"}
	packID := protocol.OfflineUUID("pack")
	cookieDone := make(chan bool, 1)
	var saved plugin.Player
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "setup", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerConfigureEvent) {
			saved = e.Player
			if !saved.IsValid() || saved.Name() != "Setup" || saved.Profile().ID != protocol.OfflineUUID("Setup") || saved.Address() == nil {
				t.Error("configuration identity")
			}
			if len(s.Players()) != 0 {
				t.Error("configuration player was already in world")
			}
			saved.StoreCookie(key, []byte{9})
			saved.RequestCookie(key, func(data []byte, present bool) { cookieDone <- present && bytes.Equal(data, []byte{7}) })
			prompt := plugin.Plain("optional setup pack")
			saved.SendResourcePack(packID, "https://example.invalid/pack.zip", "", false, &prompt)
			saved.RemoveResourcePack(packID)
			saved.SendPluginMessage(plugin.Key{Namespace: "setup", Path: "hello"}, []byte{1, 2})
			saved.Transfer("example.invalid", 25566)
		})
		return nil
	}}}})
	c := servertest.DialServer(t, srv)
	c.NoBatchAcks = true
	c.Login("Setup")
	c.Expect(protocol.IDConfigPluginMessage) // brand
	r := protocol.NewReader(c.Expect(protocol.IDConfigStoreCookie))
	if r.ReadString(32767) != key.String() || !bytes.Equal(r.ReadBytes(5120), []byte{9}) || r.Err() != nil {
		t.Fatal("configuration cookie store")
	}
	r = protocol.NewReader(c.Expect(protocol.IDConfigCookieRequest))
	if r.ReadString(32767) != key.String() || r.Err() != nil {
		t.Fatal("configuration cookie request")
	}
	r = protocol.NewReader(c.Expect(protocol.IDConfigResourcePackPush))
	if r.ReadUUID() != packID || r.ReadString(32767) != "https://example.invalid/pack.zip" || r.ReadString(40) != "" || r.ReadBool() || !r.ReadBool() {
		t.Fatal("configuration resource pack")
	}
	if text, ok := protocol.ReadText(r); !ok || text.Plain() != "optional setup pack" {
		t.Fatal("configuration pack prompt")
	}
	r = protocol.NewReader(c.Expect(protocol.IDConfigResourcePackPop))
	if !r.ReadBool() || r.ReadUUID() != packID || r.Err() != nil {
		t.Fatal("configuration pack removal")
	}
	r = protocol.NewReader(c.Expect(protocol.IDConfigPluginMessage))
	if r.ReadString(32767) != "setup:hello" || !bytes.Equal(r.Rest(), []byte{1, 2}) {
		t.Fatal("configuration custom payload")
	}
	r = protocol.NewReader(c.Expect(protocol.IDConfigTransfer))
	if r.ReadString(32767) != "example.invalid" || r.ReadVarInt() != 25566 || r.Err() != nil {
		t.Fatal("configuration transfer")
	}
	packs := c.Expect(protocol.IDKnownPacks) // setup callbacks have finished
	servertest.HoldTicks(t, srv)
	c.Send(protocol.IDConfigCookieResponse, func(b *protocol.Buffer) { b.PutString(key.String()); b.PutBool(true); b.PutBytes([]byte{7}) })
	// A response to the next packet proves the cookie callback is queued
	// before held ticks can outrun the connection goroutine.
	c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) { b.PutRaw(packs) })
	c.Expect(protocol.IDFeatureFlags)
	servertest.UntilTicks(t, srv, 20, "configuration cookie callback", func() bool {
		select {
		case ok := <-cookieDone:
			if !ok {
				t.Fatal("configuration cookie callback")
			}
			return true
		default:
			return false
		}
	})
	servertest.ReleaseTicks(t, srv)
	c.Raw.Close()
	// Wait for cleanup through the server's connection accounting, not a wall-clock sleep.
	servertest.UntilTicks(t, srv, 20, "configuration handle invalidation", func() bool { return !saved.IsValid() })
}

func TestPluginClientNotifications(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	var settings []string
	var statuses []plugin.PackStatus
	var clicks [][]byte
	packID := protocol.OfflineUUID("pack")
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "notices", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerClientSettingsEvent) {
			if e.Player.Name() != "Notices" || e.ViewDistance != int(config.MinDistance) {
				t.Error("settings inputs")
			}
			settings = append(settings, e.Locale)
		})
		plugin.Listen(s, func(e *plugin.PlayerResourcePackStatusEvent) {
			if e.ID != packID || e.Player.Name() != "Notices" {
				t.Error("pack inputs")
			}
			statuses = append(statuses, e.Status)
		})
		plugin.Listen(s, func(e *plugin.PlayerCustomClickEvent) {
			if e.ID.String() != "notices:click" || e.Player.Name() != "Notices" {
				t.Error("click inputs")
			}
			clicks = append(clicks, e.Payload)
		})
		return nil
	}}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Notices")
	servertest.HoldTicks(t, srv)
	c.Settle()
	if len(settings) != 1 || settings[0] != "en_GB" {
		t.Fatalf("configuration settings = %v", settings)
	}
	c.Send(protocol.IDPlayClientInformation, func(b *protocol.Buffer) {
		b.PutString("de_de")
		b.PutI8(config.MinDistance)
		b.PutVarInt(0)
		b.PutBool(true)
		b.PutU8(127)
		b.PutVarInt(1)
		b.PutBool(false)
		b.PutBool(true)
		b.PutVarInt(0)
	})
	for status := range 8 {
		c.Send(protocol.IDPlayResourcePackIn, func(b *protocol.Buffer) { b.PutUUID(packID); b.PutVarInt(int32(status)) })
	}
	data, err := nbt.EncodeNetwork(nbt.Compound{"n": nbt.Int(7)})
	if err != nil {
		t.Fatal(err)
	}
	c.Send(protocol.IDPlayCustomClickAction, func(b *protocol.Buffer) { b.PutString("notices:click"); b.PutBytes(data) })
	c.Send(protocol.IDPlayCustomClickAction, func(b *protocol.Buffer) { b.PutString("notices:click"); b.PutBytes([]byte{0}) })
	c.Settle()
	if len(settings) != 1 || len(statuses) != 0 || len(clicks) != 0 {
		t.Fatal("notification ran on connection goroutine")
	}
	servertest.RunTicks(t, srv, 1)
	if len(settings) != 2 || settings[1] != "de_de" || len(statuses) != 8 || len(clicks) != 2 || !bytes.Equal(clicks[0], data) || clicks[1] != nil {
		t.Fatalf("notifications: %v, %v, %v", settings, statuses, clicks)
	}
	for i, status := range statuses {
		if int(status) != i {
			t.Fatal("pack status order")
		}
	}
}

// vanilla: ServerboundCustomClickActionPacket.UNTRUSTED_TAG_CODEC, NbtAccounter.pushDepth
func TestPluginCustomClickWireDepth(t *testing.T) {
	for _, pairs := range []int{8, 9} {
		t.Run(fmt.Sprint(pairs), func(t *testing.T) {
			data := []byte{byte(nbt.TypeInt), 0, 0, 0, 7}
			for range pairs {
				// Each empty-name compound unwraps during decoding, but both
				// containers must count against the wire depth limit of 16.
				outer := []byte{byte(nbt.TypeList), byte(nbt.TypeCompound), 0, 0, 0, 1, data[0], 0, 0}
				outer = append(outer, data[1:]...)
				data = append(outer, byte(nbt.TypeEnd))
			}
			key := plugin.Key{Namespace: "depth", Path: "click"}
			srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "depth", Plugin: &testPlugin{enable: func(s plugin.Server) error {
				plugin.Listen(s, func(e *plugin.PlayerCustomClickEvent) {
					if e.ID != key || e.Player.Name() != "Depth" {
						t.Error("custom click identity")
					}
					e.Player.SendPluginMessage(key, e.Payload)
				})
				return nil
			}}}})
			c := servertest.DialServer(t, srv)
			c.Login("Depth")
			c.Expect(protocol.IDConfigPluginMessage)
			c.Expect(protocol.IDKnownPacks)
			c.Send(protocol.IDConfigCustomClickAction, func(b *protocol.Buffer) { b.PutString(key.String()); b.PutBytes(data) })
			if pairs == 8 {
				r := protocol.NewReader(c.Expect(protocol.IDConfigPluginMessage))
				if r.ReadString(32767) != key.String() || !bytes.Equal(r.Rest(), data) || r.Err() != nil {
					t.Fatal("custom click changed raw network NBT")
				}
			} else {
				c.Expect(protocol.IDConfigDisconnect)
			}
		})
	}
}

// vanilla: SignBlockEntity.executeClickCommandsIfPresent
func TestPluginSignCustomClick(t *testing.T) {
	var clicks []*plugin.PlayerCustomClickEvent
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "signs", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerCustomClickEvent) {
			clicks = append(clicks, e)
		})
		return nil
	}}}}, servertest.OpsListing(t, "SignClick"))
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("SignClick")
	servertest.HoldTicks(t, srv)
	c.MoveTo(4.5, 64, 5.5, 0, 0)
	c.Settle()
	data, err := nbt.EncodeNetwork(nbt.Compound{"n": nbt.Int(7)})
	if err != nil {
		t.Fatal(err)
	}
	for _, allowed := range []bool{true, false} {
		opFeatures := ""
		if allowed {
			opFeatures = "allow_op_features:1b,"
		}
		c.ChatCommand(`setblock 4 65 7 minecraft:oak_sign{is_waxed:1b,` + opFeatures + `front_text:{messages:[{text:"Click",click_event:{action:"custom",id:"signs:click",payload:{n:7}}},"","",""]}}`)
		c.ExpectCommandFeedback("commands.setblock.success", "4", "65", "7")
		c.Settle()
		c.UseBlock(4, 65, 7)
		c.Settle()
		if len(clicks) != 0 {
			t.Fatal("sign callback ran on connection goroutine")
		}
		servertest.RunTicks(t, srv, 1)
		if allowed {
			if len(clicks) != 1 {
				t.Fatalf("enabled sign events = %d, want 1", len(clicks))
			}
			e := clicks[0]
			if !e.Player.IsValid() || e.Player.Name() != "SignClick" || e.Player.UUID() != protocol.OfflineUUID("SignClick") || e.ID.String() != "signs:click" || !bytes.Equal(e.Payload, data) {
				t.Fatalf("sign click = %+v", e)
			}
		} else if len(clicks) != 0 {
			t.Fatal("sign without op features fired a plugin event")
		}
		clicks = nil
	}
}

func TestPluginChannelOwnershipAndDelivery(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	key := plugin.Key{Namespace: "channels", Path: "echo"}
	var host plugin.Server
	calls := 0
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{
		{Name: "channels", Plugin: &testPlugin{enable: func(s plugin.Server) error {
			host = s
			s.Messenger().Listen(key, func(p plugin.Player, data []byte) {
				if p.Name() != "Channels" || !bytes.Equal(data, []byte{7, 8}) {
					t.Error("channel inputs")
				}
				calls++
				s.Messenger().Send(p, key, data)
				data[0] = 99
			})
			s.Messenger().Listen(key, func(_ plugin.Player, data []byte) {
				if !bytes.Equal(data, []byte{7, 8}) {
					t.Error("listener payload was shared")
				}
				calls++
			})
			return nil
		}}},
		{Name: "failed", Plugin: &testPlugin{enable: func(s plugin.Server) error {
			s.Messenger().Listen(key, func(plugin.Player, []byte) { t.Error("failed shared-key listener called") })
			s.Messenger().Listen(plugin.Key{Namespace: "failed", Path: "removed"}, func(plugin.Player, []byte) { t.Error("failed listener called") })
			return errors.New("fixture")
		}}},
	})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Login("Channels")
	c.Configure()
	r := protocol.NewReader(c.AwaitPacket(protocol.IDPlayPluginMessage))
	if r.ReadString(32767) != "minecraft:register" || string(r.Rest()) != key.String() {
		t.Fatal("initial channel registration")
	}
	c.EnterWorld("Channels")
	servertest.HoldTicks(t, srv)
	c.Settle()
	dynamic := plugin.Key{Namespace: "channels", Path: "dynamic"}
	host.Messenger().Listen(dynamic, func(plugin.Player, []byte) {})
	servertest.RunTicks(t, srv, 1)
	r = protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayPluginMessage))
	if r.ReadString(32767) != "minecraft:register" || string(r.Rest()) != dynamic.String() {
		t.Fatal("dynamic channel registration")
	}
	c.Send(protocol.IDPlayPluginMessageIn, func(b *protocol.Buffer) { b.PutString(key.String()); b.PutRaw([]byte{7, 8}) })
	c.Settle()
	if calls != 0 {
		t.Fatal("channel callback ran on connection goroutine")
	}
	servertest.RunTicks(t, srv, 1)
	r = protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayPluginMessage))
	if r.ReadString(32767) != key.String() || !bytes.Equal(r.Rest(), []byte{7, 8}) || calls != 2 {
		t.Fatal("channel reply")
	}
	c.Send(protocol.IDPlayPluginMessageIn, func(b *protocol.Buffer) { b.PutString("failed:removed"); b.PutRaw([]byte{7, 8}) })
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if calls != 2 {
		t.Fatal("unregistered channel dispatched")
	}
}

func TestPluginRequiredPackReportsBeforeDisconnect(t *testing.T) {
	packID := protocol.OfflineUUID("required")
	reported := make(chan bool, 1)
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "required", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerConfigureEvent) {
			e.Player.SendResourcePack(packID, "https://example.invalid/pack", "", true, nil)
		})
		plugin.Listen(s, func(e *plugin.PlayerResourcePackStatusEvent) {
			reported <- e.ID == packID && e.Status == plugin.PackDeclined && e.Player.IsValid() && e.Player.Name() == "Required"
		})
		return nil
	}}}})
	c := servertest.DialServer(t, srv)
	c.Login("Required")
	c.Expect(protocol.IDConfigPluginMessage)
	c.Expect(protocol.IDConfigResourcePackPush)
	c.Expect(protocol.IDKnownPacks)
	c.Send(protocol.IDConfigResourcePackIn, func(b *protocol.Buffer) { b.PutUUID(packID); b.PutVarInt(int32(plugin.PackDeclined)) })
	text, ok := protocol.ReadText(protocol.NewReader(c.Expect(protocol.IDConfigDisconnect)))
	if !ok || text.Translate != "multiplayer.requiredTexturePrompt.disconnect" {
		t.Fatal("required pack denial")
	}
	select {
	case ok := <-reported:
		if !ok {
			t.Fatal("pack status inputs")
		}
	default:
		t.Fatal("pack denial was not reported before disconnect")
	}
}

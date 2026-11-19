package network

import (
	"bytes"
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The bounds the wire input is held to, matching vanilla's: a field a
// packet's format allows a small set of numbered values - statuses, counts -
// and a value outside the set fails the packet's decode there and costs its
// sender their connection alone, the server going on serving.
// vanilla: FriendlyByteBuf.readEnum, ByteBufCodecs.map

// wantDecodeFailed builds the disconnect reason a packet that did not read
// is sent away with, the message vanilla's Connection.exceptionCaught
// hands a client whose packet did not decode.
func wantDecodeFailed(packet string) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{
		Translate: "disconnect.genericReason",
		With:      []protocol.Text{{Text: "Internal Exception: io.netty.handler.codec.DecoderException: Failed to decode packet 'serverbound/minecraft:" + packet + "'"}},
	})
	return b.Bytes()
}

// wantExtraBytes builds the disconnect reason vanilla's PacketDecoder hands a
// client whose packet carried bytes past the fields it read.
func wantExtraBytes(state, packet, class string, extra int) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{
		Translate: "disconnect.genericReason",
		With: []protocol.Text{{Text: fmt.Sprintf(
			"Internal Exception: io.netty.handler.codec.DecoderException: java.io.IOException: Packet %s/serverbound/minecraft:%s (%s) was larger than I expected, found %d bytes extra whilst reading packet serverbound/minecraft:%s",
			state, packet, class, extra, packet)}},
	})
	return b.Bytes()
}

// wantDisconnect builds the disconnect reason a packet refused by a gameplay
// rule is sent away with, a bare translation key.
func wantDisconnect(key string) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Translate: key})
	return b.Bytes()
}

// wantLoginDisconnect builds the login disconnect reason, whose component
// travels in the JSON form the login Disconnect packet carries.
func wantLoginDisconnect(key string) []byte {
	var b protocol.Buffer
	b.PutString(protocol.JSONText(protocol.Text{Translate: key}))
	return b.Bytes()
}

// expectDecodeDisconnect fails unless the server's next word to the client
// is a disconnect for a packet that did not read, carrying vanilla's own
// reason for one, and waits for the player to have been put away.
func expectDecodeDisconnect(t *testing.T, srv *server.Server, c *servertest.Client, packet string) {
	t.Helper()
	if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, wantDecodeFailed(packet)) {
		t.Errorf("disconnect reason % X, want % X", reason, wantDecodeFailed(packet))
	}
	servertest.WaitFor(t, srv, 100, func() bool { return srv.PlayerCount() == 0 })
}

// TestAWireValueTheDecoderRefusesDisconnectsItsSender sends, one case each,
// a packet that carries a value outside what its format allows - a Player
// Action status outside its enum, a changed-slots count past the codec's 128 or under zero - or a packet cut
// short mid-field: a Use Item On, a Use Item, a Player Action.
// Every one is a disconnect for its sender alone.
func TestAWireValueTheDecoderRefusesDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		packet string
		send   func(*servertest.Client)
	}{
		{
			"a use item on that ends mid-field",
			"use_item_on",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) { b.PutVarInt(protocol.HandMain) })
			},
		},
		{
			"a player action whose status is below the enum",
			"player_action",
			func(c *servertest.Client) { c.SendPlayerAction(-1) },
		},
		{
			"a player action whose status is past the enum",
			"player_action",
			// 9 is one past vanilla's enum, whose last is the stab (8).
			func(c *servertest.Client) { c.SendPlayerAction(9) },
		},
		{
			"an empty use item",
			"use_item",
			func(c *servertest.Client) { c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {}) },
		},
		{
			"a player action that ends mid-field",
			"player_action",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) { b.PutVarInt(0) })
			},
		},
		{
			"a click container claiming a negative number of changed slots",
			"container_click",
			func(c *servertest.Client) { c.ClickClaims(-1) },
		},
		{
			"a click container claiming more changed slots than the codec's 128",
			"container_click",
			func(c *servertest.Client) { c.ClickClaims(129) },
		},
		{
			"a hashed slot adding more components than the codec's 256",
			"container_click",
			func(c *servertest.Client) { c.ClickHashedComponents(257, 0) },
		},
		{
			"a hashed slot removing more components than the codec's 256",
			"container_click",
			func(c *servertest.Client) { c.ClickHashedComponents(0, 257) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.BlockTestConfig())

			sender := connect().Join("Sender")

			tc.send(sender)
			expectDecodeDisconnect(t, srv, sender, tc.packet)

			// The disconnect cost its sender alone: the server serves
			// whoever comes next.
			keeper := connect().Join("Keeper")
			keeper.Chat("still here")
			keeper.ExpectChat("Keeper", "still here")
		})
	}
}

// TestAWireValueTheDecoderRefusesBeforeTheClientLoads sends a packet whose
// status is outside its enum, or that is cut off mid-field, while the
// loading screen is still up: vanilla decodes on the netty thread, before its
// hasClientLoaded check, so the value costs the sender the connection even
// then.
func TestAWireValueTheDecoderRefusesBeforeTheClientLoads(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		packet string
		send   func(*servertest.Client)
	}{
		{
			"a player action whose status is past the enum",
			"player_action",
			func(c *servertest.Client) { c.SendPlayerAction(9) },
		},
		{
			"a position cut off before the client loads",
			"move_player_pos",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) { b.PutF64(0) })
			},
		},
		{
			"a position and rotation cut off before the client loads",
			"move_player_pos_rot",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayPositionAndRotation, func(b *protocol.Buffer) { b.PutF64(0) })
			},
		},
		{
			"a rotation cut off before the client loads",
			"move_player_rot",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayRotation, func(b *protocol.Buffer) { b.PutF32(0) })
			},
		},
		{
			"movement flags cut off before the client loads",
			"move_player_status_only",
			func(c *servertest.Client) { c.Send(protocol.IDPlayMovementFlags, nil) },
		},
		{
			"an attack cut off before the client loads",
			"attack",
			func(c *servertest.Client) { c.Send(protocol.IDPlayAttackIn, nil) },
		},
		{
			"an interact cut off before the client loads",
			"interact",
			func(c *servertest.Client) { c.Send(protocol.IDPlayInteractIn, nil) },
		},
		{
			"a player command cut off before the client loads",
			"player_command",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) { b.PutVarInt(0) })
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.BlockTestConfig())

			sender := connect()
			sender.Login("Sender")
			sender.Configure()
			// The chunk stream has run and Player Loaded is yet to come:
			// the loading screen is still up.
			sender.ReadThroughJoin("Sender")

			tc.send(sender)
			expectDecodeDisconnect(t, srv, sender, tc.packet)
		})
	}
}

// TestAStabActionTheVanillaEnumCarriesIsIgnored pins the top of the Player
// Action status' own range: vanilla's enum runs to status 7, the stab,
// which this server has no behaviour for, so
// the packet is ignored with the sender kept - one past it, the case
// above, is a value no vanilla client would ever reach a server with.
func TestAStabActionTheVanillaEnumCarriesIsIgnored(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Stabber")

	c.SendPlayerAction(7) // the stab, status 7
	// No behaviour means no answer to the sequence; the connection is
	// still good behind it.
	c.Settle()
	if got := srv.PlayerCount(); got != 1 {
		t.Fatalf("player count = %d, want the stab not to cost its sender the connection", got)
	}
	c.Chat("still here")
	c.ExpectChat("Stabber", "still here")
}

// The depth limit on the decode of a slot and its components: a container
// component holds slots, and every one of those can carry a container
// again, so a level of nesting costs a few bytes on the wire but a frame
// of the decode. A Set Creative Mode Slot packet can nest a million levels
// inside the decompressed packet cap, so a stack past the limit fails the
// packet and ends the one connection that sent it.

// nestedCreativeSlot writes a Set Creative Mode Slot packet setting a stack
// whose every level carries one container entry holding a container again,
// levels deep. The outer component's value travels inside the byte length
// the untrusted slot shape prefixes it with; the nesting under it rides
// bare, the way the entries inside a component do. Each level costs seven
// bytes, so a nest a real client could never build still fits a packet.
func nestedCreativeSlot(b *protocol.Buffer, slot int16, item, levels int32) {
	b.PutI16(slot)
	b.PutVarInt(1) // the count
	b.PutVarInt(item)
	b.PutVarInt(1) // one component added
	b.PutVarInt(0) // none removed
	b.PutVarInt(protocol.DataComponentContainer)
	var value protocol.Buffer
	for range levels {
		value.PutVarInt(1) // one entry
		value.PutBool(true)
		value.PutVarInt(item) // the entry's item
		value.PutVarInt(1)    // the entry's count
		value.PutVarInt(1)    // one component added
		value.PutVarInt(0)    // none removed
		value.PutVarInt(protocol.DataComponentContainer)
	}
	value.PutVarInt(0) // the innermost entry adds nothing
	value.PutVarInt(0) // and removes nothing
	b.PutVarInt(int32(value.Len()))
	b.PutRaw(value.Bytes())
}

// TestDeeplyNestedCreativeSlotDisconnectsOnlyTheSender pins the whole fix:
// a stack nested a million levels deep fails the packet — the decode runs
// before the creative-mode gate, so the sender's game mode does not matter
// — the sender alone is disconnected, and everyone else keeps playing.
func TestDeeplyNestedCreativeSlotDisconnectsOnlyTheSender(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())

	watcher := connect().Join("Watcher")

	bomber := connect().Join("Bomber")
	bomberEntity := watcher.ExpectPlayerAdded("Bomber")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Bomber")

	// A million levels a few bytes apiece fill the 8 MiB decompressed cap.
	bomber.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
		nestedCreativeSlot(b, 36, 1, 1_100_000)
	})

	var want protocol.Buffer
	want.PutText(protocol.Text{
		Translate: "disconnect.genericReason",
		With:      []protocol.Text{{Text: "Internal Exception: io.netty.handler.codec.DecoderException: Failed to decode packet 'serverbound/minecraft:set_creative_mode_slot'"}},
	})
	if got := bomber.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(got, want.Bytes()) {
		t.Errorf("disconnect reason % X, want % X", got, want.Bytes())
	}
	servertest.WaitFor(t, srv, 100, func() bool { return srv.PlayerCount() == 1 })

	// The others never stopped playing: the leaving is announced to
	// whoever is left, and the player still there can keep talking.
	watcher.ExpectPlayerRemoved(bomberEntity, "Bomber")
	watcher.ExpectAnnouncement("multiplayer.player.left", "Bomber")
	watcher.Chat("still here")
	watcher.ExpectChat("Watcher", "still here")
}

// TestCreativeSlotKeepsNestingWithinTheDepthLimit checks the other side of
// the limit: a stack nested seventeen containers deep is taken in, kept in
// the slot it was set to, and relayed to the other players whole.
func TestCreativeSlotKeepsNestingWithinTheDepthLimit(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	packer := connect()
	packer.Login("Packer")
	packer.Configure()
	packer.Inventory()
	packer.EnterWorld("Packer")
	packerEntity := watcher.ExpectPlayerAdded("Packer")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Packer")

	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the registry has no stone")
	}
	const levels = 17
	packer.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
		nestedCreativeSlot(b, 36, stone, levels)
	})

	want := protocol.ItemStack{ItemID: stone, Count: 1}
	for range levels {
		want = protocol.ItemStack{ItemID: stone, Count: 1, Container: []protocol.ItemStack{want}}
	}
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.InventorySlot("Packer", player.HotbarSlot).Equal(want)
	})
	if got := srv.InventorySlot("Packer", player.HotbarSlot); !got.Equal(want) {
		t.Errorf("the held stack = %+v, want the nested stack %+v", got, want)
	}

	// And the stack rides back out: the selected hotbar slot is visible,
	// so the others are told what the packer now holds, nesting and all.
	entries := watcher.ExpectEquipment(packerEntity)
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); !got.Equal(want) {
		t.Errorf("the relayed equipment = %+v, want the nested stack %+v", got, want)
	}
}

// TestAPlayPacketThatFailsToDecodeDisconnectsItsSender drives every play
// handler that reads a packet with input its decoder refuses - a string whose
// length prefix exceeds 3 x its limit, or a packet cut off mid-field - and
// checks each one disconnects its sender with vanilla's reason instead of
// ignoring the packet. vanilla: Connection.exceptionCaught
func TestAPlayPacketThatFailsToDecodeDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		packet string
		send   func(*servertest.Client)
	}{
		{
			"a chat whose string overflows its limit",
			"chat",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayChatMessage, func(b *protocol.Buffer) { b.PutVarInt(3*256 + 1) })
			},
		},
		{
			"a chat command whose string overflows its limit",
			"chat_command",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayChatCommand, func(b *protocol.Buffer) { b.PutVarInt(3*32767 + 1) })
			},
		},
		{
			"a signed chat command whose string overflows its limit",
			"chat_command_signed",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlaySignedChatCommand, func(b *protocol.Buffer) { b.PutVarInt(3*32767 + 1) })
			},
		},
		{
			"a command suggestion whose string overflows its limit",
			"command_suggestion",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayCommandSuggestion, func(b *protocol.Buffer) {
					b.PutVarInt(0) // the request's ID
					b.PutVarInt(3*32500 + 1)
				})
			},
		},
		{
			"client information whose locale overflows its limit",
			"client_information",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayClientInformation, func(b *protocol.Buffer) { b.PutVarInt(3*16 + 1) })
			},
		},
		{
			"client information that ends after the skin parts",
			"client_information",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayClientInformation, func(b *protocol.Buffer) {
					b.PutString("en")
					b.PutI8(10)
					b.PutVarInt(0)  // chat mode
					b.PutBool(true) // chat colors
					b.PutU8(0xff)   // skin parts
				})
			},
		},
		{
			"a plugin message whose channel overflows its limit",
			"custom_payload",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayPluginMessageIn, func(b *protocol.Buffer) { b.PutVarInt(3*32767 + 1) })
			},
		},
		{
			"a click container cut off before its claims",
			"container_click",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayClickContainer, func(b *protocol.Buffer) { b.PutVarInt(0) })
			},
		},
		{
			"a rename whose string overflows its limit",
			"rename_item",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayRenameItemIn, func(b *protocol.Buffer) { b.PutVarInt(3*32767 + 1) })
			},
		},
		{
			"a close container cut off",
			"container_close",
			func(c *servertest.Client) { c.Send(protocol.IDPlayCloseContainerIn, nil) },
		},
		{
			"a container button click cut off",
			"container_button_click",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayContainerButtonClickIn, func(b *protocol.Buffer) { b.PutVarInt(0) })
			},
		},
		{
			"an attack cut off",
			"attack",
			func(c *servertest.Client) { c.Send(protocol.IDPlayAttackIn, nil) },
		},
		{
			"an interact cut off",
			"interact",
			func(c *servertest.Client) { c.Send(protocol.IDPlayInteractIn, nil) },
		},
		{
			"a client command cut off",
			"client_command",
			func(c *servertest.Client) { c.Send(protocol.IDPlayClientCommandIn, nil) },
		},
		{
			"a client command whose action is past the enum",
			"client_command",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) { b.PutVarInt(5) })
			},
		},
		{
			"a debug subscription request cut off",
			"debug_subscription_request",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayDebugSubscriptionRequest, func(b *protocol.Buffer) { b.PutVarInt(1) })
			},
		},
		{
			"a debug subscription naming an unknown registry id",
			"debug_subscription_request",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayDebugSubscriptionRequest, func(b *protocol.Buffer) {
					b.PutVarInt(1)  // one subscription
					b.PutVarInt(99) // not an entry of the registry
				})
			},
		},
		{
			"a debug subscription request past the codec's 32",
			"debug_subscription_request",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayDebugSubscriptionRequest, func(b *protocol.Buffer) { b.PutVarInt(33) })
			},
		},
		{
			"a teleport acceptance cut off",
			"accept_teleportation",
			func(c *servertest.Client) { c.Send(protocol.IDPlayConfirmTeleport, nil) },
		},
		{
			"a position cut off",
			"move_player_pos",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) { b.PutF64(0) })
			},
		},
		{
			"a position and rotation cut off",
			"move_player_pos_rot",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayPositionAndRotation, func(b *protocol.Buffer) { b.PutF64(0) })
			},
		},
		{
			"a rotation cut off",
			"move_player_rot",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayRotation, func(b *protocol.Buffer) { b.PutF32(0) })
			},
		},
		{
			"movement flags cut off",
			"move_player_status_only",
			func(c *servertest.Client) { c.Send(protocol.IDPlayMovementFlags, nil) },
		},
		{
			"a pick item from block cut off",
			"pick_item_from_block",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayPickItemFromBlock, func(b *protocol.Buffer) { b.PutPosition(0, 63, 1) })
			},
		},
		{
			"a held item change cut off",
			"set_carried_item",
			func(c *servertest.Client) { c.Send(protocol.IDPlayHeldItemIn, nil) },
		},
		{
			"player input cut off",
			"player_input",
			func(c *servertest.Client) { c.Send(protocol.IDPlayPlayerInput, nil) },
		},
		{
			"player abilities cut off",
			"player_abilities",
			func(c *servertest.Client) { c.Send(protocol.IDPlayPlayerAbilitiesIn, nil) },
		},
		{
			"a player command cut off",
			"player_command",
			func(c *servertest.Client) { c.Send(protocol.IDPlayPlayerCommand, nil) },
		},
		{
			"a player command with no data field",
			"player_command",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
					b.PutVarInt(0) // entity ID
					b.PutVarInt(protocol.PlayerCommandStartSprinting)
				})
			},
		},
		{
			"a player command whose action is past the enum",
			"player_command",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
					b.PutVarInt(0) // entity ID
					b.PutVarInt(9) // one past vanilla's last action (6)
					b.PutVarInt(0) // the data field
				})
			},
		},
		{
			"a keep alive cut off",
			"keep_alive",
			func(c *servertest.Client) { c.Send(protocol.IDPlayKeepAliveIn, nil) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, sender := servertest.Join(t, servertest.BlockTestConfig(), "Sender")

			tc.send(sender)
			expectDecodeDisconnect(t, srv, sender, tc.packet)
		})
	}
}

// TestAConfigurationPacketThatFailsToDecodeDisconnectsItsSender sends a
// client_information that ends after the skin parts while the configuration
// phase is still running: the packet decodes on the read loop there too, so it
// costs the sender the connection before the play phase begins.
// vanilla: Connection.exceptionCaught
func TestAConfigurationPacketThatFailsToDecodeDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())

	sender := connect()
	sender.Login("Sender")

	// The server introduces itself before it reads anything back.
	brand := protocol.NewReader(sender.Expect(protocol.IDConfigPluginMessage))
	if got := brand.ReadString(32767); got != "minecraft:brand" {
		t.Errorf("first plugin message channel = %q, want minecraft:brand", got)
	}
	sender.Expect(protocol.IDKnownPacks)

	sender.Send(protocol.IDClientInformation, func(b *protocol.Buffer) {
		b.PutString("en")
		b.PutI8(10)
		b.PutVarInt(0)  // chat mode
		b.PutBool(true) // chat colors
		b.PutU8(0xff)   // skin parts
	})
	if reason := sender.Expect(protocol.IDConfigDisconnect); !bytes.Equal(reason, wantDecodeFailed("client_information")) {
		t.Errorf("disconnect reason % X, want % X", reason, wantDecodeFailed("client_information"))
	}

	// The disconnect cost its sender alone: the server serves whoever
	// comes next.
	keeper := connect().Join("Keeper")
	keeper.Chat("still here")
	keeper.ExpectChat("Keeper", "still here")
}

// TestAKnownPacksReplyPastTheCodecLimitDisconnectsItsSender sends a Known
// Packs reply listing 65 packs, one past the 64 the codec reads: the count
// fails the decode and costs its sender the connection.
// vanilla: ServerboundSelectKnownPacks.STREAM_CODEC
func TestAKnownPacksReplyPastTheCodecLimitDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())

	sender := connect()
	sender.Login("Sender")

	// The server introduces itself before it reads anything back.
	sender.Expect(protocol.IDConfigPluginMessage)
	sender.Expect(protocol.IDKnownPacks)

	sender.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(65)
		for range 65 {
			b.PutString("minecraft")
			b.PutString("core")
			b.PutString(registry.Version)
		}
	})
	reason, ok := servertest.ReadUntil(t, sender, servertest.ExpectTimeout, protocol.IDConfigDisconnect)
	if !ok {
		t.Fatal("no configuration disconnect for a known-packs count past the codec's 64")
	}
	if !bytes.Equal(reason, wantDecodeFailed("select_known_packs")) {
		t.Errorf("disconnect reason % X, want % X", reason, wantDecodeFailed("select_known_packs"))
	}
}

// TestAKnownPacksReplyAtTheCodecLimitIsAccepted sends a Known Packs reply
// listing exactly 64 packs, the most the codec reads, with the vanilla pack
// among them: the count is accepted and the server finishes configuration.
// vanilla: ServerboundSelectKnownPacks.STREAM_CODEC
func TestAKnownPacksReplyAtTheCodecLimitIsAccepted(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())

	sender := connect()
	sender.Login("Sender")

	// The server introduces itself before it reads anything back.
	sender.Expect(protocol.IDConfigPluginMessage)
	sender.Expect(protocol.IDKnownPacks)

	sender.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(64)
		for i := range 63 {
			b.PutString("example")
			b.PutString(fmt.Sprintf("pack_%d", i))
			b.PutString("1")
		}
		b.PutString("minecraft")
		b.PutString("core")
		b.PutString(registry.Version)
	})

	if _, ok := servertest.ReadUntil(t, sender, servertest.ExpectTimeout, protocol.IDConfigFinish); !ok {
		t.Fatal("no finish for a known-packs count at the codec's 64")
	}
}

// TestANegativeKnownPacksCountDisconnectsItsSender sends a Known Packs reply
// whose count is negative: vanilla's list codec rejects a negative capacity
// before reading, so the count fails the decode rather than falling through to
// the "does not know data pack" disconnect.
// vanilla: ServerboundSelectKnownPacks.STREAM_CODEC
func TestANegativeKnownPacksCountDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())

	sender := connect()
	sender.Login("Sender")

	// The server introduces itself before it reads anything back.
	sender.Expect(protocol.IDConfigPluginMessage)
	sender.Expect(protocol.IDKnownPacks)

	sender.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) { b.PutVarInt(-1) })
	reason, ok := servertest.ReadUntil(t, sender, servertest.ExpectTimeout, protocol.IDConfigDisconnect)
	if !ok {
		t.Fatal("no configuration disconnect for a negative known-packs count")
	}
	if !bytes.Equal(reason, wantDecodeFailed("select_known_packs")) {
		t.Errorf("disconnect reason % X, want % X", reason, wantDecodeFailed("select_known_packs"))
	}
}

// TestAPacketThatDecodesButBreaksAGameplayRuleStillOnlyDrops pins the other
// side of the decode failures: a packet the decoder accepts but a gameplay rule
// refuses - a hotbar slot outside the nine, a move out of the world - is still
// dropped alone, the connection left good.
func TestAPacketThatDecodesButBreaksAGameplayRuleStillOnlyDrops(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		send func(*servertest.Client)
	}{
		{
			"a hotbar slot outside the nine",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayHeldItemIn, func(b *protocol.Buffer) { b.PutI16(9) })
			},
		},
		{
			"a move out of the world",
			func(c *servertest.Client) {
				c.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
					b.PutF64(30_000_001) // past the world border
					b.PutF64(64)
					b.PutF64(0)
					b.PutU8(0)
				})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Sender")

			tc.send(c)
			c.Settle()
			if got := srv.PlayerCount(); got != 1 {
				t.Fatalf("player count = %d, want the packet not to cost its sender the connection", got)
			}
			c.Chat("still here")
			c.ExpectChat("Sender", "still here")
		})
	}
}

// TestABodyWithBytesPastItsFieldsDisconnectsItsSender sends a Set Held Item
// carrying one byte more than its short field: vanilla's decoder refuses a
// packet with bytes left after its read, and so does this server.
// vanilla: PacketDecoder.decode
func TestABodyWithBytesPastItsFieldsDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Sender")

	// The packet carries a short; the trailing 00 is a byte it does not declare.
	c.Send(protocol.IDPlayHeldItemIn, func(b *protocol.Buffer) {
		b.PutI16(1)
		b.PutU8(0)
	})
	want := wantExtraBytes("play", "set_carried_item", "ServerboundSetCarriedItemPacket", 1)
	if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, want) {
		t.Errorf("disconnect reason % X, want % X", reason, want)
	}
	servertest.WaitFor(t, srv, 100, func() bool { return srv.PlayerCount() == 0 })
}

// TestAPunchCarryingAHandDisconnectsItsSender sends a punch with a hand
// behind it: the packet has no fields, so the hand is a byte past its read.
// vanilla: ServerboundPunchPacket.STREAM_CODEC, PacketDecoder.decode
func TestAPunchCarryingAHandDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	srv, watcher := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	c := servertest.DialServer(t, srv).Join("Sender")
	entityID := watcher.ExpectPlayerAdded("Sender")

	// The bare punch is the whole packet, and is relayed as a swing.
	c.Send(protocol.IDPlayPunch, func(b *protocol.Buffer) {})
	swing := watcher.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation)
	if got := protocol.NewReader(swing).ReadVarInt(); got != entityID {
		t.Errorf("swinging entity = %d, want %d", got, entityID)
	}

	c.Send(protocol.IDPlayPunch, func(b *protocol.Buffer) { b.PutVarInt(protocol.HandMain) })
	want := wantExtraBytes("play", "punch", "ServerboundPunchPacket", 1)
	if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, want) {
		t.Errorf("disconnect reason % X, want % X", reason, want)
	}
	servertest.UntilTicks(t, srv, 20, "the sender to be dropped", func() bool { return srv.PlayerCount() == 1 })
}

// TestAPingRequestCutShortDisconnectsItsSender sends a Ping Request whose
// body is one byte: the timestamp it is answered with is a long, so a short
// one does not read and costs the sender the connection.
func TestAPingRequestCutShortDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	srv, sender := servertest.Join(t, servertest.BlockTestConfig(), "Sender")

	sender.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutU8(0) })
	expectDecodeDisconnect(t, srv, sender, "ping_request")
}

// TestANaNMoveKicksWithInvalidPlayerMovement sends a move whose x is NaN,
// the value vanilla ends the connection over.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
func TestANaNMoveKicksWithInvalidPlayerMovement(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Mover")

	c.MoveTo(math.NaN(), 64, 0.5, 0, 0)
	want := wantDisconnect("multiplayer.disconnect.invalid_player_movement")
	if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, want) {
		t.Errorf("disconnect reason % X, want % X", reason, want)
	}
	servertest.WaitFor(t, srv, 100, func() bool { return srv.PlayerCount() == 0 })
}

// TestAnInfiniteCoordinateIsClampedNotDropped sends a move with an infinite
// x: vanilla only NaN-checks a coordinate, so the value is clamped to the
// world border and the connection survives.
// vanilla: ServerGamePacketListenerImpl.clampHorizontal
func TestAnInfiniteCoordinateIsClampedNotDropped(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Mover")

	c.MoveTo(math.Inf(1), 64, 0.5, 0, 0)
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.PlayerPosition("Mover").X == 30_000_000
	})
	if got := srv.PlayerCount(); got != 1 {
		t.Fatalf("player count = %d, want the infinite move not to cost its sender the connection", got)
	}
	c.Chat("still here")
	c.ExpectChat("Mover", "still here")
}

// TestAConfigurationPacketWithBytesPastItsFieldsDisconnectsItsSender sends an
// Acknowledge Finish Configuration carrying one byte more than its empty body:
// the configuration phase holds its packets to the same trailing check the
// play phase does.
// vanilla: PacketDecoder.decode
func TestAConfigurationPacketWithBytesPastItsFieldsDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.BlockTestConfig())
	c.Login("Sender")

	// The server introduces itself and asks about data packs, as at join.
	c.Expect(protocol.IDConfigPluginMessage)
	k := protocol.NewReader(c.Expect(protocol.IDKnownPacks))
	for count := k.ReadVarInt(); count > 0; count-- {
		k.ReadString(32767)
		k.ReadString(32767)
		k.ReadString(32767)
	}
	c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString("minecraft")
		b.PutString("core")
		b.PutString(registry.Version)
	})
	c.Send(protocol.IDConfigFinishAck, func(b *protocol.Buffer) { b.PutU8(0) })

	reason, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDConfigDisconnect)
	if !ok {
		t.Fatal("no configuration disconnect for the extra byte")
	}
	want := wantExtraBytes("configuration", "finish_configuration", "ServerboundFinishConfigurationPacket", 1)
	if !bytes.Equal(reason, want) {
		t.Errorf("disconnect reason % X, want % X", reason, want)
	}
}

// TestAnUnknownConfigurationPacketIDDisconnectsItsSender sends a configuration
// id past the serverbound range: vanilla's IdDispatchCodec has no packet for it
// and ends the connection with its unknown-id reason, the way play does.
// vanilla: IdDispatchCodec.decode
func TestAnUnknownConfigurationPacketIDDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())

	sender := connect()
	sender.Login("Sender")

	// The server introduces itself before it reads anything back.
	sender.Expect(protocol.IDConfigPluginMessage)
	sender.Expect(protocol.IDKnownPacks)

	sender.Send(protocol.IDConfigAcceptCodeOfConduct+1, nil)
	var want protocol.Buffer
	want.PutText(protocol.Text{
		Translate: "disconnect.genericReason",
		With: []protocol.Text{{Text: fmt.Sprintf(
			"Internal Exception: io.netty.handler.codec.DecoderException: Received unknown packet id %d",
			protocol.IDConfigAcceptCodeOfConduct+1)}},
	})
	reason, ok := servertest.ReadUntil(t, sender, servertest.ExpectTimeout, protocol.IDConfigDisconnect)
	if !ok {
		t.Fatal("no configuration disconnect for an id past the serverbound range")
	}
	if !bytes.Equal(reason, want.Bytes()) {
		t.Errorf("disconnect reason % X, want % X", reason, want.Bytes())
	}
}

// TestAnUnknownPlayPacketIDDisconnectsItsSender sends a play id past the
// serverbound range: vanilla's IdDispatchCodec has no packet for it and ends
// the connection with its unknown-id reason.
// vanilla: IdDispatchCodec.decode
func TestAnUnknownPlayPacketIDDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Sender")

	c.Send(protocol.IDPlayCustomClickAction+1, nil)
	var b protocol.Buffer
	b.PutText(protocol.Text{
		Translate: "disconnect.genericReason",
		With:      []protocol.Text{{Text: "Internal Exception: io.netty.handler.codec.DecoderException: Received unknown packet id 69"}},
	})
	if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, b.Bytes()) {
		t.Errorf("disconnect reason % X, want % X", reason, b.Bytes())
	}
	servertest.WaitFor(t, srv, 100, func() bool { return srv.PlayerCount() == 0 })
}

// TestALegitimateCustomClickActionIsNotKicked sends Custom Click Action, play
// id 0x44, the last of the serverbound range: vanilla's decoder has the
// packet, so the top of the range is not an unknown id.
func TestALegitimateCustomClickActionIsNotKicked(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Sender")

	c.Send(protocol.IDPlayCustomClickAction, func(b *protocol.Buffer) {
		b.PutString("minecraft:test")
		b.PutVarInt(1) // one payload byte
		b.PutU8(0)     // an empty NBT tag, which the docs allow
	})
	if got := srv.PlayerCount(); got != 1 {
		t.Fatalf("player count = %d, want the top-of-range id not to cost its sender the connection", got)
	}
	c.Chat("still here")
	c.ExpectChat("Sender", "still here")
}

// TestANonBrandCustomPayloadIsIgnored sends a payload of bare bytes on a
// channel this server does not know: vanilla's DiscardedPayload has no length
// prefix, so the bytes are the rest of the payload rather than a length-prefixed
// array, and the sender stays connected.
// vanilla: DiscardedPayload
func TestANonBrandCustomPayloadIsIgnored(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Sender")

	c.Send(protocol.IDPlayPluginMessageIn, func(b *protocol.Buffer) {
		b.PutString("foo:bar")
		b.PutRaw([]byte("hello world"))
	})
	c.Settle()
	if got := srv.PlayerCount(); got != 1 {
		t.Fatalf("player count = %d, want the mod channel not to cost its sender the connection", got)
	}
	c.Chat("still here")
	c.ExpectChat("Sender", "still here")
}

// TestAnOversizedCustomPayloadDisconnectsItsSender sends more than the 32767
// bytes vanilla's DiscardedPayload allows on an unrecognized channel: the
// overrun fails the packet and ends that one connection.
func TestAnOversizedCustomPayloadDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Sender")

	c.Send(protocol.IDPlayPluginMessageIn, func(b *protocol.Buffer) {
		b.PutString("foo:bar")
		b.PutRaw(bytes.Repeat([]byte{0}, 32768))
	})
	expectDecodeDisconnect(t, srv, c, "custom_payload")
}

// The server sends no Cookie Request and no Login Plugin Request, so an answer
// to one is unsolicited wherever it arrives: vanilla disconnects its sender
// with multiplayer.disconnect.unexpected_query_response.
// vanilla: ServerCommonPacketListenerImpl.handleCookieResponse,
// ServerLoginPacketListenerImpl.handleCustomQueryPacket

// TestAConfigurationCookieResponseDisconnectsItsSender sends a Cookie Response
// while configuration runs: the response answers a request the server never
// made, so its sender is disconnected.
func TestAConfigurationCookieResponseDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())

	sender := connect()
	sender.Login("Sender")

	// The server introduces itself before it reads anything back.
	sender.Expect(protocol.IDConfigPluginMessage)
	sender.Expect(protocol.IDKnownPacks)

	sender.Send(protocol.IDConfigCookieResponse, func(b *protocol.Buffer) {
		b.PutString("minecraft:test")
		b.PutBool(false) // no payload
	})
	reason, ok := servertest.ReadUntil(t, sender, servertest.ExpectTimeout, protocol.IDConfigDisconnect)
	if !ok {
		t.Fatal("no configuration disconnect for an unsolicited cookie response")
	}
	if want := wantDisconnect("multiplayer.disconnect.unexpected_query_response"); !bytes.Equal(reason, want) {
		t.Errorf("disconnect reason % X, want % X", reason, want)
	}
}

// TestAPlayCookieResponseDisconnectsItsSender sends a Cookie Response once the
// player is in the world: the play phase refuses it the way configuration does.
func TestAPlayCookieResponseDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Sender")

	c.Send(protocol.IDPlayCookieResponse, func(b *protocol.Buffer) {
		b.PutString("minecraft:test")
		b.PutBool(false) // no payload
	})
	want := wantDisconnect("multiplayer.disconnect.unexpected_query_response")
	if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, want) {
		t.Errorf("disconnect reason % X, want % X", reason, want)
	}
	servertest.WaitFor(t, srv, 100, func() bool { return srv.PlayerCount() == 0 })
}

// TestALoginPluginResponseWhereAcknowledgedIsDueDisconnectsItsSender sends a
// Login Plugin Response where Login Acknowledged is due: the server never sent
// a Login Plugin Request, so the answer is unsolicited and the client is
// disconnected rather than left to time out.
func TestALoginPluginResponseWhereAcknowledgedIsDueDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.BlockTestConfig())

	c.Handshake(protocol.NextStateLogin)
	c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
		b.PutString("Sender")
		b.PutUUID(uuid.Nil)
	})
	threshold := protocol.NewReader(c.Expect(protocol.IDSetCompression)).ReadVarInt()
	c.Conn.EnableCompression(int(threshold))
	c.Expect(protocol.IDLoginSuccess)

	c.Send(protocol.IDLoginPluginResponse, func(b *protocol.Buffer) {
		b.PutVarInt(7)   // the query id the answer names
		b.PutBool(false) // not understood
	})
	want := wantLoginDisconnect("multiplayer.disconnect.unexpected_query_response")
	if reason := c.Expect(protocol.IDLoginDisconnect); !bytes.Equal(reason, want) {
		t.Errorf("login disconnect reason % X, want % X", reason, want)
	}
}

// TestALoginCookieResponseWhereStartIsDueDisconnectsItsSender sends a Cookie
// Response in place of Login Start: the server never sent a Cookie Request, so
// the answer is unsolicited wherever it arrives, including before the name.
func TestALoginCookieResponseWhereStartIsDueDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.BlockTestConfig())

	c.Handshake(protocol.NextStateLogin)
	c.Send(protocol.IDLoginCookieResponse, func(b *protocol.Buffer) {
		b.PutString("minecraft:test")
		b.PutBool(false) // no payload
	})
	want := wantLoginDisconnect("multiplayer.disconnect.unexpected_query_response")
	if reason := c.Expect(protocol.IDLoginDisconnect); !bytes.Equal(reason, want) {
		t.Errorf("login disconnect reason % X, want % X", reason, want)
	}
}

// TestALoginPluginResponseWhereStartIsDueDisconnectsItsSender sends a Login
// Plugin Response in place of Login Start, which the server never asked for.
func TestALoginPluginResponseWhereStartIsDueDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.BlockTestConfig())

	c.Handshake(protocol.NextStateLogin)
	c.Send(protocol.IDLoginPluginResponse, func(b *protocol.Buffer) {
		b.PutVarInt(7)   // the query id the answer names
		b.PutBool(false) // not understood
	})
	want := wantLoginDisconnect("multiplayer.disconnect.unexpected_query_response")
	if reason := c.Expect(protocol.IDLoginDisconnect); !bytes.Equal(reason, want) {
		t.Errorf("login disconnect reason % X, want % X", reason, want)
	}
}

// TestALoginCookieResponseWhereAcknowledgedIsDueDisconnectsItsSender sends a
// Cookie Response where Login Acknowledged is due, which vanilla refuses the
// same way it refuses the plugin response there.
func TestALoginCookieResponseWhereAcknowledgedIsDueDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.BlockTestConfig())

	c.Handshake(protocol.NextStateLogin)
	c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
		b.PutString("Sender")
		b.PutUUID(uuid.Nil)
	})
	threshold := protocol.NewReader(c.Expect(protocol.IDSetCompression)).ReadVarInt()
	c.Conn.EnableCompression(int(threshold))
	c.Expect(protocol.IDLoginSuccess)

	c.Send(protocol.IDLoginCookieResponse, func(b *protocol.Buffer) {
		b.PutString("minecraft:test")
		b.PutBool(false) // no payload
	})
	want := wantLoginDisconnect("multiplayer.disconnect.unexpected_query_response")
	if reason := c.Expect(protocol.IDLoginDisconnect); !bytes.Equal(reason, want) {
		t.Errorf("login disconnect reason % X, want % X", reason, want)
	}
}

// TestALoginCookieResponseWhereEncryptionIsDueDisconnectsItsSender sends a
// Cookie Response in place of Encryption Response in online mode, before the
// shared secret exists.
func TestALoginCookieResponseWhereEncryptionIsDueDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = true
	_, c := servertest.Dial(t, cfg)

	c.Handshake(protocol.NextStateLogin)
	c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
		b.PutString("Sender")
		b.PutUUID(uuid.Nil)
	})
	c.Expect(protocol.IDEncryptionRequest)

	c.Send(protocol.IDLoginCookieResponse, func(b *protocol.Buffer) {
		b.PutString("minecraft:test")
		b.PutBool(false) // no payload
	})
	want := wantLoginDisconnect("multiplayer.disconnect.unexpected_query_response")
	if reason := c.Expect(protocol.IDLoginDisconnect); !bytes.Equal(reason, want) {
		t.Errorf("login disconnect reason % X, want % X", reason, want)
	}
}

// TestALoginPluginResponseWhereEncryptionIsDueDisconnectsItsSender sends a
// Login Plugin Response in place of Encryption Response in online mode.
func TestALoginPluginResponseWhereEncryptionIsDueDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = true
	_, c := servertest.Dial(t, cfg)

	c.Handshake(protocol.NextStateLogin)
	c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
		b.PutString("Sender")
		b.PutUUID(uuid.Nil)
	})
	c.Expect(protocol.IDEncryptionRequest)

	c.Send(protocol.IDLoginPluginResponse, func(b *protocol.Buffer) {
		b.PutVarInt(7)   // the query id the answer names
		b.PutBool(false) // not understood
	})
	want := wantLoginDisconnect("multiplayer.disconnect.unexpected_query_response")
	if reason := c.Expect(protocol.IDLoginDisconnect); !bytes.Equal(reason, want) {
		t.Errorf("login disconnect reason % X, want % X", reason, want)
	}
}

// TestARepeatedKnownPacksReplyDisconnectsItsSender answers Select Known Packs
// twice: the negotiation is over after the first reply, so the second costs
// its sender the connection.
// vanilla: ServerConfigurationPacketListenerImpl.finishCurrentTask
func TestARepeatedKnownPacksReplyDisconnectsItsSender(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())

	sender := connect()
	sender.Login("Sender")
	sender.Expect(protocol.IDConfigPluginMessage)
	sender.Expect(protocol.IDKnownPacks)

	reply := func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString("minecraft")
		b.PutString("core")
		b.PutString(registry.Version)
	}
	sender.Send(protocol.IDKnownPacksIn, reply)
	if _, ok := servertest.ReadUntil(t, sender, servertest.ExpectTimeout, protocol.IDConfigFinish); !ok {
		t.Fatal("no finish for the first known-packs reply")
	}
	sender.Send(protocol.IDKnownPacksIn, reply)
	reason, ok := servertest.ReadUntil(t, sender, servertest.ExpectTimeout, protocol.IDConfigDisconnect)
	if !ok {
		t.Fatal("no configuration disconnect for a repeated known-packs reply")
	}
	var want protocol.Buffer
	want.PutText(protocol.Text{
		Translate: "disconnect.genericReason",
		With: []protocol.Text{{Text: "Internal Exception: java.lang.IllegalStateException: " +
			"Unexpected request for task finish, current task: join_world, requested: synchronize_registries"}},
	})
	if !bytes.Equal(reason, want.Bytes()) {
		t.Errorf("disconnect reason % X, want % X", reason, want.Bytes())
	}
}

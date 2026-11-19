package network

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
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

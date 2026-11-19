package entities

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// fixtureField is one entry of a display's, interaction's metadata.
type fixtureField struct {
	typ    int32
	byteV  uint8
	intV   int32
	floatV float32
	text   nbt.Tag
	slot   protocol.ItemStack
	vec    []float32
}

// awaitFixtureMeta reads the metadata of the entity, understanding the
// types a display carries.
func awaitFixtureMeta(c *servertest.Client, id int32) map[uint8]fixtureField {
	c.T.Helper()
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		if packetID != protocol.IDPlayEntityMetadata {
			continue
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			continue
		}
		fields := map[uint8]fixtureField{}
		for r.Err() == nil {
			index := r.ReadU8()
			if index == protocol.MetadataEnd {
				break
			}
			f := fixtureField{typ: r.ReadVarInt()}
			switch f.typ {
			case protocol.MetadataByte, protocol.MetadataBoolean:
				f.byteV = r.ReadU8()
			case protocol.MetadataVarInt, protocol.MetadataBlockState, 43: // 43: DYE_COLOR, a VarInt ID
				f.intV = r.ReadVarInt()
			case protocol.MetadataFloat:
				f.floatV = r.ReadF32()
			case protocol.MetadataSlot:
				f.slot, _ = r.ReadSlot()
			case 5, protocol.MetadataOptionalText:
				if f.typ == protocol.MetadataOptionalText && !r.ReadBool() {
					break
				}
				rest := r.Rest()
				tag, n, err := nbt.DecodeNetwork(rest)
				if err != nil {
					c.T.Fatalf("text component: %v", err)
				}
				f.text, r = tag, protocol.NewReader(rest[n:])
			case 39, 40:
				n := 3
				if f.typ == 40 {
					n = 4
				}
				for range n {
					f.vec = append(f.vec, r.ReadF32())
				}
			default:
				c.T.Fatalf("metadata field %d has type %d this reader does not know", index, f.typ)
			}
			fields[index] = f
		}
		return fields
	}
	c.T.Fatalf("no metadata for entity %d", id)
	return nil
}

// TestASummonedTextDisplayIsShownWithItsTag summons a text display whose tag
// sets the text, the line width, the shadow flag, the billboard and the
// scale: the spawn arrives where the command points and the metadata carries
// those fields and none of the defaults.
// vanilla: Display.TextDisplay.readAdditionalSaveData
func TestASummonedTextDisplayIsShownWithItsTag(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`summon minecraft:text_display 3.5 70 4.5 {text:'"hi"',line_width:100,shadow:1b,` +
		`billboard:"center",transformation:{translation:[0f,1f,0f],scale:[2f,2f,2f],` +
		`left_rotation:[0f,0f,0f,1f],right_rotation:[0f,0f,0f,1f]}}`)
	typ, ok := registry.EntityTypeID("minecraft:text_display")
	if !ok {
		t.Fatal("no text_display type")
	}
	spawn := c.AwaitSpawn(typ)
	if spawn.X != 3.5 || spawn.Y != 70 || spawn.Z != 4.5 {
		t.Errorf("text display at (%g, %g, %g), want (3.5, 70, 4.5)", spawn.X, spawn.Y, spawn.Z)
	}
	f := awaitFixtureMeta(c, spawn.EntityID)
	if text := f[23]; text.typ != 5 || fmt.Sprint(text.text) != `"hi"` {
		t.Errorf("text field = %+v, want the component \"hi\"", text)
	}
	if got := f[24]; got.typ != protocol.MetadataVarInt || got.intV != 100 {
		t.Errorf("line width = %+v, want 100", got)
	}
	if got := f[27]; got.byteV != 1 {
		t.Errorf("style flags = %+v, want the shadow bit", got)
	}
	if got := f[15]; got.byteV != 3 {
		t.Errorf("billboard = %+v, want center (3)", got)
	}
	if got := f[12]; len(got.vec) != 3 || got.vec[0] != 2 {
		t.Errorf("scale = %+v, want (2, 2, 2)", got)
	}
	if got := f[11]; len(got.vec) != 3 || got.vec[1] != 1 {
		t.Errorf("translation = %+v, want (0, 1, 0)", got)
	}
	for _, index := range []uint8{13, 14, 25, 26} {
		if _, sent := f[index]; sent {
			t.Errorf("field %d is a default and was sent", index)
		}
	}
	if got := srv.FixtureCount(); got != 1 {
		t.Errorf("%d fixtures stand, want 1", got)
	}
}

// TestASummonedInteractionCarriesItsSizeAndResponse checks the interaction's
// width, height and response metadata.
// vanilla: Interaction.defineSynchedData
func TestASummonedInteractionCarriesItsSizeAndResponse(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`summon minecraft:interaction 3.5 70 4.5 {width:2.5f,response:1b}`)
	typ, _ := registry.EntityTypeID("minecraft:interaction")
	spawn := c.AwaitSpawn(typ)
	f := awaitFixtureMeta(c, spawn.EntityID)
	if got := f[8]; got.typ != protocol.MetadataFloat || got.floatV != 2.5 {
		t.Errorf("width = %+v, want 2.5", got)
	}
	if _, sent := f[9]; sent {
		t.Error("the default height was sent")
	}
	if got := f[10]; got.typ != protocol.MetadataBoolean || got.byteV != 1 {
		t.Errorf("response = %+v, want true", got)
	}
}

// TestASummonedBlockAndItemDisplayShowTheirContent checks the block state and
// the item with its display context.
// vanilla: Display.BlockDisplay, Display.ItemDisplay
func TestASummonedBlockAndItemDisplayShowTheirContent(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`summon minecraft:block_display 3.5 70 4.5 {block_state:{Name:"minecraft:stone"}}`)
	blockType, _ := registry.EntityTypeID("minecraft:block_display")
	spawn := c.AwaitSpawn(blockType)
	stone, _ := registry.FindState("minecraft:stone", nil)
	if got := awaitFixtureMeta(c, spawn.EntityID)[23]; got.typ != protocol.MetadataBlockState || got.intV != int32(stone) {
		t.Errorf("block state = %+v, want stone %d", got, stone)
	}

	c.ChatCommand(`summon minecraft:item_display 3.5 70 4.5 {item:{id:"minecraft:diamond",count:1},item_display:"gui"}`)
	itemType, _ := registry.EntityTypeID("minecraft:item_display")
	spawn = c.AwaitSpawn(itemType)
	f := awaitFixtureMeta(c, spawn.EntityID)
	diamond, _ := registry.ItemID("minecraft:diamond")
	if got := f[23]; got.typ != protocol.MetadataSlot || got.slot.ItemID != diamond {
		t.Errorf("item = %+v, want a diamond", got)
	}
	if got := f[24]; got.byteV != 6 {
		t.Errorf("display context = %+v, want gui (6)", got)
	}
}

// TestAMarkerIsNeverShown checks that a marker stands in the world without a
// spawn packet.
// vanilla: EntityTypes.MARKER clientTrackingRange(0)
func TestAMarkerIsNeverShown(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Mapper", servertest.OpsListing(t, "Mapper"))

	srv.StopTick()
	c.ChatCommand(`summon minecraft:marker 3.5 70 4.5 {data:{a:1}}`)
	c.ChatCommand(`summon minecraft:text_display 3.5 70 4.5 {text:'"after"'}`)
	markerType, _ := registry.EntityTypeID("minecraft:marker")
	textType, _ := registry.EntityTypeID("minecraft:text_display")
	for range 5 {
		srv.RunTick()
	}
	// Packets arrive in order, so the marker's spawn would come first; the
	// text display's spawn must arrive for the wait to have proved anything.
	seen := false
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		if packetID != protocol.IDPlaySpawnEntity {
			continue
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		got := r.ReadVarInt()
		if got == markerType {
			t.Fatal("a marker was sent to the client")
		}
		if got == textType {
			seen = true
			break
		}
	}
	if !seen {
		t.Fatal("the text display was never spawned, so the marker's absence proves nothing")
	}
	if got := srv.FixtureCount(); got != 2 {
		t.Errorf("%d fixtures stand, want the marker and the display", got)
	}
}

// TestAFixtureSurvivesARestart checks that a display is written into its
// chunk's entity file with its tag and stood back up by a restarted server.
// vanilla: Display.addAdditionalSaveData
func TestAFixtureSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`summon minecraft:text_display 3.5 70 4.5 {text:'"kept"',line_width:77,Tags:["map"]}`)
	textType, _ := registry.EntityTypeID("minecraft:text_display")
	c.AwaitSpawn(textType)
	srv.FlushMobs()
	srv.Shutdown()

	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:text_display", func(compound nbt.Compound) {
		if text := compound["text"]; fmt.Sprint(text) != `"kept"` {
			t.Errorf("saved text = %v, want \"kept\"", text)
		}
		if width, _ := nbt.IntValue(compound["line_width"]); width != 77 {
			t.Errorf("saved line_width = %v, want 77", compound["line_width"])
		}
		if tags, ok := nbt.Get[nbt.List](compound, "Tags"); !ok || len(tags.Items) != 1 {
			t.Errorf("saved Tags = %v, want [map]", compound["Tags"])
		}
	})

	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Mapper")
	again.Configure()
	again.JoinCapturing("Mapper", map[int32]string{textType: "display"})
	if got := againSrv.FixtureCount(); got != 1 {
		t.Errorf("the restarted server holds %d fixtures, want 1", got)
	}
}

// TestAFixtureDoesNotSaveItsTeam loads a text display from a compound with
// Team "red" and finds no Team in the compound it is saved with.
// vanilla: Entity.load, Entity.saveWithoutId
func TestAFixtureDoesNotSaveItsTeam(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`team add red`)
	c.ChatCommand(`summon minecraft:text_display 3.5 70 4.5 {text:'"kept"',Team:"red"}`)
	textType, _ := registry.EntityTypeID("minecraft:text_display")
	c.AwaitSpawn(textType)
	srv.FlushMobs()
	srv.Shutdown()

	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:text_display", func(compound nbt.Compound) {
		if team, has := compound["Team"]; has {
			t.Errorf("saved Team = %v, want no Team key", team)
		}
	})
}

// TestADefaultDisplayReadsBackEveryField checks that /data get entity returns
// the fields vanilla always saves, not only the ones the summon gave.
// vanilla: Display.addAdditionalSaveData
func TestADefaultDisplayReadsBackEveryField(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`summon minecraft:text_display 3.5 70 4.5 {text:'"x"'}`)
	textType, _ := registry.EntityTypeID("minecraft:text_display")
	c.AwaitSpawn(textType)
	saved := srv.FixtureSavedTag()
	for _, key := range []string{"shadow_radius", "transformation", "billboard", "view_range", "line_width", "alignment"} {
		if _, ok := saved[key]; !ok {
			t.Errorf("saved tag lacks %s", key)
		}
	}
}

// TestADisplayRotationIsNormalized checks that an unnormalized rotation is
// the identity once normalized, so no metadata is sent for it.
// vanilla: ExtraCodecs.QUATERNIONF_COMPONENTS
func TestADisplayRotationIsNormalized(t *testing.T) {
	t.Parallel()
	tag := nbt.Compound{"transformation": nbt.Compound{"left_rotation": nbt.List{Elem: nbt.TypeFloat,
		Items: []nbt.Tag{nbt.Float(0), nbt.Float(0), nbt.Float(0), nbt.Float(2)}}}}
	if got := entity.FixtureMetadata(1, entity.FixtureTextDisplay, tag); got != nil {
		t.Errorf("an identity rotation sent metadata % x", got)
	}
	tag["brightness"] = nbt.Compound{"block": nbt.Int(5)}
	if got := entity.FixtureMetadata(1, entity.FixtureTextDisplay, tag); got != nil {
		t.Errorf("a brightness without sky sent metadata % x", got)
	}
}

// TestAnInteractionRecordsItsLastAttackerAndUser checks that a hit and a use
// each leave the player's UUID and a timestamp, saved with the entity.
// vanilla: Interaction.skipAttackInteraction, Interaction.interact
func TestAnInteractionRecordsItsLastAttackerAndUser(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`summon minecraft:interaction 3.5 70 4.5 {width:4f,height:4f}`)
	typ, _ := registry.EntityTypeID("minecraft:interaction")
	spawn := c.AwaitSpawn(typ)
	saved := srv.FixtureSavedTag()
	if _, ok := saved["attack"]; ok {
		t.Fatal("a fresh interaction has an attack record")
	}
	c.Attack(spawn.EntityID)
	c.UseOn(spawn.EntityID)
	for _, key := range []string{"attack", "interaction"} {
		var rec nbt.Compound
		servertest.UntilTicks(t, srv, 40, key+" record", func() bool {
			rec, _ = nbt.Get[nbt.Compound](srv.FixtureSavedTag(), key)
			return rec != nil
		})
		if _, ok := nbt.UUIDField(rec, "player"); !ok {
			t.Errorf("%s record has no player: %v", key, rec)
		}
		if _, ok := rec["timestamp"].(nbt.Long); !ok {
			t.Errorf("%s record has no timestamp: %v", key, rec)
		}
	}
}

// TestAMergeIntoADisplayTellsWhatChanged checks that /data merge writes the
// tag and sends the changed fields, including a reset to the default.
// vanilla: EntityDataAccessor.setData, SynchedEntityData.packDirty
func TestAMergeIntoADisplayTellsWhatChanged(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`summon minecraft:text_display 3.5 70 4.5 {text:'"x"',line_width:50}`)
	typ, _ := registry.EntityTypeID("minecraft:text_display")
	spawn := c.AwaitSpawn(typ)
	awaitFixtureMeta(c, spawn.EntityID)
	c.ChatCommand(`data merge entity @e[type=minecraft:text_display,limit=1] {line_width:200,Glowing:1b,CustomName:'"n"'}`)
	f := awaitFixtureMeta(c, spawn.EntityID)
	if got := f[24]; got.typ != protocol.MetadataVarInt || got.intV != 200 {
		t.Errorf("line width = %+v, want the default 200 told", got)
	}
	if got := f[0]; got.byteV&protocol.EntityFlagGlowing == 0 {
		t.Errorf("flags = %+v, want glowing", got)
	}
	if _, ok := f[2]; !ok {
		t.Error("the custom name was not sent")
	}
	if got, _ := nbt.Get[nbt.Byte](srv.FixtureSavedTag(), "Glowing"); got != 1 {
		t.Error("the merge did not reach the saved tag")
	}
}

// TestADisplayGivenAMatrixIsSentDecomposed checks that a 16-number
// transformation reaches the client as translation and scale.
// vanilla: Transformation.EXTENDED_CODEC
func TestADisplayGivenAMatrixIsSentDecomposed(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`summon minecraft:block_display 3.5 70 4.5 {transformation:[2f,0f,0f,1f, 0f,3f,0f,2f, 0f,0f,4f,3f, 0f,0f,0f,1f]}`)
	typ, _ := registry.EntityTypeID("minecraft:block_display")
	f := awaitFixtureMeta(c, c.AwaitSpawn(typ).EntityID)
	if got := f[11].vec; len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("translation = %v, want 1 2 3", got)
	}
	if got := f[12].vec; len(got) != 3 || got[0] != 2 || got[1] != 3 || got[2] != 4 {
		t.Errorf("scale = %v, want 2 3 4", got)
	}
}

// TestATeleportedFixtureIsToldItsNewPlace checks /tp on an interaction.
// vanilla: Entity.teleportTo
func TestATeleportedFixtureIsToldItsNewPlace(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`summon minecraft:interaction 3.5 70 4.5`)
	typ, _ := registry.EntityTypeID("minecraft:interaction")
	spawn := c.AwaitSpawn(typ)
	c.ChatCommand(`tp @e[type=minecraft:interaction,limit=1] 5.5 70 6.5`)
	told := false
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayTeleportEntity {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != spawn.EntityID {
			return false
		}
		told = true
		r.ReadVarInt() // position path
		if x := r.ReadF64(); x != 5.5 {
			t.Errorf("teleported to x %v, want 5.5", x)
		}
		return true
	})
	if !told {
		t.Fatal("no teleport was sent for the fixture")
	}
	pos, _ := nbt.Get[nbt.List](srv.FixtureSavedTag(), "Pos")
	if x, _ := pos.Items[0].(nbt.Double); x != 5.5 {
		t.Errorf("saved x = %v, want 5.5", x)
	}
}

// TestAMergeResendsADisplaysStartDelayEachTime checks that every merge into a
// display tells the interpolation start delay, even an unchanged one.
// vanilla: Display.setTransformationInterpolationDelay
func TestAMergeResendsADisplaysStartDelayEachTime(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`summon minecraft:text_display 3.5 70 4.5 {text:'"x"'}`)
	typ, _ := registry.EntityTypeID("minecraft:text_display")
	spawn := c.AwaitSpawn(typ)
	awaitFixtureMeta(c, spawn.EntityID)
	for _, scale := range []string{"1f,1f,1f", "2f,2f,2f"} {
		c.ChatCommand(`data merge entity @e[type=minecraft:text_display,limit=1] {start_interpolation:0,interpolation_duration:20,transformation:{translation:[0f,0f,0f],scale:[` + scale + `],left_rotation:[0f,0f,0f,1f],right_rotation:[0f,0f,0f,1f]}}`)
		f := awaitFixtureMeta(c, spawn.EntityID)
		if got, ok := f[8]; !ok || got.typ != protocol.MetadataVarInt || got.intV != 0 {
			t.Errorf("merge with scale %s: start delay = %+v (sent %v), want 0 told", scale, got, ok)
		}
	}
}

// TestATextDisplayResolvesItsSelector summons a text display whose text is a
// selector and merges one whose text is a score: the client is sent the
// resolved text.
// vanilla: Display.TextDisplay.readAdditionalSaveData
func TestATextDisplayResolvesItsSelector(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`summon minecraft:text_display ~ ~ ~ {text:{selector:"@p"}}`)
	typ, ok := registry.EntityTypeID("minecraft:text_display")
	if !ok {
		t.Fatal("no text_display type")
	}
	spawn := c.AwaitSpawn(typ)
	f := awaitFixtureMeta(c, spawn.EntityID)
	if got := flattenText(f[23].text); got != "Mapper" {
		t.Errorf("selector text = %q (%v), want Mapper", got, f[23].text)
	}

	c.ChatCommand(`scoreboard objectives add o dummy`)
	c.ChatCommand(`scoreboard players set Mapper o 7`)
	// A merge into a compound text keeps its keys, so merge into a plain one.
	c.ChatCommand(`summon minecraft:text_display ~ ~ ~ {text:'"a"',Tags:["b"]}`)
	spawn = c.AwaitSpawn(typ)
	awaitFixtureMeta(c, spawn.EntityID)
	c.ChatCommand(`data merge entity @n[type=text_display,tag=b] {text:{score:{name:"Mapper",objective:"o"}}}`)
	f = awaitFixtureMeta(c, spawn.EntityID)
	if got := flattenText(f[23].text); got != "7" {
		t.Errorf("score text = %q (%v), want 7", got, f[23].text)
	}
}

func flattenText(tag nbt.Tag) string {
	text, _ := protocol.TextOf(tag)
	return text.Plain()
}

// TestATextDisplayKeepsStylesTheModelHasNoFieldFor checks bold text and a
// keybind arrive as their components, not as an empty string.
// vanilla: Display.TextDisplay.DATA_TEXT_ID
func TestATextDisplayKeepsStylesTheModelHasNoFieldFor(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Mapper", servertest.OpsListing(t, "Mapper"))
	typ, _ := registry.EntityTypeID("minecraft:text_display")
	for _, tc := range []struct{ tag, want string }{
		{`{text:{text:"D",bold:true}}`, `map[bold:1 text:D]`},
		{`{text:{keybind:"key.jump"}}`, `map[keybind:key.jump]`},
	} {
		c.ChatCommand(`summon minecraft:text_display 3.5 70 4.5 ` + tc.tag)
		f := awaitFixtureMeta(c, c.AwaitSpawn(typ).EntityID)
		if got := fmt.Sprint(f[23].text); got != tc.want {
			t.Errorf("%s: text = %s, want %s", tc.tag, got, tc.want)
		}
	}
}

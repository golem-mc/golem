package commands

import (
	"encoding/hex"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAttributeFeedbackUsesTeamDisplayName checks that /attribute, /clear and
// /enchant name a player on a team by the display name the team decorates:
// the prefix and suffix around the name, under the /tell click and hover.
// vanilla: AttributeCommand, ClearInventoryCommands, EnchantCommand
func TestAttributeFeedbackUsesTeamDisplayName(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("team add red")
	c.ChatCommand(`team modify red prefix {"text":"[R] "}`)
	c.ChatCommand("team join red")
	servertest.AwaitSystemChatSaying(t, c, "commands.team.join.success.single")

	who := servertest.TeamPlayerDisplayComponent("Testificate", "[R] ", "")

	c.ChatCommand("attribute @s minecraft:max_health get")
	servertest.AwaitCommandFeedbackText(t, c, "commands.attribute.value.get.success",
		protocol.Text{Translate: "attribute.name.max_health"}, who, protocol.Text{Text: "20.0"})

	c.ChatCommand("clear @s")
	servertest.AwaitCommandErrorText(t, c, "clear.failed.single", who)

	// /enchant spells the display name out as plain text.
	c.ChatCommand("enchant @s sharpness 1")
	servertest.AwaitItemFailure(t, c, "commands.enchant.failed.itemless", protocol.Text{Text: "[R] Testificate"})
	c.Disconnect()
}

// TestAttributeFeedbackNamesEntityByCustomNameInTeam checks that a named mob
// on a team is named by its custom name between the team's prefix and
// suffix, with the UUID insertion and a hover carrying the bare custom name.
// vanilla: Entity.getDisplayName, PlayerTeam.formatNameForTeam
func TestAttributeFeedbackNamesEntityByCustomNameInTeam(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	id := uuid.MustParse("00000001-0000-0002-0000-000300000004")
	c.ChatCommand(`summon zombie ~ ~ ~ {NoAI:1b,UUID:[I;1,2,3,4],CustomName:"Bob"}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand("team add blue")
	c.ChatCommand(`team modify blue prefix {"text":"[B] "}`)
	c.ChatCommand("team join blue @e[type=zombie,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.team.join.success.single")

	bob := protocol.Text{Text: "Bob"}
	who := protocol.Text{
		Extra:     []protocol.Text{{Text: "[B] "}, bob, {}},
		Insertion: id.String(),
		HoverEntity: &protocol.HoverEntity{
			Name: bob,
			ID:   "minecraft:zombie",
			UUID: [4]uint32{1, 2, 3, 4},
		},
	}
	c.ChatCommand("attribute @e[type=zombie,limit=1] minecraft:max_health get")
	servertest.AwaitCommandFeedbackText(t, c, "commands.attribute.value.get.success",
		protocol.Text{Translate: "attribute.name.max_health"}, who, protocol.Text{Text: "20.0"})
	c.Disconnect()
}

// TestTagFeedbackNamesArmorStandByCustomName checks that an armor stand or a
// minecart is named by its custom name, not its type, with the hover carrying
// it too.
// vanilla: Entity.getName, Entity.getDisplayName
func TestTagFeedbackNamesArmorStandByCustomName(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"armor_stand", "minecart"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

			id := uuid.MustParse("00000001-0000-0002-0000-000300000004")
			c.ChatCommand("summon " + kind + ` ~ ~ ~ {UUID:[I;1,2,3,4],CustomName:"Bob"}`)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")

			bob := protocol.Text{Text: "Bob"}
			who := protocol.Text{
				Text:      "Bob",
				Insertion: id.String(),
				HoverEntity: &protocol.HoverEntity{
					Name: bob,
					ID:   "minecraft:" + kind,
					UUID: [4]uint32{1, 2, 3, 4},
				},
			}
			c.ChatCommand("tag @e[type=" + kind + ",name=Bob,limit=1] add named")
			servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
				protocol.Text{Text: "named"}, who)
			c.Disconnect()
		})
	}
}

// TestTagFeedbackNamesTNTAndItemByCustomName checks that a primed TNT, an
// item entity and the other bodies that are not mobs keep the CustomName they
// are summoned with: /tag names each by
// it, the name option selects both, /data reads it back, the spawn's metadata
// carries it and a /data write sends the new one.
// vanilla: Entity.load, Entity.getName, ItemEntity.getName
func TestTagFeedbackNamesTNTAndItemByCustomName(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	bodies := []struct{ kind, nbt string }{
		{"tnt", `fuse:1000`},
		{"item", `Item:{id:"minecraft:stone",count:1},PickupDelay:32767s`},
		{"arrow", `pickup:0b`},
		{"experience_orb", `Value:3s`},
		{"snowball", `Motion:[0d,0d,0d]`},
		{"falling_block", `BlockState:{id:"minecraft:sand"}`},
		{"firework_rocket", `LifeTime:1000`},
		{"area_effect_cloud", `Duration:1000`},
	}
	for _, b := range bodies {
		feedbackWith(t, srv, c, "summon "+b.kind+` ~ ~3 ~ {NoGravity:1b,CustomName:"Bob",`+b.nbt+"}", "commands.summon.success")
		with := feedbackWith(t, srv, c, "tag @e[type="+b.kind+",limit=1] add named", "commands.tag.add.success.single")
		if who := with[1]; who.Text != "Bob" || who.HoverEntity == nil ||
			who.HoverEntity.Name.Text != "Bob" || who.HoverEntity.ID != "minecraft:"+b.kind {
			t.Errorf("/tag named the %s %+v, want Bob hovering the %s", b.kind, who, b.kind)
		}
		with = feedbackWith(t, srv, c, "data get entity @e[type="+b.kind+",limit=1] CustomName", "commands.data.entity.query")
		if got := with[1].Plain(); got != `"Bob"` {
			t.Errorf("the %s's saved CustomName reads %s, want \"Bob\"", b.kind, got)
		}
	}
	with := feedbackWith(t, srv, c, "tag @e[name=Bob] add both", "commands.tag.add.success.multiple")
	if want := servertest.NumberText(int64(len(bodies))); !reflect.DeepEqual(with[1], want) {
		t.Errorf("@e[name=Bob] tagged %+v, want %+v", with[1], want)
	}

	// Each kind whose /data write sends metadata: the named spawn carries the
	// name and visibility, the rename the name field alone, or among all of
	// them for a thrown body, whose write resends every field.
	r := servertest.NewMetadataRecorder(t, srv, c)
	for _, k := range []struct {
		kind, nbt string
		typ       int32
		full      bool
	}{
		{"tnt", `fuse:1000`, protocol.EntityTypeTnt, false},
		{"item", `Item:{id:"minecraft:stone",count:1},PickupDelay:32767s`, protocol.EntityTypeItem, false},
		{"experience_orb", `Value:3s`, protocol.EntityTypeExperienceOrb, false},
		{"falling_block", `BlockState:{id:"minecraft:sand"}`, protocol.EntityTypeFallingBlock, false},
		{"snowball", `Motion:[0d,0d,0d]`, protocol.EntityTypeSnowball, true},
	} {
		packets := r.Run("summon " + k.kind + ` ~ ~3 ~ {NoGravity:1b,Tags:["shown_` + k.kind + `"],CustomName:"Carl",CustomNameVisible:1b,` + k.nbt + "}")
		id := servertest.SpawnedID(packets, k.typ)
		if id == 0 {
			t.Errorf("summoning the %s sent no spawn", k.kind)
			continue
		}
		spawn, write := "spawn "+k.kind, "write "+k.kind
		r.Record(spawn, id, packets)
		// The first payload is the spawn's; a burning fuse sends one a tick.
		if got := r.Got[spawn]; len(got) == 0 || !slices.Contains(servertest.MetadataIndices(t, got[0]), protocol.MetadataIndexCustomName) ||
			!slices.Contains(servertest.MetadataIndices(t, got[0]), protocol.MetadataIndexCustomNameVisible) {
			t.Errorf("the named %s spawned with metadata %v, want its name and visibility", k.kind, got)
		}
		r.Record(write, id, r.Run(`data merge entity @e[tag=shown_`+k.kind+`,limit=1] {CustomName:"Dan"}`))
		renames := 0
		for _, payload := range r.Got[write] {
			indices := servertest.MetadataIndices(t, payload)
			if slices.Equal(indices, []uint8{protocol.MetadataIndexCustomName}) ||
				k.full && slices.Contains(indices, protocol.MetadataIndexCustomName) && strings.Contains(payload, hex.EncodeToString([]byte("Dan"))) {
				renames++
			}
		}
		if renames != 1 {
			t.Errorf("the %s's rename was told in %d payloads of %v, want one carrying the new name", k.kind, renames, r.Got[write])
		}
	}
	c.Disconnect()
}

// feedbackWith runs command on the held loop and returns the arguments of
// the feedback translated by key, failing if none arrives within 5 ticks.
func feedbackWith(t *testing.T, srv *server.Server, c *servertest.Client, command, key string) []protocol.Text {
	t.Helper()
	c.ChatCommand(command)
	var with []protocol.Text
	c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySystemChat {
			return false
		}
		text, ok := protocol.ReadText(protocol.NewReader(data))
		if !ok || text.Translate != key || len(text.With) == 0 {
			return false
		}
		with = text.With
		return true
	})
	if with == nil {
		t.Fatalf("no %s feedback for %q", key, command)
	}
	return with
}

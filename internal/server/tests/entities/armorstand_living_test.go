package entities

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const livingStand = "@e[type=armor_stand,limit=1]"

func standCommandText(t *testing.T, c *servertest.Client, cmd, key string) protocol.Text {
	t.Helper()
	c.ChatCommand(cmd)
	var reply protocol.Text
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySystemChat {
			reply, _ = protocol.ReadText(protocol.NewReader(data))
		}
	})
	if !namesTranslation(reply, key) {
		t.Fatalf("%s returned %q, want %s", cmd, reply.Plain(), key)
	}
	return reply
}

func standLivingData(t *testing.T, c *servertest.Client, path string) string {
	t.Helper()
	return standCommandText(t, c, "data get entity "+livingStand+" "+path, "commands.data.entity.query").With[1].Plain()
}

func checkStandScale(t *testing.T, c *servertest.Client, standID int32, want float64) {
	t.Helper()
	var got float64
	var found bool
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayUpdateAttributes && protocol.NewReader(data).ReadVarInt() == standID {
			_, attrs := servertest.ReadAttributes(t, data)
			if scale, ok := attrs[int32(registry.AttributeScale)]; ok {
				got, found = scale.Base, true
			}
		}
	})
	if !found || got != want {
		t.Errorf("stand scale update = %g, sent %v, want %g", got, found, want)
	}
}

func TestArmorStandLivingAttributes(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "StandScale")
	c.ChatCommand(`summon armor_stand 2.5 64 2.5 {attributes:[{id:"minecraft:scale",base:2.0d}]}`)
	spawn := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySpawnEntity))
	standID := spawn.ReadVarInt()
	spawn.ReadUUID()
	if got := spawn.ReadVarInt(); got != protocol.EntityTypeArmorStand {
		t.Fatalf("spawned type %d, want armor stand", got)
	}
	checkStandScale(t, c, standID, 2)
	if got := standLivingData(t, c, "attributes[{id:\"minecraft:scale\"}].base"); got != "2.0d" {
		t.Errorf("saved scale = %s, want 2.0d", got)
	}
	if got := standCommandText(t, c, "attribute "+livingStand+" minecraft:step_height base get", "commands.attribute.base_value.get.success").With[2].Plain(); got != "0" && got != "0.0" {
		t.Errorf("step height = %s, want 0", got)
	}
	const highSelector = "@e[type=armor_stand,x=2,y=68,z=2,dx=0,dy=0,dz=0]"
	if selects(c, srv, highSelector, "tooHigh") {
		t.Error("scale 2 stand intersects the block above its head")
	}
	standCommandText(t, c, "attribute "+livingStand+" minecraft:scale base set 3", "commands.attribute.base_value.set.success")
	servertest.RunTicks(t, srv, 1)
	checkStandScale(t, c, standID, 3)
	if !selects(c, srv, highSelector, "nowTallEnough") {
		t.Error("scale 3 stand does not intersect the block its head now reaches")
	}
}

func TestArmorStandGlowingExpires(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "StandEffects")
	c.ChatCommand(`summon armor_stand 2.5 64 2.5 {NoGravity:1b}`)
	stand := c.ExpectArmorStandSpawn()
	standCommandText(t, c, "effect give "+livingStand+" minecraft:glowing 1", "commands.effect.give.success.single")
	servertest.RunTicks(t, srv, 1)
	checkStandFlags(t, c, stand.EntityID, protocol.EntityFlagGlowing)
	servertest.RunTicks(t, srv, 18)
	if got := standLivingData(t, c, `active_effects[{id:"minecraft:glowing"}].duration`); got != "1" {
		t.Errorf("after 19 ticks glowing duration = %s, want 1", got)
	}
	// The tick the effect expires in steps after tracking; the next tells it.
	servertest.RunTicks(t, srv, 2)
	checkStandFlags(t, c, stand.EntityID, 0)
	standCommandText(t, c, "data get entity "+livingStand+" active_effects", "arguments.nbtpath.nothing_found")
}

func checkStandFlags(t *testing.T, c *servertest.Client, standID int32, want uint8) {
	t.Helper()
	var got *servertest.MetaEntry
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != standID {
			return
		}
		entries, ok := servertest.ReadMetadata(r)
		if !ok {
			t.Fatal("invalid armor stand metadata")
		}
		if flags := servertest.MetaOf(entries, protocol.MetadataIndexEntityFlags); flags != nil {
			got = flags
		}
	})
	if got == nil || got.ByteVal != want {
		t.Fatalf("stand flag update = %+v, want %#x", got, want)
	}
}

func TestArmorStandInvisibilityEffectKeepsItsSavedVisibility(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "StandVisibility")
	c.ChatCommand(`summon armor_stand 2.5 64 2.5 {active_effects:[{id:"minecraft:invisibility",duration:200},{id:"minecraft:glowing",duration:200}]}`)
	stand := c.ExpectArmorStandSpawn()
	if stand.Flags != protocol.EntityFlagGlowing {
		t.Errorf("spawn flags = %#x, want glowing with the stand still visible", stand.Flags)
	}
	servertest.RunTicks(t, srv, 1)
	if got := standLivingData(t, c, `active_effects[{id:"minecraft:invisibility"}].duration`); got != "199" {
		t.Errorf("saved invisibility duration = %s, want 199", got)
	}
	standCommandText(t, c, "effect clear "+livingStand, "commands.effect.clear.everything.success.single")
	servertest.RunTicks(t, srv, 1)
	checkStandFlags(t, c, stand.EntityID, 0)
}

func TestArmorStandIgnoresSplashAndLingeringEffects(t *testing.T) {
	for _, potion := range []string{"splash_potion", "lingering_potion"} {
		t.Run(potion, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "StandPotion")
			c.ChatCommand(`summon armor_stand 3.5 64 3.5`)
			c.ExpectArmorStandSpawn()
			c.ChatCommand(`summon mannequin 4.5 64 3.5 {immovable:1b}`)
			awaitMannequin(t, c, srv)
			c.ChatCommand(fmt.Sprintf(`summon %s 3.5 65 3.5 {Motion:[0d,-1d,0d],Item:{id:"minecraft:%s",count:1,components:{"minecraft:potion_contents":{custom_effects:[{id:"minecraft:glowing",duration:200}]}}}}`, potion, potion))
			c.Settle()
			servertest.RunTicks(t, srv, 20)
			if got := dataOfMannequin(t, c, srv, "active_effects"); !strings.Contains(got, "minecraft:glowing") {
				t.Errorf("nearby mannequin effects = %s, want glowing", got)
			}
			standCommandText(t, c, "data get entity "+livingStand+" active_effects", "arguments.nbtpath.nothing_found")
		})
	}
}

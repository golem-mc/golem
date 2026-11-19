package entities

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestMannequinLivingEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, setup, tag, command, path, want string
		ticks                                 int
	}{
		{name: "poison", command: "effect give @e[type=mannequin] poison 10 1", path: "Health", want: "12.0f", ticks: 100},
		{name: "lava", setup: "setblock 3 64 3 lava", path: "Health", want: "12.0f", ticks: 20},
		{name: "burn", tag: "Fire:100s", path: "Health", want: "19.0f", ticks: 1},
		{name: "air", setup: "fill 3 64 3 3 66 3 water", path: "Air", want: "200s", ticks: 100},
		{name: "drown", setup: "fill 3 64 3 3 66 3 water", path: "Health", want: "18.0f", ticks: 320},
		{name: "water-breathing", setup: "fill 3 64 3 3 66 3 water", command: "effect give @e[type=mannequin] water_breathing 60", path: "Air", want: "300s", ticks: 1},
		{name: "fire-resistance", setup: "setblock 3 64 3 lava", command: "effect give @e[type=mannequin] fire_resistance 60", path: "Health", want: "20.0f", ticks: 1},
		{name: "wither", command: "effect give @e[type=mannequin] wither 10 1", path: "Health", want: "17.0f", ticks: 60},
		{name: "regeneration", tag: "Health:10f", command: "effect give @e[type=mannequin] regeneration 10", path: "Health", want: "12.0f", ticks: 100},
		{name: "instant-health", tag: "Health:10f", command: "effect give @e[type=mannequin] instant_health", path: "Health", want: "14.0f", ticks: 1},
		{name: "instant-damage", command: "effect give @e[type=mannequin] instant_damage", path: "Health", want: "14.0f", ticks: 1},
		{name: "absorption", tag: "Fire:100s", command: "effect give @e[type=mannequin] absorption 10", path: "Health", want: "20.0f", ticks: 1},
		{name: "resistance", tag: "Fire:100s", command: "effect give @e[type=mannequin] resistance 10 4", path: "Health", want: "20.0f", ticks: 1},
		{name: "water-extinguishes", setup: "fill 3 64 3 3 66 3 water", tag: "Fire:100s", path: "Fire", want: "0s", ticks: 1},
		{name: "freeze", setup: "fill 3 64 3 3 65 3 powder_snow", path: "Health", want: "19.0f", ticks: 160},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "Environment")
			if tc.setup != "" {
				c.ChatCommand(tc.setup)
			}
			tag := "{immovable:1b"
			if tc.tag != "" {
				tag += "," + tc.tag
			}
			tag += "}"
			c.ChatCommand("summon mannequin 3.5 64 3.5 " + tag)
			awaitMannequin(t, c, srv)
			if tc.command != "" {
				c.ChatCommand(tc.command)
			}
			c.Settle()
			servertest.RunTicks(t, srv, tc.ticks)
			if got := dataOfMannequin(t, c, srv, tc.path); got != tc.want {
				t.Errorf("after %d ticks %s = %s, want %s", tc.ticks, tc.path, got, tc.want)
			}
		})
	}
}

func TestMannequinEffectMetadataAndSavedState(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "EffectWatcher")
	c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b}")
	id := awaitMannequin(t, c, srv).id
	given := c.Give("effect give @e[type=mannequin] poison 10 1", id)
	if !slices.Equal(given.Particles[id], servertest.PoisonParticles) {
		t.Fatalf("poison particles = %v", given.Particles[id])
	}
	servertest.RunTicks(t, srv, 100)
	saved := dataOfMannequin(t, c, srv, "active_effects")
	tag, err := nbt.ParseSNBT("{active_effects:" + saved + "}")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := nbt.Get[nbt.List](tag, "active_effects")
	effects := effect.Decode(list)
	if active := effects.All(); len(active) != 1 || active[0].Duration != 100 || active[0].Amplifier != 1 {
		t.Fatalf("saved poison = %s", saved)
	}
	cleared := c.Give("effect clear @e[type=mannequin] poison", id)
	if particles, ok := cleared.Particles[id]; !ok || len(particles) != 0 {
		t.Fatalf("cleared particles = %v, sent %v", particles, ok)
	}
	c.ChatCommand(`data merge entity @e[type=mannequin,limit=1] {active_effects:[{id:"minecraft:poison",duration:200,amplifier:1b}],Health:20f}`)
	c.Settle()
	servertest.RunTicks(t, srv, 100)
	if got := dataOfMannequin(t, c, srv, "Health"); got != "12.0f" {
		t.Fatalf("reloaded poison left Health %s", got)
	}
}

func TestMannequinEnvironmentReachesItsWatcher(t *testing.T) {
	for _, tc := range []struct {
		name, setup, tag string
		ticks            int
		index            uint8
		value            int32
	}{
		{"lava", "setblock 3 64 3 lava", "", 1, protocol.MetadataIndexEntityFlags, int32(protocol.EntityFlagOnFire)},
		{"air", "fill 3 64 3 3 66 3 water", "", 1, protocol.MetadataIndexAir, 299},
		{"drown", "fill 3 64 3 3 66 3 water", "Air:0s", 20, protocol.MetadataIndexLivingHealth, 18},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "BodyWatcher")
			c.ChatCommand(tc.setup)
			tag := "{immovable:1b"
			if tc.tag != "" {
				tag += "," + tc.tag
			}
			tag += "}"
			c.ChatCommand("summon mannequin 3.5 64 3.5 " + tag)
			id := awaitMannequin(t, c, srv).id
			c.Settle()
			servertest.RunTicks(t, srv, tc.ticks)
			seen, burst := false, false
			c.SettleAndCollect(func(packet int32, data []byte) {
				r := protocol.NewReader(data)
				if packet == protocol.IDPlayEntityEvent && r.ReadI32() == id && r.ReadU8() == protocol.EntityEventDrown {
					burst = true
				}
				if packet != protocol.IDPlayEntityMetadata || protocol.NewReader(data).ReadVarInt() != id {
					return
				}
				r = protocol.NewReader(data)
				r.ReadVarInt()
				meta, ok := servertest.ReadMetadata(r)
				if !ok {
					t.Fatal("invalid metadata")
				}
				if field := servertest.MetaOf(meta, tc.index); field != nil {
					switch tc.index {
					case protocol.MetadataIndexEntityFlags:
						seen = seen || int32(field.ByteVal)&tc.value != 0
					case protocol.MetadataIndexAir:
						seen = seen || field.VarintVal == tc.value
					case protocol.MetadataIndexLivingHealth:
						seen = seen || field.FloatVal == float32(tc.value)
					}
				}
			})
			if !seen {
				t.Errorf("no %s metadata", tc.name)
			}
			if tc.name == "drown" && !burst {
				t.Error("no drowning bubble event")
			}
		})
	}
}

func TestMannequinKeepsBurningAfterLavaContact(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "LavaBurn")
	c.ChatCommand("setblock 3 64 3 lava")
	c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b}")
	awaitMannequin(t, c, srv)
	c.Settle()
	servertest.RunTicks(t, srv, 18)
	c.ChatCommand("setblock 3 64 3 air")
	c.Settle()
	servertest.RunTicks(t, srv, 82)
	if got := dataOfMannequin(t, c, srv, "Health"); got != "8.0f" {
		t.Errorf("lava and subsequent burning left Health %s, want 8.0f", got)
	}
	if got := dataOfMannequin(t, c, srv, "Fire"); got != "218s" {
		t.Errorf("Fire = %s, want 218s after 82 ticks away from lava", got)
	}
}

func TestMannequinDamageThenHealingReachesWatcher(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "HealWatcher")
	c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b,Health:10f,Fire:20s}")
	id := awaitMannequin(t, c, srv).id
	c.ChatCommand("effect give @e[type=mannequin] regeneration 10")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	var last float32
	c.SettleAndCollect(func(packet int32, data []byte) {
		if packet != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return
		}
		meta, ok := servertest.ReadMetadata(r)
		if !ok {
			t.Fatal("invalid metadata")
		}
		if health := servertest.MetaOf(meta, protocol.MetadataIndexLivingHealth); health != nil {
			last = health.FloatVal
		}
	})
	if got := dataOfMannequin(t, c, srv, "Health"); got != "10.0f" || last != 10 {
		t.Fatalf("server health=%s, last health sent to watcher=%g; want 10 for both", got, last)
	}
}

func TestMannequinFireContact(t *testing.T) {
	for _, tc := range []struct{ name, block, fire, wantFire, wantHealth string }{
		{"negative", "fire", "-20s", "-19s", "19.0f"},
		{"negative-multiple-cells", "fire", "-20s", "-19s", "19.0f"},
		{"reaches-zero", "fire", "-1s", "159s", "19.0f"},
		{"ignites", "fire", "0s", "159s", "19.0f"},
		{"campfire", "campfire", "0s", "0s", "19.0f"},
		{"soul-campfire", "soul_campfire", "0s", "0s", "18.0f"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "FireContact")
			c.ChatCommand("setblock 3 64 3 " + tc.block)
			if tc.name == "negative-multiple-cells" {
				c.ChatCommand("setblock 3 65 3 fire")
			}
			c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b,Fire:" + tc.fire + "}")
			arrival := awaitMannequin(t, c, srv)
			flags := byte(0)
			if field := servertest.MetaOf(arrival.meta, protocol.MetadataIndexEntityFlags); field != nil {
				flags = field.ByteVal
			}
			c.Settle()
			servertest.RunTicks(t, srv, 1)
			c.SettleAndCollect(func(packet int32, data []byte) {
				if packet != protocol.IDPlayEntityMetadata {
					return
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() != arrival.id {
					return
				}
				meta, ok := servertest.ReadMetadata(r)
				if !ok {
					t.Fatal("invalid metadata")
				}
				if field := servertest.MetaOf(meta, protocol.MetadataIndexEntityFlags); field != nil {
					flags = field.ByteVal
				}
			})
			if burning := flags&protocol.EntityFlagOnFire != 0; burning != (tc.wantFire == "159s") {
				t.Errorf("burning flag=%v, Fire should be %s", burning, tc.wantFire)
			}
			if got := dataOfMannequin(t, c, srv, "Fire"); got != tc.wantFire {
				t.Errorf("Fire=%s, want %s", got, tc.wantFire)
			}
			if got := dataOfMannequin(t, c, srv, "Health"); got != tc.wantHealth {
				t.Errorf("Health=%s, want %s", got, tc.wantHealth)
			}
		})
	}
}

func TestMannequinBurningTimeAttribute(t *testing.T) {
	for _, tc := range []struct{ name, command, want string }{
		{"base", "attribute @e[type=mannequin,limit=1] burning_time base set 0", "0s"},
		{"command-modifier", "attribute @e[type=mannequin,limit=1] burning_time modifier add test:burn -1 add_value", "0s"},
		{"equipment", `item replace entity @e[type=mannequin,limit=1] armor.feet with leather_boots[attribute_modifiers=[{type:"minecraft:burning_time",id:"test:burn",amount:-1d,operation:"add_value",slot:"feet"}]]`, "0s"},
		{"enchantment", `item replace entity @e[type=mannequin,limit=1] armor.feet with leather_boots[enchantments={"minecraft:fire_protection":1}]`, "255s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "BurnAttribute")
			c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b}")
			awaitMannequin(t, c, srv)
			c.ChatCommand(tc.command)
			c.ChatCommand("setblock 3 64 3 lava")
			c.Settle()
			servertest.RunTicks(t, srv, 1)
			if got := dataOfMannequin(t, c, srv, "Fire"); got != tc.want {
				t.Fatalf("Fire=%s, want %s", got, tc.want)
			}
			c.ChatCommand("item replace entity @e[type=mannequin,limit=1] armor.feet with air")
			c.ChatCommand("data merge entity @e[type=mannequin,limit=1] {Fire:0s}")
			c.Settle()
			servertest.RunTicks(t, srv, 1)
			want := tc.want
			if tc.name == "equipment" || tc.name == "enchantment" {
				want = "300s"
			}
			if got := dataOfMannequin(t, c, srv, "Fire"); got != want {
				t.Fatalf("after removing equipment Fire=%s, want %s", got, want)
			}
		})
	}
}

func TestMannequinEquipmentOxygenAttribute(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "OxygenAttribute")
	c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b}")
	awaitMannequin(t, c, srv)
	c.ChatCommand("attribute @e[type=mannequin,limit=1] oxygen_bonus base set 5")
	for _, tc := range []struct{ item, want string }{
		{`leather_helmet[enchantments={"minecraft:respiration":3}]`, "8.0"},
		{`leather_helmet[attribute_modifiers=[{type:"minecraft:oxygen_bonus",id:"test:oxygen",amount:2d,operation:"add_value",slot:"head"}]]`, "7.0"},
		{"air", "5.0"},
	} {
		c.ChatCommand("item replace entity @e[type=mannequin,limit=1] armor.head with " + tc.item)
		c.Settle()
		servertest.RunTicks(t, srv, 1)
		c.ChatCommand("attribute @e[type=mannequin,limit=1] oxygen_bonus get")
		var got string
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySystemChat {
				return
			}
			text, ok := protocol.ReadText(protocol.NewReader(data))
			if ok && text.Translate == "commands.attribute.value.get.success" {
				got = text.With[len(text.With)-1].Text
			}
		})
		if got != tc.want {
			t.Errorf("%s: oxygen_bonus=%q, want %s", tc.item, got, tc.want)
		}
	}
}

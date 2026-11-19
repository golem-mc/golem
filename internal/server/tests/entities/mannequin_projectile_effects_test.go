package entities

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: Arrow.doPostHurtEffects, ThrownSplashPotion.onHitAsPotion,
// AreaEffectCloud.serverTick. These are the same shots used in the real client.
func TestMannequinProjectilePoison(t *testing.T) {
	for _, tc := range []struct {
		kind, position, motion, field, item, health string
		duration                                    int32
	}{
		{"arrow", "3.5 65 -0.5", "0d,0d,1.5d", "item", "tipped_arrow", "16.0f", 0},
		{"splash_potion", "3.5 65 3.5", "0d,-1d,0d", "Item", "splash_potion", "12.0f", 101},
		{"lingering_potion", "3.5 65 3.5", "0d,-1d,0d", "Item", "lingering_potion", "15.0f", 41},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "PotionWatcher")
			c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b}")
			id := awaitMannequin(t, c, srv).id
			gravity := ""
			if tc.kind == "arrow" {
				gravity = "NoGravity:1b,"
			}
			c.ChatCommand(fmt.Sprintf(`summon %s %s {%sMotion:[%s],%s:{id:"minecraft:%s",count:1,components:{"minecraft:potion_contents":{custom_effects:[{id:"minecraft:poison",duration:200,amplifier:1}]}}}}`, tc.kind, tc.position, gravity, tc.motion, tc.field, tc.item))
			c.Settle()
			servertest.RunTicks(t, srv, 100)
			var poisoned bool
			bodies := map[int32]bool{id: true}
			c.SettleAndCollect(func(packet int32, data []byte) {
				if packet == protocol.IDPlayEntityMetadata {
					if meta, ok := servertest.ReadBodyMetadata(c, data, bodies); ok && slices.Equal(meta.Particles, servertest.PoisonParticles) {
						poisoned = true
					}
				}
			})
			if got := dataOfMannequin(t, c, srv, "Health"); got != tc.health {
				t.Errorf("after 100 ticks Health = %s, want vanilla's %s", got, tc.health)
			}
			if !poisoned {
				t.Error("watcher never received the mannequin's poison particles")
			}
			if tc.duration > 0 {
				saved := dataOfMannequin(t, c, srv, "active_effects")
				tag, err := nbt.ParseSNBT("{active_effects:" + saved + "}")
				if err != nil {
					t.Fatal(err)
				}
				list, _ := nbt.Get[nbt.List](tag, "active_effects")
				set := effect.Decode(list)
				if active := set.All(); len(active) != 1 || active[0].Duration != tc.duration {
					t.Errorf("saved poison = %s, want duration %d", saved, tc.duration)
				}
			}
		})
	}
}

func TestMannequinPotionInstants(t *testing.T) {
	for _, source := range []struct {
		kind  string
		ticks int
		half  bool
	}{
		{"splash_potion", 1, false},
		{"area_effect_cloud", 5, true},
	} {
		for _, tc := range []struct {
			name, tag, effects, full, half string
		}{
			{"heal", "Health:10f", `{id:"minecraft:instant_health",duration:1}`, "14.0f", "12.0f"},
			{"harm", "Health:20f", `{id:"minecraft:instant_damage",duration:1}`, "14.0f", "17.0f"},
			{"harm-then-heal", "Health:10f", `{id:"minecraft:instant_damage",duration:1},{id:"minecraft:instant_health",duration:1}`, "8.0f", "9.0f"},
			{"absorb-then-harm", "Health:20f", `{id:"minecraft:absorption",duration:200},{id:"minecraft:instant_damage",duration:1}`, "18.0f", "20.0f"},
			{"harm-then-absorb", "Health:20f", `{id:"minecraft:instant_damage",duration:1},{id:"minecraft:absorption",duration:200}`, "14.0f", "17.0f"},
			{"health-boost-then-heal", "Health:20f", `{id:"minecraft:health_boost",duration:200},{id:"minecraft:instant_health",duration:1}`, "24.0f", "22.0f"},
			{"invulnerable", "Invulnerable:1b", `{id:"minecraft:instant_damage",duration:1}`, "20.0f", "20.0f"},
		} {
			t.Run(source.kind+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				srv, c := joinMannequinOp(t, "InstantWatcher")
				c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b," + tc.tag + "}")
				id := awaitMannequin(t, c, srv).id
				if source.half {
					c.ChatCommand(`summon area_effect_cloud 3.5 64 3.5 {WaitTime:0,Radius:1f,potion_duration_scale:1f,potion_contents:{custom_effects:[` + tc.effects + `]}}`)
				} else {
					c.ChatCommand(`summon splash_potion 3.5 65 3.5 {Motion:[0d,-1d,0d],Item:{id:"minecraft:splash_potion",count:1,components:{"minecraft:potion_contents":{custom_effects:[` + tc.effects + `]}}}}`)
				}
				c.Settle()
				servertest.RunTicks(t, srv, source.ticks)
				want := tc.full
				if source.half {
					want = tc.half
				}
				// Watcher health must end at the saved health even when a heal follows a hurt.
				var health float32
				c.SettleAndCollect(func(packet int32, data []byte) {
					if packet == protocol.IDPlayEntityMetadata {
						if meta, ok := servertest.ReadBodyMetadata(c, data, map[int32]bool{id: true}); ok && meta.HasHealth {
							health = meta.Health
						}
					}
				})
				if got := dataOfMannequin(t, c, srv, "Health"); got != want {
					t.Fatalf("Health = %s, want %s", got, want)
				}
				if want != "20.0f" && fmt.Sprintf("%.1ff", health) != want {
					t.Errorf("watcher health = %v, want %s", health, want)
				}
			})
		}
	}
}

func TestMannequinWaterSplashExtinguishes(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "DouseWatcher")
	c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b,Fire:100s}")
	awaitMannequin(t, c, srv)
	c.ChatCommand(`summon splash_potion 3.5 65 3.5 {Motion:[0d,-1d,0d],Item:{id:"minecraft:splash_potion",count:1,components:{"minecraft:potion_contents":{potion:"minecraft:water"}}}}`)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := dataOfMannequin(t, c, srv, "Fire"); got != "0s" {
		t.Errorf("Fire = %s, want extinguished", got)
	}
}

func TestMannequinRefusesArrowSideEffects(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "RefusedWatcher")
	c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b,Invulnerable:1b}")
	id := awaitMannequin(t, c, srv).id
	c.ChatCommand(`summon arrow 3.5 65 -0.5 {NoGravity:1b,Fire:100s,Motion:[0d,0d,1.5d],item:{id:"minecraft:tipped_arrow",count:1,components:{"minecraft:potion_contents":{potion:"minecraft:poison"}}}}`)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	if got := dataOfMannequin(t, c, srv, "Health"); got != "20.0f" {
		t.Errorf("refused arrow Health = %s", got)
	}
	if got := dataOfMannequin(t, c, srv, "Fire"); got != "0s" {
		t.Errorf("refused arrow Fire = %s", got)
	}
	cleared := c.Give("effect clear @e[type=mannequin] poison", id)
	text, ok := protocol.ReadText(protocol.NewReader(cleared.Chat))
	if !ok || !namesTranslation(text, "commands.effect.clear.specific.failed") {
		t.Error("refused arrow left poison on the mannequin")
	}
}

func TestMannequinPotionMultipleRecipients(t *testing.T) {
	for _, tc := range []struct {
		kind   string
		ticks  int
		health float32
	}{
		{"splash_potion", 1, 14},
		{"area_effect_cloud", 5, 12},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "CrowdWatcher")
			bodies := make(map[int32]bool)
			for range 4 {
				c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b,Health:10f}")
				bodies[awaitMannequin(t, c, srv).id] = true
			}
			if len(bodies) != 4 {
				t.Fatalf("got %d recipients, want 4", len(bodies))
			}
			if tc.kind == "area_effect_cloud" {
				c.ChatCommand(`summon area_effect_cloud 3.5 64 3.5 {WaitTime:0,Radius:3f,RadiusOnUse:0f,potion_contents:{custom_effects:[{id:"minecraft:instant_health",duration:1}]}}`)
			} else {
				c.ChatCommand(`summon splash_potion 3.5 65 3.5 {Motion:[0d,-1d,0d],Item:{id:"minecraft:splash_potion",count:1,components:{"minecraft:potion_contents":{custom_effects:[{id:"minecraft:instant_health",duration:1}]}}}}`)
			}
			c.Settle()
			servertest.RunTicks(t, srv, tc.ticks)
			health := make(map[int32]float32)
			c.SettleAndCollect(func(packet int32, data []byte) {
				if packet != protocol.IDPlayEntityMetadata {
					return
				}
				if meta, ok := servertest.ReadBodyMetadata(c, data, bodies); ok && meta.HasHealth {
					health[meta.EntityID] = meta.Health
				}
			})
			for id := range bodies {
				if got := health[id]; got != tc.health {
					t.Errorf("recipient %d health = %v, want %v", id, got, tc.health)
				}
			}
		})
	}
}

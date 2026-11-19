package items

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: SoundEvent.DIRECT_STREAM_CODEC, LivingEntity.onEquipItem,
// Consumable.emitParticlesAndSounds, PlaySoundConsumeEffect.apply
func TestComponentSoundHoldersReachPlayback(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"consume", "consume effect", "equip"} {
		for _, form := range []string{"inline known", "inline unknown", "registered"} {
			t.Run(action+"/"+form, func(t *testing.T) {
				t.Parallel()
				name := "minecraft:block.amethyst_block.chime"
				if form == "inline unknown" {
					name = "test:component_sound"
				}
				sound := fmt.Sprintf(`{sound_id:%q,range:32}`, name)
				if form == "registered" {
					sound = fmt.Sprintf(`%q`, name)
				}
				var item string
				switch action {
				case "consume":
					item = `stick[consumable={consume_seconds:0,sound:` + sound + `},food={nutrition:1,saturation:0,can_always_eat:true}]`
				case "consume effect":
					item = `stick[consumable={consume_seconds:0,on_consume_effects:[{type:"play_sound",sound:` + sound + `}]}]`
				case "equip":
					item = `stick[equippable={slot:"head",equip_sound:` + sound + `}]`
				}
				srv, w := givenWindowed(t, servertest.SurvivalTestConfig(), item)
				w.UseItemRightClicks(protocol.HandMain)
				if action != "equip" {
					w.Settle()
					servertest.RunTicks(t, srv, 1)
				}
				found := false
				w.SettleAndCollect(func(id int32, data []byte) {
					if id != protocol.IDPlaySoundEffect {
						return
					}
					packet := servertest.DecodePlaySoundEffect(t, data)
					wireID := packet.RegistryID + 1
					got := packet.Name
					if wireID != 0 {
						got, _ = servertest.SoundEventName(packet.RegistryID)
					}
					if got != name {
						return
					}
					found = true
					if form == "registered" {
						want, _ := registry.SoundEventID(name)
						if wireID != want+1 {
							t.Errorf("sound holder ID = %d, want %d", wireID, want+1)
						}
					} else if wireID != 0 || !packet.HasFixedRange || packet.FixedRange != 32 {
						t.Errorf("sound holder = (ID %d, range %v/%v), want inline range 32", wireID, packet.HasFixedRange, packet.FixedRange)
					}
				})
				if !found {
					t.Fatalf("no playback of %s", name)
				}
			})
		}
	}
}

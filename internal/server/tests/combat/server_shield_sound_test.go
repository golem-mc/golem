package combat

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BlocksAttacks.onBlocked, BlocksAttacks.disable, SoundEvent.DIRECT_STREAM_CODEC
func TestShieldSoundHoldersReachPlayback(t *testing.T) {
	t.Parallel()
	for _, form := range []string{"inline known", "inline unknown", "registered"} {
		t.Run(form, func(t *testing.T) {
			t.Parallel()
			names := []string{"minecraft:item.shield.block", "minecraft:item.shield.break"}
			if form == "inline unknown" {
				names = []string{"test:block", "test:disable"}
			}
			sounds := make([]string, 2)
			for i, name := range names {
				sounds[i] = fmt.Sprintf(`{sound_id:%q,range:32}`, name)
				if form == "registered" {
					sounds[i] = fmt.Sprintf(`%q`, name)
				}
			}
			cfg := servertest.HostileTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, connect := servertest.Start(t, cfg)
			servertest.FreezeUntilHeld(t, srv)
			blocker := connect().Join("Blocker")
			blocker.MoveToGround(0.5, 64, 0.5, true)
			srv.RunLine(`give Blocker shield[blocks_attacks={block_sound:` + sounds[0] + `,disabled_sound:` + sounds[1] + `}]`)
			blocker.Settle()
			blocker.UseItemRightClicks(protocol.HandMain)
			blocker.Settle()
			attacker := connect().Join("Axeman")
			attacker.MoveToGround(0.5, 64, 1.5, true)
			srv.GiveSlot("Axeman", player.HotbarSlot, servertest.ItemStack(t, "minecraft:iron_axe", 1))
			servertest.HoldTicks(t, srv)
			servertest.AwaitFullSwing(t, srv, "Axeman")
			blocker.Settle()
			attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(blocker.PlayerEntityID) })
			attacker.Settle()
			found := map[string]bool{}
			blocker.SettleAndCollect(func(id int32, data []byte) {
				if id != protocol.IDPlaySoundEffect {
					return
				}
				packet := servertest.DecodePlaySoundEffect(t, data)
				wireID := packet.RegistryID + 1
				name := packet.Name
				if wireID != 0 {
					name, _ = servertest.SoundEventName(packet.RegistryID)
				}
				if name != names[0] && name != names[1] {
					return
				}
				found[name] = true
				if form == "registered" {
					want, _ := registry.SoundEventID(name)
					if wireID != want+1 {
						t.Errorf("sound holder ID = %d, want %d", wireID, want+1)
					}
				} else if wireID != 0 || !packet.HasFixedRange || packet.FixedRange != 32 {
					t.Errorf("%s holder = (ID %d, range %v/%v), want inline range 32", name, wireID, packet.HasFixedRange, packet.FixedRange)
				}
			})
			for _, name := range names {
				if !found[name] {
					t.Errorf("no playback of %s", name)
				}
			}
		})
	}
}

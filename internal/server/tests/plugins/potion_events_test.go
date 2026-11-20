package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// potionOf is a stack of the item carrying the named brewing and nothing else.
func potionOf(t *testing.T, item, potion string) protocol.ItemStack {
	t.Helper()
	var value protocol.Buffer
	value.PutBool(true)
	value.PutVarInt(registry.MustPotion(potion))
	value.PutBool(false) // no custom colour
	value.PutVarInt(0)   // no custom effects
	value.PutBool(false) // no custom name
	return protocol.ItemStack{ItemID: servertest.MustItemID(t, item), Count: 1,
		Raw: []protocol.RawComponent{{Type: protocol.DataComponentPotionContents, Data: value.Bytes()}}}
}

// throwAtFeet has Splasher throw the stack at their own feet, Bystander two
// blocks east, and runs ticks until done holds or the potion has surely
// landed.
func throwAtFeet(t *testing.T, listen func(plugin.Server), item, potion string, done func() bool) *server.Server {
	t.Helper()
	s := servertest.NewTestServerWithPlugins(t, servertest.MobTestConfig(), []plugin.Registration{{Name: "potions", Plugin: &testPlugin{enable: func(h plugin.Server) error {
		listen(h)
		return nil
	}}}})
	a := servertest.DialServer(t, s)
	a.Login("Splasher")
	a.Configure()
	a.EnterWorldAs("Splasher")
	b := servertest.DialServer(t, s)
	b.Login("Bystander")
	b.Configure()
	b.EnterWorldAs("Bystander")
	b.MoveTo(2.0, 64, 0.5, 270, 0)
	a.Settle()
	b.Settle()
	s.GiveSlot("Splasher", 36, potionOf(t, item, potion))
	a.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(a, -90, 90)
	for range 100 {
		if done() {
			break
		}
		servertest.RunTicks(t, s, 1)
	}
	servertest.RunTicks(t, s, 3)
	return s
}

// vanilla: ThrownSplashPotion.onHitAsPotion
func TestPluginPotionSplashEvent(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"all", "spare-bystander", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var fired int
			var names []string
			srv := throwAtFeet(t, func(h plugin.Server) {
				plugin.Listen(h, func(e *plugin.PotionSplashEvent) {
					fired++
					if shooter, ok := e.Potion.Shooter(); !ok || shooter == nil || e.Item.Empty() {
						t.Errorf("shooter %v %v, item %v", shooter, ok, e.Item)
					}
					for l, scale := range e.Affected {
						p := l.(plugin.Player)
						names = append(names, p.Name())
						if scale <= 0 || scale > 1 {
							t.Errorf("%s intensity %v", p.Name(), scale)
						}
						if mode == "spare-bystander" && p.Name() == "Bystander" {
							e.Affected[l] = 0
						}
					}
					e.SetCancelled(mode == "cancel")
				})
			}, "minecraft:splash_potion", "minecraft:poison", func() bool { return fired > 0 })
			if fired != 1 || len(names) != 2 {
				t.Fatalf("fired %d times with %v", fired, names)
			}
			poisoned := func(name string) bool {
				_, _, ok := srv.PlayerEffect(name, "minecraft:poison")
				return ok
			}
			wantSplasher, wantBystander := mode != "cancel", mode == "all"
			if poisoned("Splasher") != wantSplasher || poisoned("Bystander") != wantBystander {
				t.Errorf("poisoned: Splasher %v (want %v), Bystander %v (want %v)", poisoned("Splasher"), wantSplasher, poisoned("Bystander"), wantBystander)
			}
		})
	}
}

// vanilla: ThrownLingeringPotion.onHitAsPotion
func TestPluginLingeringPotionSplashEvent(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		var fired int
		srv := throwAtFeet(t, func(h plugin.Server) {
			plugin.Listen(h, func(e *plugin.LingeringPotionSplashEvent) {
				fired++
				e.SetCancelled(cancel)
			})
		}, "minecraft:lingering_potion", "minecraft:poison", func() bool { return fired > 0 })
		if want := map[bool]int{false: 1, true: 0}[cancel]; fired != 1 || srv.CloudCount() != want {
			t.Errorf("cancel %v: fired %d, clouds %d, want %d", cancel, fired, srv.CloudCount(), want)
		}
	}
}

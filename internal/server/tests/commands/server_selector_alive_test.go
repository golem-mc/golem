package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// A dead body lingers in the mob book for the tick between its death and the
// mob pass that reports it, and a claimed item lingers until the pass that
// takes it out; both are already dead where a selector reads them. The tests
// hold the loop stopped so those windows stay open instead of racing a tick,
// and check that the alive filter the @e letter adds (vanilla EntitySelector
// filtering with Entity::isAlive) leaves the dead bodies out.

// TestKillSelectorSkipsADyingCow checks /kill @e[type=cow] against a cow that
// is out of health but still in the world: a second sweep finds nobody where
// the dying body would still be found.
func TestKillSelectorSkipsADyingCow(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// The loop stopped, the cow's death is not carried out of the book by
	// the mob pass, so the window the command reads it in stays open.
	srv.StopTick()
	cow := srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)
	cowUUID, ok := srv.MobUUID(cow)
	if !ok {
		t.Fatal("the cow has no UUID to be named by")
	}
	c.Settle()

	c.ChatCommand("kill @e[type=cow]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", mustUUID(t, cowUUID)))

	c.ChatCommand("kill @e[type=cow]")
	expectNotfoundEntity(t, c)

	c.Disconnect()
}

// TestExecuteAsSkipsADyingCow checks execute as @e[type=cow]: the dead body
// names no context, so the tail the fork would run says nothing.
func TestExecuteAsSkipsADyingCow(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	srv.StopTick()
	cow := srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)
	cowUUID, ok := srv.MobUUID(cow)
	if !ok {
		t.Fatal("the cow has no UUID to be named by")
	}
	c.Settle()

	c.ChatCommand("kill @e[type=cow]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", mustUUID(t, cowUUID)))

	c.ChatCommand("execute as @e[type=cow] run say hi")
	expectNoChat(t, c)

	c.Disconnect()
}

// TestKillSelectorSkipsAPickedUpItem checks /kill @e[type=item] against a
// stack a player has just picked up: the claimed body is Dead though still
// in the list, and the selector leaves it out.
func TestKillSelectorSkipsAPickedUpItem(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// Drive the item pass by hand until the player has claimed the stack:
	// the claimed body stays in the list, marked Dead, until the next pass
	// would take it out, so the command reads the dead body, not nothing.
	srv.StopTick()
	srv.SpawnItemEntity(0.5, 64.125, 0.5, "minecraft:stone", 3)
	for i := 0; i < 15 && srv.InventorySlot("Testificate", player.HotbarSlot).Count == 0; i++ {
		srv.TickEntities(1)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 3 {
		t.Fatalf("the pickup never landed the stack: the hotbar holds %+v", got)
	}

	c.ChatCommand("kill @e[type=item]")
	expectNotfoundEntity(t, c)

	c.Disconnect()
}

// expectNotfoundEntity reads past the noise the command's own doing left and
// checks the not-found refusal the selector answers a sweep that names nobody
// with.
func expectNotfoundEntity(t *testing.T, c *servertest.Client) {
	t.Helper()
	data := c.AwaitPacket(protocol.IDPlaySystemChat)
	if want := servertest.CommandErrorMessageForTest("argument.entity.notfound.entity"); !bytes.Equal(data, want) {
		t.Errorf("the empty sweep answered % X, want the not-found refusal % X", data, want)
	}
}

// expectNoChat fails when any chat reaches the client before the ping the
// helper sends answers, which is how a fork that should have named nobody is
// caught saying something.
func expectNoChat(t *testing.T, c *servertest.Client) {
	t.Helper()
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	for id, data := range c.Stream() {
		t.Helper()
		switch id {
		case protocol.IDPlayPingResponse:
			return
		case protocol.IDPlayDisguisedChat, protocol.IDPlaySystemChat:
			t.Fatalf("a chat arrived where silence was wanted: % X", data)
		}
	}
	t.Fatal("the ping was never answered")
}

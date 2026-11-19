package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The sign tests: the editor a placement opens, the Update Sign that writes
// it, the dye and the honeycomb that change it, and both sides of it. Each
// writes through the wire the way a client does, and reads what the server
// answers — above all the Block Entity Data a change is told as, which is
// how another client learns a sign's text.

// signActor joins a player in creative for the sign tests, where placing
// does not cost the held stack and the packets of a survival inventory are
// not what is under test.
func signActor(t *testing.T, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), name)
	return srv, c
}

// signSide reads one side of a sign out of the NBT a Block Entity Data
// packet carried: its four plain lines, the colour, the glow.
func signSide(t *testing.T, data nbt.Compound, side string) (messages [4]string, color string, glowing bool) {
	t.Helper()
	compound, ok := nbt.Get[nbt.Compound](data, side)
	if !ok {
		t.Fatalf("block entity NBT has no %s: %#v", side, data)
	}
	list, ok := nbt.Get[nbt.List](compound, "messages")
	if !ok || len(list.Items) != 4 {
		t.Fatalf("%s.messages = %#v, want four lines", side, compound["messages"])
	}
	for i, item := range list.Items {
		line, ok := item.(nbt.String)
		if !ok {
			t.Fatalf("%s message %d = %#v, want a plain string", side, i, item)
		}
		messages[i] = string(line)
	}
	if c, ok := nbt.Get[nbt.String](compound, "color"); ok {
		color = string(c)
	}
	if glow, ok := nbt.Get[nbt.Byte](compound, "has_glowing_text"); ok && glow != 0 {
		glowing = true
	}
	return messages, color, glowing
}

// signWaxed reads the wax byte out of the NBT a Block Entity Data packet
// carried for a sign.
func signWaxed(t *testing.T, data nbt.Compound) bool {
	t.Helper()
	waxed, ok := nbt.Get[nbt.Byte](data, "is_waxed")
	if !ok {
		t.Fatalf("block entity NBT has no is_waxed: %#v", data)
	}
	return waxed != 0
}

// seePacket is what pumpTicks watches for: one packet id, nothing else.
func seePacket(id int32) func(int32, []byte) bool {
	return func(got int32, _ []byte) bool { return got == id }
}

// placeSignFront stands a player south of the cell x, z holding a sign, and
// places it on the ground at (x, 64, z) facing them, the position every test
// in this file wants its signs at. Writing happens in the caller, through
// the editor this placement has opened.
func placeSignFront(c *servertest.Client, item string, x, z int32) {
	c.T.Helper()
	c.MoveTo(float64(x)+0.5, 64, float64(z)+1.5, 180, 0)
	servertest.GiveHeldItem(c, item)
	c.Settle()
	c.PlaceBlock(x, 63, z, protocol.FaceTop)
}

// TestPlacingASignOpensTheEditorAndUpdateSignWritesIt covers the whole
// editor round trip: a placed sign opens its front editor for the placer,
// the text the editor finishes with lands on the front with its §-codes
// stripped, and the change is broadcast to the world as the block entity's
// data, both sides default and unwaxed.
func TestPlacingASignOpensTheEditorAndUpdateSignWritesIt(t *testing.T) {
	t.Parallel()
	srv, c := signActor(t, "Writer")
	servertest.HoldTicks(t, srv)

	placeSignFront(c, "minecraft:oak_sign", 4, 7)
	x, y, z, front := c.AwaitOpenSignEditor()
	if x != 4 || y != 64 || z != 7 {
		t.Errorf("sign editor at (%d,%d,%d), want (4,64,7)", x, y, z)
	}
	if !front {
		t.Error("the placed sign's editor is for the back, want the front")
	}

	// Flush the placement's empty block entity data before writing the text.
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	c.SendSignUpdate(4, 64, 7, true, [4]string{"Hello", "world", "§6Gold", ""})
	_, _, _, beType, data := c.AwaitBlockEntityData()
	if beType != protocol.BlockEntitySign {
		t.Errorf("sign block entity type = %d, want %d", beType, protocol.BlockEntitySign)
	}
	if _, has := data["id"]; has {
		t.Errorf("sign update tag carries an id: %#v", data)
	}
	messages, color, glowing := signSide(t, data, "front_text")
	if want := [4]string{"Hello", "world", "Gold", ""}; messages != want {
		t.Errorf("front text = %q, want the §-codes stripped to %q", messages, want)
	}
	if color != "black" {
		t.Errorf("front text colour = %q, want black", color)
	}
	if glowing {
		t.Error("fresh sign text glows, want it plain")
	}
	if back, _, _ := signSide(t, data, "back_text"); back != [4]string{} {
		t.Errorf("back text = %q, want it empty", back)
	}
	if signWaxed(t, data) {
		t.Error("fresh sign is waxed, want it editable")
	}
}

// TestTheSignsBackSideEditsFromTheBack walks around a placed sign and clicks
// its back: the editor that opens is the back's, and what it writes lands on
// the back alone, the front's text left as it was.
func TestTheSignsBackSideEditsFromTheBack(t *testing.T) {
	t.Parallel()
	_, c := signActor(t, "Writer")

	placeSignFront(c, "minecraft:oak_sign", 4, 7)
	if _, _, _, front := c.AwaitOpenSignEditor(); !front {
		t.Fatal("the placed sign's editor is for the back")
	}
	c.SendSignUpdate(4, 64, 7, true, [4]string{"One", "Two", "Three", "Four"})
	c.AwaitBlockEntityData()

	// North of the sign, the back faces the clicker.
	c.MoveTo(4.5, 64, 5.5, 0, 0)
	c.Settle()
	c.UseBlock(4, 64, 7)
	if _, _, _, front := c.AwaitOpenSignEditor(); front {
		t.Error("the back-side editor is for the front")
	}

	c.SendSignUpdate(4, 64, 7, false, [4]string{"Back", "of", "the", "sign"})
	_, _, _, _, data := c.AwaitBlockEntityData()
	if back, _, _ := signSide(t, data, "back_text"); back != [4]string{"Back", "of", "the", "sign"} {
		t.Errorf("back text = %q, want %q", back, [4]string{"Back", "of", "the", "sign"})
	}
	if frontMessages, _, _ := signSide(t, data, "front_text"); frontMessages != [4]string{"One", "Two", "Three", "Four"} {
		t.Errorf("front text = %q, want it left alone", frontMessages)
	}
}

// TestAWaxedSignRefusesAnUpdate waxes a sign with a honeycomb — the wax
// applying even to the player holding the sign's edit — and then refuses
// their update: the wobble sound and no editor are all a waxed sign ever
// answers.
func TestAWaxedSignRefusesAnUpdate(t *testing.T) {
	t.Parallel()
	srv, c := signActor(t, "Waxer")

	placeSignFront(c, "minecraft:oak_sign", 4, 7)
	c.AwaitOpenSignEditor()

	// The honeycomb waxes while the edit is the waxer's own.
	servertest.GiveHeldItem(c, "minecraft:honeycomb")
	c.PlaceBlock(4, 64, 7, protocol.FaceTop)
	_, _, _, beType, data := c.AwaitBlockEntityData()
	if beType != protocol.BlockEntitySign {
		t.Fatalf("wax block entity type = %d, want %d", beType, protocol.BlockEntitySign)
	}
	if !signWaxed(t, data) {
		t.Fatal("the waxed sign's block entity is not waxed")
	}
	if _, has := data["id"]; has {
		t.Errorf("the sign's update tag carries an id: %#v", data)
	}
	if _, _, _, d := c.AwaitWorldEvent(protocol.WorldEventWaxOn); d != 0 {
		t.Errorf("wax event data = %d, want 0", d)
	}
	if name := c.AwaitSoundName(); name != "minecraft:item.honeycomb.wax_on" {
		t.Errorf("wax sound = %q, want item.honeycomb.wax_on", name)
	}

	// The flushes of the sign's placement and its wax are still to come:
	// their block entity data is read past before the update is sent.
	c.PumpTicks(srv, 3, func(int32, []byte) bool { return false })

	// The update of a waxed sign is dropped, editor or none.
	c.SendSignUpdate(4, 64, 7, true, [4]string{"Nope", "Nope", "Nope", "Nope"})
	if c.PumpTicks(srv, 3, seePacket(protocol.IDPlayBlockEntityData)) {
		t.Error("a waxed sign answered an update with block entity data")
	}

	// A bare hand on the waxed sign wobbles and opens nothing.
	c.UseBlock(4, 64, 7)
	if name := c.AwaitSoundName(); name != "minecraft:block.sign.waxed_interact_fail" {
		t.Errorf("waxed sign click sound = %q, want the wobble", name)
	}
	if c.PumpTicks(srv, 3, seePacket(protocol.IDPlayOpenSignEditor)) {
		t.Error("a waxed sign opened an editor")
	}
}

// TestApplyingGlowDyeAndWaxToASign runs the items that change what a sign
// is over the side facing the clicker: a dye colours it, a glow ink sac
// makes it glow, an ink sac takes the glow back off, a honeycomb waxes it —
// each answered with the block entity's data and its own sound.
func TestApplyingGlowDyeAndWaxToASign(t *testing.T) {
	t.Parallel()
	srv, c := signActor(t, "Painter")
	servertest.HoldTicks(t, srv)

	// The dye, the glow ink and the ink all ask for text to be there.
	placeSignFront(c, "minecraft:oak_sign", 4, 7)
	c.AwaitOpenSignEditor()
	// Drain the placement snapshot before reading the item updates.
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	c.SendSignUpdate(4, 64, 7, true, [4]string{"Some", "text", "on", "it"})
	c.AwaitBlockEntityData()

	servertest.GiveHeldItem(c, "minecraft:red_dye")
	c.PlaceBlock(4, 64, 7, protocol.FaceTop)
	_, _, _, _, data := c.AwaitBlockEntityData()
	if messages, color, _ := signSide(t, data, "front_text"); messages != [4]string{"Some", "text", "on", "it"} {
		t.Errorf("dye rewrote the text: %q", messages)
	} else if color != "red" {
		t.Errorf("dyed front text colour = %q, want red", color)
	}
	if name := c.AwaitSoundName(); name != "minecraft:item.dye.use" {
		t.Errorf("dye sound = %q, want the dye use", name)
	}

	servertest.GiveHeldItem(c, "minecraft:glow_ink_sac")
	c.PlaceBlock(4, 64, 7, protocol.FaceTop)
	_, _, _, _, data = c.AwaitBlockEntityData()
	if _, _, glowing := signSide(t, data, "front_text"); !glowing {
		t.Error("glow ink did not make the front glow")
	}
	if name := c.AwaitSoundName(); name != "minecraft:item.glow_ink_sac.use" {
		t.Errorf("glow ink sound = %q, want the glow ink use", name)
	}

	servertest.GiveHeldItem(c, "minecraft:ink_sac")
	c.PlaceBlock(4, 64, 7, protocol.FaceTop)
	_, _, _, _, data = c.AwaitBlockEntityData()
	if _, _, glowing := signSide(t, data, "front_text"); glowing {
		t.Error("ink sac did not take the glow off")
	}
	if name := c.AwaitSoundName(); name != "minecraft:item.ink_sac.use" {
		t.Errorf("ink sac sound = %q, want the ink sac use", name)
	}

	servertest.GiveHeldItem(c, "minecraft:honeycomb")
	c.PlaceBlock(4, 64, 7, protocol.FaceTop)
	_, _, _, _, data = c.AwaitBlockEntityData()
	if !signWaxed(t, data) {
		t.Error("honeycomb did not wax the sign")
	}
	c.AwaitWorldEvent(protocol.WorldEventWaxOn)
}

// TestAnotherPlayerSeesTheSignTextAndTheEditStaysWithItsHolder watches a
// second client while the first writes a sign: the text reaches them through
// the Block Entity Data broadcast, an update from anyone but the editor
// changes nothing, and the click of the sign — once the writer is done —
// hands the edit to the watcher to write in kind.
func TestAnotherPlayerSeesTheSignTextAndTheEditStaysWithItsHolder(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")
	watcher.ExpectPlayerAdded("Actor")
	servertest.HoldTicks(t, srv)

	placeSignFront(actor, "minecraft:oak_sign", 4, 7)
	actor.AwaitOpenSignEditor()
	// Flush placement before either client writes or reads the sign text.
	actor.ReadTicks(srv, 1, func(int32, []byte) bool { return false })

	// The watcher tries an update no editor was opened to them for; the
	// settle runs it through the server before the actor writes.
	watcher.SendSignUpdate(4, 64, 7, true, [4]string{"Hacked", "", "", ""})
	watcher.Settle()

	actor.SendSignUpdate(4, 64, 7, true, [4]string{"Mine", "not", "yours", ""})
	_, _, _, _, data := actor.AwaitBlockEntityData()
	if messages, _, _ := signSide(t, data, "front_text"); messages != [4]string{"Mine", "not", "yours", ""} {
		t.Fatalf("front text = %q, want the writer's", messages)
	}
	// The other player sees the same text in the same broadcast.
	_, _, _, _, seen := watcher.AwaitBlockEntityData()
	if messages, _, _ := signSide(t, seen, "front_text"); messages != [4]string{"Mine", "not", "yours", ""} {
		t.Errorf("the watcher saw %q, want the writer's %q", messages, [4]string{"Mine", "not", "yours", ""})
	}

	// The writer's update spent the edit; the watcher's own click takes the
	// sign from here.
	watcher.MoveTo(4.5, 64, 8.5, 180, 0)
	watcher.Settle()
	watcher.UseBlock(4, 64, 7)
	if x, y, z, front := watcher.AwaitOpenSignEditor(); front {
		if x != 4 || y != 64 || z != 7 {
			t.Errorf("watcher's editor at (%d,%d,%d), want (4,64,7)", x, y, z)
		}
	}
	watcher.SendSignUpdate(4, 64, 7, true, [4]string{"Theirs", "now", "", ""})
	_, _, _, _, data = watcher.AwaitBlockEntityData()
	if messages, _, _ := signSide(t, data, "front_text"); messages != [4]string{"Theirs", "now", "", ""} {
		t.Errorf("front text = %q, want the watcher's", messages)
	}
}

// TestSignTextSurvivesARestart writes a sign, restarts the server on the
// same world, and reads the text back through what a dye answers: the
// reloaded sign still says what was written, told as its own block entity
// with the dye's new colour on it.
func TestSignTextSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Writer")

	placeSignFront(c, "minecraft:oak_sign", 4, 7)
	c.AwaitOpenSignEditor()
	c.SendSignUpdate(4, 64, 7, true, [4]string{"Alpha", "Bravo", "Charlie", "Delta"})
	c.AwaitBlockEntityData()

	// Off the server and back on again, pointed at the same world.
	c.Disconnect()
	srv.Shutdown()
	restarted := *cfg
	_, reader := servertest.Join(t, &restarted, "Reader")

	// A dye answers with the whole block entity, which says what the
	// reloaded sign still holds.
	servertest.GiveHeldItem(reader, "minecraft:red_dye")
	reader.MoveTo(4.5, 64, 8.5, 180, 0)
	reader.Settle()
	reader.PlaceBlock(4, 64, 7, protocol.FaceTop)
	_, _, _, beType, data := reader.AwaitBlockEntityData()
	if beType != protocol.BlockEntitySign {
		t.Fatalf("reloaded sign block entity type = %d, want %d", beType, protocol.BlockEntitySign)
	}
	if messages, color, glowing := signSide(t, data, "front_text"); messages != [4]string{"Alpha", "Bravo", "Charlie", "Delta"} {
		t.Errorf("reloaded front text = %q, want what was written", messages)
	} else if color != "red" {
		t.Errorf("reloaded front text colour = %q, want the dye's red", color)
	} else if glowing {
		t.Error("reloaded front text glows, want it plain")
	}
	if signWaxed(t, data) {
		t.Error("reloaded sign is waxed, want it unwaxed")
	}
}

// TestAWalkedAwayEditorLapses walks the sign's editor away until the tick
// that watches the edit gives it up: their update is refused as no longer
// theirs, and the editor has to be opened again — which the walk back and a
// click do, after which the update lands.
func TestAWalkedAwayEditorLapses(t *testing.T) {
	t.Parallel()
	srv, c := signActor(t, "Walker")

	placeSignFront(c, "minecraft:oak_sign", 4, 7)
	c.AwaitOpenSignEditor()

	// Far enough that the sign's per-tick reach check gives the edit up.
	c.MoveTo(4.5, 64, 23.5, 0, 0)
	c.Settle()
	servertest.AwaitTicks(t, srv, 2)

	c.MoveTo(4.5, 64, 8.5, 180, 0)
	c.Settle()
	c.SendSignUpdate(4, 64, 7, true, [4]string{"From", "afar", "", ""})
	if c.PumpTicks(srv, 3, seePacket(protocol.IDPlayBlockEntityData)) {
		t.Error("a walked-away editor's update was answered")
	}

	// The click back where the sign stands reopens the editor, and the
	// update then lands.
	c.UseBlock(4, 64, 7)
	if _, _, _, front := c.AwaitOpenSignEditor(); !front {
		t.Fatal("the reopened editor is for the back")
	}
	c.SendSignUpdate(4, 64, 7, true, [4]string{"Back", "in", "reach", ""})
	_, _, _, _, data := c.AwaitBlockEntityData()
	if messages, _, _ := signSide(t, data, "front_text"); messages != [4]string{"Back", "in", "reach", ""} {
		t.Errorf("front text = %q, want the reopened editor's write", messages)
	}
}

// TestTheApplicatorsSpendOneItemOutsideCreative runs the editor round in
// survival, where everything costs: the placed sign comes out of the held
// stack, and the dye that colours it spends one of its own.
func TestTheApplicatorsSpendOneItemOutsideCreative(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Spender")

	sign, ok := registry.ItemID("minecraft:oak_sign")
	if !ok {
		t.Fatal("the item registry has no minecraft:oak_sign")
	}
	dye, ok := registry.ItemID("minecraft:red_dye")
	if !ok {
		t.Fatal("the item registry has no minecraft:red_dye")
	}
	srv.GiveSlot("Spender", player.HotbarSlot, protocol.ItemStack{ItemID: sign, Count: 1})
	srv.GiveSlot("Spender", player.HotbarSlot+1, protocol.ItemStack{ItemID: dye, Count: 2})

	c.MoveTo(4.5, 64, 8.5, 180, 0)
	c.Settle()
	c.PlaceBlock(4, 63, 7, protocol.FaceTop)
	if _, _, _, front := c.AwaitOpenSignEditor(); !front {
		t.Fatal("survival placement opened the back editor")
	}
	// The editor's packet reaches the client before the placement spends the
	// stack, so wait for the cost rather than reading the slot once.
	servertest.WaitForTicks(t, srv, 60, func() bool {
		return srv.InventorySlot("Spender", player.HotbarSlot).Empty()
	})
	if held := srv.InventorySlot("Spender", player.HotbarSlot); !held.Empty() {
		t.Errorf("sign stack after the placement = %d, want it spent", held.Count)
	}

	// Write some text, which a dye asks of the side it colours.
	c.SendSignUpdate(4, 64, 7, true, [4]string{"Worth", "a", "dye", ""})
	c.AwaitBlockEntityData()

	// The dye sits in the next slot; the client's held item choice says so.
	c.Send(protocol.IDPlayHeldItemIn, func(b *protocol.Buffer) { b.PutI16(1) })
	c.Settle()
	c.PlaceBlock(4, 64, 7, protocol.FaceTop)
	c.AwaitBlockEntityData()
	if name := c.AwaitSoundName(); name != "minecraft:item.dye.use" {
		t.Errorf("dye sound = %q, want the dye use", name)
	}
	// The applicator's answers reach the client before the dye is spent, so
	// wait for the cost rather than reading the slot once.
	servertest.WaitForTicks(t, srv, 60, func() bool {
		return srv.InventorySlot("Spender", player.HotbarSlot+1).Count == 1
	})
	if got := srv.InventorySlot("Spender", player.HotbarSlot+1); got.Count != 1 {
		t.Errorf("dye stack after colouring = %d, want one of the two spent", got.Count)
	}
}

// TestAHangingSignOpensItsEditorToo hangs an oak hanging sign from a block
// above and writes it: the editor opens as it does on the ground's signs,
// the block entity is the hanging kind, and waxing it wobbles with the
// hanging sign's own sound.
func TestAHangingSignOpensItsEditorToo(t *testing.T) {
	t.Parallel()
	srv, c := signActor(t, "Hanger")
	servertest.HoldTicks(t, srv)

	// Hang it from the block above the cell it stands in.
	srv.SetBlock(5, 66, 7, servertest.MustState(t, "minecraft:stone", nil))
	c.MoveTo(5.5, 64, 7.5, 0, -89)
	servertest.GiveHeldItem(c, "minecraft:oak_hanging_sign")
	c.Settle()
	c.PlaceBlock(5, 66, 7, protocol.FaceBottom)

	if x, y, z, front := c.AwaitOpenSignEditor(); front {
		if x != 5 || y != 65 || z != 7 {
			t.Fatalf("sign editor at (%d,%d,%d), want (5,65,7)", x, y, z)
		}
	}
	// The placement update carries empty text before the editor writes it.
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	c.SendSignUpdate(5, 65, 7, true, [4]string{"Up", "here", "", ""})
	_, _, _, beType, data := c.AwaitBlockEntityData()
	if beType != protocol.BlockEntityHangingSign {
		t.Fatalf("hanging sign block entity type = %d, want %d", beType, protocol.BlockEntityHangingSign)
	}
	if _, has := data["id"]; has {
		t.Errorf("hanging sign update tag carries an id: %#v", data)
	}
	if messages, _, _ := signSide(t, data, "front_text"); messages != [4]string{"Up", "here", "", ""} {
		t.Errorf("hanging sign front text = %q, want what was written", messages)
	}

	// The wax and the wobble of a hanging sign are its own.
	servertest.GiveHeldItem(c, "minecraft:honeycomb")
	c.PlaceBlock(5, 65, 7, protocol.FaceTop)
	_, _, _, _, data = c.AwaitBlockEntityData()
	if !signWaxed(t, data) {
		t.Error("honeycomb did not wax the hanging sign")
	}
	c.AwaitWorldEvent(protocol.WorldEventWaxOn)
	if name := c.AwaitSoundName(); name != "minecraft:item.honeycomb.wax_on" {
		t.Errorf("wax sound = %q, want item.honeycomb.wax_on", name)
	}
	c.UseBlock(5, 65, 7)
	if name := c.AwaitSoundName(); name != "minecraft:block.hanging_sign.waxed_interact_fail" {
		t.Errorf("waxed hanging sign click sound = %q, want its own wobble", name)
	}
}

// TestASneakingPlayerHangsACeilingSignOnItsMiddleChain hangs an oak hanging
// sign under stone: sneaking hangs it on its middle chain with the 16-way
// rotation (yaw 45 + 180 is segment 10), standing hangs it rigid.
func TestASneakingPlayerHangsACeilingSignOnItsMiddleChain(t *testing.T) {
	t.Parallel()
	srv, c := signActor(t, "Sneaker")
	servertest.HoldTicks(t, srv)

	stone := servertest.MustState(t, "minecraft:stone", nil)
	for _, tc := range []struct {
		sneaking bool
		x        int32
		attached string
	}{
		{sneaking: true, x: 5, attached: "true"},
		{sneaking: false, x: 8, attached: "false"},
	} {
		srv.SetBlock(tc.x, 66, 7, stone)
		c.MoveTo(float64(tc.x)+0.5, 64, 7.5, 45, -89)
		c.SendSneak(tc.sneaking)
		servertest.GiveHeldItem(c, "minecraft:oak_hanging_sign")
		c.Settle()
		c.PlaceBlock(tc.x, 66, 7, protocol.FaceBottom)
		c.AwaitOpenSignEditor()

		name, props := servertest.StateProps(srv.BlockAt(tc.x, 65, 7))
		if name != "minecraft:oak_hanging_sign" || props["attached"] != tc.attached {
			t.Fatalf("sneaking=%v: sign = %s %v, want oak_hanging_sign attached=%s", tc.sneaking, name, props, tc.attached)
		}
		if tc.sneaking && props["rotation"] != "10" {
			t.Errorf("sneaking: rotation = %s, want 10", props["rotation"])
		}
	}
}

// TestAHangingSignChainsBelowACeilingHangingSign clicks the underside of a
// ceiling hanging sign holding a hanging sign: a new one hangs below it. A
// plain sign held opens the clicked sign's editor.
func TestAHangingSignChainsBelowACeilingHangingSign(t *testing.T) {
	t.Parallel()
	srv, c := signActor(t, "Chainer")
	servertest.HoldTicks(t, srv)

	const x = 5
	srv.SetBlock(x, 66, 7, servertest.MustState(t, "minecraft:stone", nil))
	c.MoveTo(x+0.5, 64, 7.5, 45, -89)
	servertest.GiveHeldItem(c, "minecraft:oak_hanging_sign")
	c.Settle()
	c.PlaceBlock(x, 66, 7, protocol.FaceBottom)
	c.AwaitOpenSignEditor()
	c.SendSignUpdate(x, 65, 7, true, [4]string{"top", "", "", ""})
	c.Settle()

	servertest.GiveHeldItem(c, "minecraft:oak_sign")
	c.PlaceBlock(x, 65, 7, protocol.FaceBottom)
	if _, y, _, _ := c.AwaitOpenSignEditor(); y != 65 {
		t.Errorf("plain sign held: editor at y=%d, want the clicked sign's 65", y)
	}
	c.SendSignUpdate(x, 65, 7, true, [4]string{"top", "", "", ""})
	c.Settle()

	servertest.GiveHeldItem(c, "minecraft:oak_hanging_sign")
	c.PlaceBlock(x, 65, 7, protocol.FaceBottom)
	if _, y, _, _ := c.AwaitOpenSignEditor(); y != 64 {
		t.Errorf("editor at y=%d, want the chained sign's 64", y)
	}
	if name, _ := servertest.StateProps(srv.BlockAt(x, 64, 7)); name != "minecraft:oak_hanging_sign" {
		t.Errorf("block below = %s, want oak_hanging_sign", name)
	}
}

// TestAHangingSignChainsOnlyBelowASignWithoutLiveClicks puts a hanging sign
// on the underside of a ceiling hanging sign whose front has a click event: a
// waxed one stops the chain, an unwaxed one does not.
func TestAHangingSignChainsOnlyBelowASignWithoutLiveClicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Chainer", servertest.OpsListing(t, "Chainer"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(5, 66, 7, servertest.MustState(t, "minecraft:stone", nil))
	// The default rotation, 8, turns the front north, toward the player.
	c.MoveTo(5.5, 64, 5.5, 0, 0)
	c.Settle()
	for _, tc := range []struct {
		wax   string
		chain bool
	}{{"1b", false}, {"0b", true}} {
		c.ChatCommand(`setblock 5 65 7 minecraft:oak_hanging_sign[attached=false]{is_waxed:` + tc.wax + `,front_text:{messages:[{text:"Go",click_event:{action:"run_command",command:"/say hi"}},"","",""]}}`)
		c.ExpectCommandFeedback("commands.setblock.success", "5", "65", "7")
		srv.SetBlock(5, 64, 7, 0)
		servertest.GiveHeldItem(c, "minecraft:oak_hanging_sign")
		c.Settle()
		c.PlaceBlock(5, 65, 7, protocol.FaceBottom)
		c.Settle()
		name, _ := servertest.StateProps(srv.BlockAt(5, 64, 7))
		if got := name == "minecraft:oak_hanging_sign"; got != tc.chain {
			t.Errorf("waxed=%s: chained = %v, want %v", tc.wax, got, tc.chain)
		}
	}
}

// TestABrokenSignLeavesNoTextForTheNextStandingThere breaks a sign's support
// so the sign pops, puts the support back, and stands a new sign in its
// place: the new sign must servertest.Start empty, not inherit the broken sign's text.
func TestABrokenSignLeavesNoTextForTheNextStandingThere(t *testing.T) {
	t.Parallel()
	srv, c := signActor(t, "Replacer")

	placeSignFront(c, "minecraft:oak_sign", 4, 7)
	c.AwaitOpenSignEditor()
	c.SendSignUpdate(4, 64, 7, true, [4]string{"old", "", "", ""})
	c.AwaitBlockEntityData()

	// Breaking the support pops the standing sign standing on it.
	c.BreakBlock(4, 63, 7)
	servertest.WaitForBlock(t, srv, 4, 64, 7, func(s uint16) bool { return s == 0 })
	c.Settle()

	// Put the support back and stand a new sign in the cell.
	servertest.GiveHeldItem(c, "minecraft:stone")
	c.PlaceBlock(4, 63, 7, protocol.FaceTop)
	c.Settle()
	placeSignFront(c, "minecraft:oak_sign", 4, 7)
	c.AwaitOpenSignEditor()
	// Drain the placement's block entity snapshot before reading the edit.
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })

	// The new sign's back is written; the front comes back in the same
	// broadcast and must be empty, not the old sign's.
	c.SendSignUpdate(4, 64, 7, false, [4]string{"new", "", "", ""})
	_, _, _, _, data := c.AwaitBlockEntityData()
	if front, _, _ := signSide(t, data, "front_text"); front != [4]string{} {
		t.Errorf("the new sign's front = %q, want it empty rather than the broken sign's text", front)
	}
	if back, _, _ := signSide(t, data, "back_text"); back != [4]string{"new", "", "", ""} {
		t.Errorf("the new sign's back = %q, want the write", back)
	}
}

// TestTheEditorWaitStepsOverAnItemsMotion breaks a sign's support with the
// mailbox's writer held, so the popped item's motion packets queue ahead of
// the placement that stands the next sign: the editor wait has to step over
// them the way it steps over the echoes.
func TestTheEditorWaitStepsOverAnItemsMotion(t *testing.T) {
	t.Parallel()
	srv, c := signActor(t, "Replacer")
	servertest.HoldTicks(t, srv)

	placeSignFront(c, "minecraft:oak_sign", 4, 7)
	c.AwaitOpenSignEditor()
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	c.SendSignUpdate(4, 64, 7, true, [4]string{"old", "", "", ""})
	c.AwaitBlockEntityData()

	// Breaking the support pops the standing sign standing on it. The break
	// is the connection's own handling, which held ticks do not wait for, so
	// it is settled before the hold: the loop runs no tick in between.
	c.BreakBlock(4, 63, 7)
	c.Settle()
	if s := srv.BlockAt(4, 64, 7); s != 0 {
		t.Fatalf("block at (4, 64, 7) = %d after the support broke, want air", s)
	}

	release := srv.HoldMailbox("Replacer")
	defer release()

	// The hold keeps the flight's packets queued. The budget runs past the
	// item's movement rhythm — its motion rides its twentieth tick, a
	// bounce sooner — so the editor the placement queues lands behind a
	// motion packet.
	servertest.AwaitTicks(t, srv, 30)

	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the item registry has no minecraft:stone")
	}
	sign, ok := registry.ItemID("minecraft:oak_sign")
	if !ok {
		t.Fatal("the item registry has no minecraft:oak_sign")
	}
	// The support back and a new sign in the cell, no settles between: a
	// settle's answer rides the held mailbox too, and the placement's
	// editor must queue behind the motion.
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	c.PlaceBlock(4, 63, 7, protocol.FaceTop)
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: sign, Count: 1})
	c.PlaceBlock(4, 63, 7, protocol.FaceTop)
	release()

	if x, y, z, front := c.AwaitOpenSignEditor(); !front || x != 4 || y != 64 || z != 7 {
		t.Errorf("sign editor at (%d,%d,%d) front %v, want (4,64,7) front", x, y, z, front)
	}

	// Drain the placement's block entity snapshot before reading the edit.
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })

	// The new sign's back is written; the front comes back in the same
	// broadcast and must be empty, not the broken sign's.
	c.SendSignUpdate(4, 64, 7, false, [4]string{"new", "", "", ""})
	_, _, _, _, data := c.AwaitBlockEntityData()
	if front, _, _ := signSide(t, data, "front_text"); front != [4]string{} {
		t.Errorf("the new sign's front = %q, want it empty rather than the broken sign's text", front)
	}
	if back, _, _ := signSide(t, data, "back_text"); back != [4]string{"new", "", "", ""} {
		t.Errorf("the new sign's back = %q, want the write", back)
	}
}

// TestPlacingAPickedSignKeepsItsText ctrl-picks a sign that was written to and
// places the picked item: the text travels in minecraft:sign_text_front, not
// block_entity_data, and is laid on the new sign.
// vanilla: SignBlockEntity.collectImplicitComponents,
// SignBlockEntity.removeComponentsFromTag, SignBlockEntity.applyImplicitComponents
func TestPlacingAPickedSignKeepsItsText(t *testing.T) {
	t.Parallel()
	_, c := signActor(t, "Writer")

	placeSignFront(c, "minecraft:oak_sign", 4, 7)
	if _, _, _, front := c.AwaitOpenSignEditor(); !front {
		t.Fatal("the placed sign's editor is for the back")
	}
	c.SendSignUpdate(4, 64, 7, true, [4]string{"Hello", "world", "", ""})
	c.AwaitBlockEntityData()

	picked := servertest.PickedStack(t, c, 4, 64, 7, true)
	if _, tag, ok := picked.BlockEntityDataOf(); ok {
		t.Errorf("the picked sign carried block_entity_data %v, want its text in components only", tag)
	}
	front, ok := picked.SignTextOf(true)
	if !ok {
		t.Fatalf("the picked sign carried no sign_text_front: %+v", picked)
	}
	if want := (protocol.SignText{Messages: [4]nbt.Tag{nbt.String("Hello"), nbt.String("world"), nbt.String(""), nbt.String("")},
		Dye: 15}); !reflect.DeepEqual(front, want) {
		t.Errorf("the picked sign's sign_text_front = %+v, want %+v", front, want)
	}
	// The empty back is the item's default, no patch.
	if back, ok := picked.SignTextOf(false); ok {
		t.Errorf("the picked sign carried sign_text_back %+v, want none", back)
	}
	if picked.Waxed() {
		t.Error("the picked unwaxed sign carried minecraft:waxed")
	}

	// The ctrl-pick left the sign in hand; put it down again. Its text rides
	// out in a Block Entity Data packet.
	c.PlaceBlock(6, 63, 7, protocol.FaceTop)
	_, placed := collectPlacedSign(c, 6, 64, 7)
	if placed == nil {
		t.Fatal("the placed sign's text never reached the client")
	}
	if messages, _, _ := signSide(t, placed, "front_text"); messages != [4]string{"Hello", "world", "", ""} {
		t.Errorf("the placed sign's front text = %q, want the picked sign's", messages)
	}
}

// TestAPickedWaxedSignCarriesWax picks a waxed sign: the wax travels as
// minecraft:waxed, and the empty sides as no component at all.
// vanilla: SignBlockEntity.collectImplicitComponents
func TestAPickedWaxedSignCarriesWax(t *testing.T) {
	t.Parallel()
	srv, c := signActor(t, "Waxer")

	placeSignFront(c, "minecraft:oak_sign", 4, 7)
	c.AwaitOpenSignEditor()
	// Drain the placement's unwaxed block entity snapshot before reading the wax.
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	servertest.GiveHeldItem(c, "minecraft:honeycomb")
	c.PlaceBlock(4, 64, 7, protocol.FaceTop)
	if _, _, _, _, data := c.AwaitBlockEntityData(); !signWaxed(t, data) {
		t.Fatal("the honeycomb did not wax the sign")
	}

	picked := servertest.PickedStack(t, c, 4, 64, 7, true)
	if !picked.Waxed() {
		t.Errorf("the picked waxed sign carried no minecraft:waxed: %+v", picked)
	}
	if _, ok := picked.SignTextOf(true); ok {
		t.Error("the picked blank sign carried sign_text_front")
	}
	if _, tag, ok := picked.BlockEntityDataOf(); ok {
		t.Errorf("the picked sign carried block_entity_data %v", tag)
	}
}

// TestPlacingAPickedBlankSignOpensTheEditor picks a sign that carries no text
// and places it: the pick carries nothing a plain sign does not, and the
// editor opens as it would for a plain placement.
// vanilla: SignBlock.setPlacedBy
func TestPlacingAPickedBlankSignOpensTheEditor(t *testing.T) {
	t.Parallel()
	_, c := signActor(t, "Writer")

	placeSignFront(c, "minecraft:oak_sign", 4, 7)
	if _, _, _, front := c.AwaitOpenSignEditor(); !front {
		t.Fatal("the placed sign's editor is for the back")
	}

	// Something else in hand, so the pick has a sign to conjure rather than
	// finding the held plain sign already the picked stack.
	servertest.GiveHeldItem(c, "minecraft:stone")
	picked := servertest.PickedStack(t, c, 4, 64, 7, true)
	if !picked.Equal(protocol.ItemStack{ItemID: picked.ItemID, Count: 1}) {
		t.Fatalf("the picked blank sign = %+v, want a plain sign", picked)
	}

	c.PlaceBlock(6, 63, 7, protocol.FaceTop)
	if x, y, z, front := c.AwaitOpenSignEditor(); !front || x != 6 || y != 64 || z != 7 {
		t.Errorf("sign editor at (%d,%d,%d) front %v, want (6,64,7) front", x, y, z, front)
	}
}

// TestPlacingAGivenWaxedSignWithTextOpensNoEditor gives a sign carrying front
// text and wax and places it: the sign takes both, and no editor opens.
// vanilla: SignBlockEntity.applyImplicitComponents, SignBlock.setPlacedBy
func TestPlacingAGivenWaxedSignWithTextOpensNoEditor(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Giver", servertest.OpsListing(t, "Giver"))

	c.MoveTo(4.5, 64, 8.5, 180, 0)
	c.ChatCommand(`give @s oak_sign[sign_text_front={messages:["a","","",""]},waxed={}]`)
	servertest.AwaitSlotUpdates(t, c, 1)
	c.PlaceBlock(4, 63, 7, protocol.FaceTop)

	opened, placed := collectPlacedSign(c, 4, 64, 7)
	if opened {
		t.Error("placing a waxed sign opened an editor")
	}
	if placed == nil {
		t.Fatal("the placed sign's text never reached the client")
	}
	if messages, _, _ := signSide(t, placed, "front_text"); messages != [4]string{"a", "", "", ""} {
		t.Errorf("the placed sign's front text = %q, want the given text", messages)
	}
	if !signWaxed(t, placed) {
		t.Error("the placed sign is not waxed")
	}
}

// TestASignsBlockEntityDataTextGivesWayToItsComponents places a sign whose
// block_entity_data holds front text and wax but whose stack carries no sign
// components: the item's default empty sides and no wax replace the data's, so
// the editor opens.
// vanilla: BlockItem.place, SignBlockEntity.applyImplicitComponents
func TestASignsBlockEntityDataTextGivesWayToItsComponents(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Giver", servertest.OpsListing(t, "Giver"))

	c.MoveTo(4.5, 64, 8.5, 180, 0)
	stack := protocol.ItemStack{ItemID: int32(registry.ItemOakSign), Count: 1}
	stack.SetBlockEntityData(protocol.BlockEntitySign, nbt.Compound{
		"id":         nbt.String("minecraft:sign"),
		"front_text": nbt.Compound{"messages": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("old"), nbt.String(""), nbt.String(""), nbt.String("")}}},
		"is_waxed":   nbt.Byte(1),
	})
	c.SetCreativeSlot(player.HotbarSlot, stack)
	c.Settle()
	c.PlaceBlock(4, 63, 7, protocol.FaceTop)

	if x, y, z, front := c.AwaitOpenSignEditor(); !front || x != 4 || y != 64 || z != 7 {
		t.Errorf("sign editor at (%d,%d,%d) front %v, want (4,64,7) front", x, y, z, front)
	}
}

// collectPlacedSign settles a sign placement and reports whether an editor
// opened and the Block Entity Data sent for (x, y, z), nil if none.
func collectPlacedSign(c *servertest.Client, x, y, z int32) (opened bool, placed nbt.Compound) {
	c.T.Helper()
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayOpenSignEditor:
			opened = true
		case protocol.IDPlayBlockEntityData:
			r := protocol.NewReader(data)
			if px, py, pz := r.ReadPosition(); px == x && py == y && pz == z {
				r.ReadVarInt()
				if tag, _, err := nbt.DecodeNetwork(r.Rest()); err == nil {
					placed, _ = tag.(nbt.Compound)
				}
			}
		}
	})
	return opened, placed
}

// TestASignEditKeepsEachLinesStyle edits a sign a command styled: the new text
// keeps the line's colour and bold.
func TestASignEditKeepsEachLinesStyle(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Writer"))
	c.Join("Writer")
	c.MoveTo(4.5, 64, 8.5, 180, 0)
	c.Settle()

	c.ChatCommand(`setblock 4 65 7 minecraft:oak_sign{front_text:{messages:[{text:"Shop",color:"red",bold:true},"","",""],filtered_messages:["Filtered shop","","",""]}}`)
	c.ExpectCommandFeedback("commands.setblock.success", "4", "65", "7")
	c.Settle()
	c.UseBlock(4, 65, 7)
	c.AwaitOpenSignEditor()
	c.SendSignUpdate(4, 65, 7, true, [4]string{"Open", "", "", ""})
	_, _, _, _, data := c.AwaitBlockEntityData()

	check := func(where string, data nbt.Compound) {
		front, _ := nbt.Get[nbt.Compound](data, "front_text")
		if _, present := front["filtered_messages"]; present {
			t.Errorf("%s: an edit kept the old filtered messages", where)
		}
		list, _ := nbt.Get[nbt.List](front, "messages")
		if len(list.Items) != 4 {
			t.Fatalf("%s: messages = %#v", where, front["messages"])
		}
		line, ok := list.Items[0].(nbt.Compound)
		if !ok {
			t.Fatalf("%s: line 0 = %#v, want a compound", where, list.Items[0])
		}
		text, _ := nbt.Get[nbt.String](line, "text")
		color, _ := nbt.Get[nbt.String](line, "color")
		bold, _ := nbt.Get[nbt.Byte](line, "bold")
		if text != "Open" || color != "red" || bold != 1 {
			t.Errorf("%s: line 0 = %#v, want Open red bold", where, line)
		}
	}
	check("broadcast", data)
	saved, ok := srv.BlockEntityTagAt(4, 65, 7)
	if !ok {
		t.Fatal("no block entity saved")
	}
	check("saved", saved)
}

// TestASignsClickEventsRunOnAClick clicks three signs from the front: a waxed
// one allowed op features runs its line's command as the clicker, one not
// allowed refuses with a red overlay, and an allowed one shows its dialog.
func TestASignsClickEventsRunOnAClick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Clicker", servertest.OpsListing(t, "Clicker"))
	// The default rotation, 8, turns the front north.
	c.MoveTo(4.5, 64, 5.5, 0, 0)
	c.Settle()
	gold := registry.BlockStates["minecraft:gold_block"]

	c.ChatCommand(`setblock 4 65 7 minecraft:oak_sign{is_waxed:1b,allow_op_features:1b,front_text:{messages:[{text:"Go",click_event:{action:"run_command",command:"/setblock 3 64 3 minecraft:gold_block"}},"","",""]}}`)
	c.ExpectCommandFeedback("commands.setblock.success", "4", "65", "7")
	saved, ok := srv.BlockEntityTagAt(4, 65, 7)
	if !ok {
		t.Fatal("no block entity saved")
	}
	if flag, ok := nbt.Get[nbt.Byte](saved, "allow_op_features"); !ok || flag != 1 {
		t.Errorf("saved allow_op_features = %#v, want 1b", saved["allow_op_features"])
	}
	c.Settle()
	c.UseBlock(4, 65, 7)
	c.AwaitBlockState(3, 64, 3, gold)

	c.ChatCommand(`setblock 5 65 7 minecraft:oak_sign{front_text:{messages:[{text:"No",click_event:{action:"run_command",command:"/setblock 3 64 3 minecraft:stone"}},"","",""]}}`)
	c.ExpectCommandFeedback("commands.setblock.success", "5", "65", "7")
	c.Settle()
	c.UseBlock(5, 65, 7)
	if !c.SawSystemChatWithin(srv, 2, "sign.click_actions_disabled") {
		t.Error("a sign without op features sent no click_actions_disabled overlay")
	}
	if got := srv.BlockAt(3, 64, 3); got != gold {
		t.Errorf("block at 3 64 3 = %d after a disallowed click, want gold %d", got, gold)
	}

	c.ChatCommand(`setblock 3 65 7 minecraft:oak_sign{allow_op_features:1b,front_text:{messages:[{text:"Links",click_event:{action:"show_dialog",dialog:"minecraft:server_links"}},"","",""]}}`)
	c.ExpectCommandFeedback("commands.setblock.success", "3", "65", "7")
	c.Settle()
	c.UseBlock(3, 65, 7)
	c.ExpectWithinTicks(srv, 2, protocol.IDPlayShowDialog)
}

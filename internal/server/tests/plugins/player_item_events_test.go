package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ServerPlayer.completeUsingItem
func TestPluginItemConsumeCancelKeepsTheMeal(t *testing.T) {
	t.Parallel()
	var consumed int
	var cancel bool
	srv, _, c := blockScene(t, "minecraft:bread", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerItemConsumeEvent) {
			consumed++
			if e.Item.Item != registry.ItemBread || e.Hand != plugin.MainHand {
				t.Errorf("consume inputs: %v %v", e.Item, e.Hand)
			}
			e.SetCancelled(cancel)
		})
	})
	srv.SetFood("Miner", 10)
	cancel = true
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	for i := 0; consumed < 1; i++ {
		if i == 200 {
			t.Fatal("no consume event")
		}
		servertest.RunTicks(t, srv, 1)
	}
	c.Settle()
	if srv.InventorySlot("Miner", player.HotbarSlot).Count != 1 {
		t.Fatal("a cancelled meal still used up the bread")
	}
	if srv.Eating("Miner") {
		t.Fatal("a cancelled meal left the player eating")
	}
	cancel = false
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	for i := 0; consumed < 2; i++ {
		if i == 200 {
			t.Fatal("no second consume event")
		}
		servertest.RunTicks(t, srv, 1)
	}
	c.Settle()
	if !srv.InventorySlot("Miner", player.HotbarSlot).Empty() {
		t.Fatal("an allowed meal kept the bread")
	}
}

// vanilla: BucketItem.use
func TestPluginBucketCancelAndResult(t *testing.T) {
	t.Parallel()
	var events int
	var cancel bool
	var last plugin.PlayerBucketEvent
	srv, _, c := blockScene(t, "minecraft:water_bucket", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerBucketEvent) {
			events++
			last = *e
			e.SetCancelled(cancel)
			if !cancel {
				e.Result = plugin.ItemStack{Item: registry.ItemMilkBucket, Count: 1}
			}
		})
	})
	srv.SetBlock(0, 63, 0, servertest.BlockState(t, "minecraft:stone"))
	cancel = true
	useDown(c)
	fired(t, srv, c, "cancelled pour", &events, 1)
	if last.Fill || last.Face != plugin.Up || last.Block.Pos != (plugin.BlockPos{X: 0, Y: 64, Z: 0}) {
		t.Errorf("bucket inputs: fill %v face %v block %v", last.Fill, last.Face, last.Block.Pos)
	}
	if srv.BlockAt(0, 64, 0) == servertest.BlockState(t, "minecraft:water") ||
		srv.InventorySlot("Miner", player.HotbarSlot).ItemID != registry.MustItem("minecraft:water_bucket") {
		t.Fatal("a cancelled pour changed the world or the bucket")
	}
	cancel = false
	useDown(c)
	fired(t, srv, c, "allowed pour", &events, 2)
	if srv.BlockAt(0, 64, 0) != servertest.BlockState(t, "minecraft:water") {
		t.Fatal("an allowed pour placed no water")
	}
	if got := srv.InventorySlot("Miner", player.HotbarSlot).ItemID; got != registry.MustItem("minecraft:milk_bucket") {
		t.Fatalf("hand holds item %d, want the listener's Result", got)
	}
}

// vanilla: ServerPlayer.startSleepInBed
func TestPluginBedEnterResultAndLeave(t *testing.T) {
	t.Parallel()
	var enters, leaves int
	var result plugin.BedResult
	var cancel bool
	srv, _, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerBedEnterEvent) {
			enters++
			result = e.Result
			e.SetCancelled(cancel)
			e.Result = plugin.BedOK
		})
		plugin.Listen(s, func(e *plugin.PlayerBedLeaveEvent) { leaves++ })
	})
	if !srv.SetBlock(2, 64, 1, servertest.BedStateName("red", "foot", "south")) ||
		!srv.SetBlock(2, 64, 2, servertest.BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed")
	}
	srv.SetDayTime(6000)
	cancel = true
	c.UseBlock(2, 64, 1)
	fired(t, srv, c, "cancelled bed", &enters, 1)
	if result != plugin.BedNotPossibleNow || srv.Sleeping("Miner") {
		t.Fatalf("by day: result %v, sleeping %v, want NotPossibleNow and awake", result, srv.Sleeping("Miner"))
	}
	// By night nothing refuses and the sleeper stays down, so the leave below
	// is the only one.
	cancel = false
	srv.SetDayTime(14000)
	c.UseBlock(2, 64, 1)
	fired(t, srv, c, "allowed bed", &enters, 2)
	if result != plugin.BedOK || !srv.Sleeping("Miner") {
		t.Fatalf("by night: result %v, sleeping %v, want OK and asleep", result, srv.Sleeping("Miner"))
	}
	c.LeaveBed()
	fired(t, srv, c, "leave bed", &leaves, 1)
	if srv.Sleeping("Miner") {
		t.Fatal("the player is still asleep")
	}
}

// useDown sends Use Item looking straight down.
func useDown(c *servertest.Client) {
	c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutVarInt(1)
		b.PutF32(0)
		b.PutF32(90)
	})
}

// vanilla: Player.giveExperiencePoints, ServerPlayer.setExperienceLevels
func TestPluginExpAndLevelChange(t *testing.T) {
	t.Parallel()
	var exps, levels int
	var oldLevel, newLevel int
	srv, h, run, _ := statePlugin(t, "Eater")
	p := handle(t, h, "Eater")
	plugin.Listen(h, func(e *plugin.PlayerExpChangeEvent) {
		exps++
		e.Amount *= 2
	})
	plugin.Listen(h, func(e *plugin.PlayerLevelChangeEvent) {
		levels++
		oldLevel, newLevel = e.Old, e.New
	})
	run(func() { p.GiveExperience(10) })
	if exps != 1 || p.TotalExperience() != 20 {
		t.Fatalf("events %d, total %d, want 1 and the doubled 20", exps, p.TotalExperience())
	}
	if levels != 1 || oldLevel != 0 || newLevel != 2 {
		t.Fatalf("level events %d (%d -> %d), want one from 0 to 2", levels, oldLevel, newLevel)
	}
	_ = srv
}

// vanilla: PlayerAdvancements.award
func TestPluginAdvancementDoneEvent(t *testing.T) {
	t.Parallel()
	var done int
	var message plugin.Text
	_, h, run, _ := statePlugin(t, "Eater")
	p := handle(t, h, "Eater")
	plugin.Listen(h, func(e *plugin.PlayerAdvancementDoneEvent) {
		done++
		if e.Message != nil {
			message = *e.Message
		}
		e.Message = nil
	})
	adv, _ := h.Advancement(plugin.MinecraftKey("story/mine_stone"))
	run(func() {
		for _, name := range adv.Criteria() {
			p.Advancements(adv).Award(name)
		}
	})
	if done == 0 || message.Translate != "chat.type.advancement.task" {
		t.Fatalf("events %d, message %q, want the task announcement", done, message.Translate)
	}
}

// vanilla: ServerPlayerGameMode.handleBlockBreakAction
func TestPluginLeftClickBlockAndAir(t *testing.T) {
	t.Parallel()
	var blocks, airs int
	var cancel bool
	srv, _, c := blockScene(t, "minecraft:stone", true, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerInteractEvent) {
			switch e.Action {
			case plugin.LeftClickBlock:
				blocks++
				e.SetCancelled(cancel)
			case plugin.LeftClickAir:
				airs++
			}
		})
	})
	d := servertest.DigTarget
	cancel = true
	c.StartDigging(d[0], d[1], d[2])
	ids := fired(t, srv, c, "left click on a block", &blocks, 1)
	resent(t, ids)
	c.Send(protocol.IDPlayPunch, func(*protocol.Buffer) {})
	fired(t, srv, c, "left click in the air", &airs, 1)
}

// vanilla: ServerPlayer.drop
func TestPluginDropItemCancelKeepsTheStack(t *testing.T) {
	t.Parallel()
	var drops int
	var cancel bool
	srv, _, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerDropItemEvent) {
			drops++
			if e.Item.Stack().Item != registry.ItemStone {
				t.Errorf("dropped %v", e.Item.Stack())
			}
			e.SetCancelled(cancel)
		})
	})
	cancel = true
	c.DropKey(false)
	fired(t, srv, c, "cancelled drop", &drops, 1)
	if srv.InventorySlot("Miner", player.HotbarSlot).Count != 1 {
		t.Fatalf("a cancelled drop did not give the stone back: %+v", srv.InventorySlot("Miner", player.HotbarSlot))
	}
	cancel = false
	c.DropKey(false)
	fired(t, srv, c, "allowed drop", &drops, 2)
	if !srv.InventorySlot("Miner", player.HotbarSlot).Empty() {
		t.Fatal("an allowed drop kept the stone")
	}
}

// editBook sends Edit Book for the hotbar slot 0.
func editBook(c *servertest.Client, pages []string, title string) {
	c.Send(protocol.IDPlayEditBook, func(b *protocol.Buffer) {
		b.PutVarInt(0)
		b.PutVarInt(int32(len(pages)))
		for _, page := range pages {
			b.PutString(page)
		}
		b.PutBool(true)
		b.PutString(title)
	})
}

// vanilla: ServerGamePacketListenerImpl.handleEditBook
func TestPluginEditBookCancelAndTitle(t *testing.T) {
	t.Parallel()
	var edits int
	var cancel bool
	srv, _, c := blockScene(t, "minecraft:writable_book", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerEditBookEvent) {
			edits++
			if len(e.Pages) != 1 || e.Pages[0] != "page" || e.Title == nil || *e.Title != "Mine" {
				t.Errorf("edit inputs: %v %v", e.Pages, e.Title)
			}
			e.SetCancelled(cancel)
			t := "Theirs"
			e.Title = &t
		})
	})
	cancel = true
	editBook(c, []string{"page"}, "Mine")
	fired(t, srv, c, "cancelled edit", &edits, 1)
	if srv.InventorySlot("Miner", player.HotbarSlot).ItemID != registry.MustItem("minecraft:writable_book") {
		t.Fatal("a cancelled signing still signed the book")
	}
	cancel = false
	editBook(c, []string{"page"}, "Mine")
	fired(t, srv, c, "allowed edit", &edits, 2)
	book, ok := srv.InventorySlot("Miner", player.HotbarSlot).WrittenBookOf()
	if !ok || book.Title != "Theirs" {
		t.Fatalf("signed book %+v, %v, want the listener's title", book, ok)
	}
}

// vanilla: Sheep.shear
func TestPluginShearEntityCancelAndDrops(t *testing.T) {
	t.Parallel()
	var shears int
	var cancel bool
	var drops []plugin.ItemStack
	srv, _, c := blockScene(t, "minecraft:shears", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerShearEntityEvent) {
			shears++
			drops = e.Drops
			e.SetCancelled(cancel)
			e.Drops = nil
		})
	})
	id := srv.SpawnMob("minecraft:sheep", 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	cancel = true
	c.UseOn(id)
	fired(t, srv, c, "cancelled shear", &shears, 1)
	if srv.InventorySlot("Miner", player.HotbarSlot).Damage != 0 {
		t.Fatal("a cancelled shear wore the shears")
	}
	cancel = false
	c.UseOn(id)
	fired(t, srv, c, "allowed shear", &shears, 2)
	if len(drops) == 0 || srv.InventorySlot("Miner", player.HotbarSlot).Damage != 1 {
		t.Fatalf("shear drops %v, shears damage %d", drops, srv.InventorySlot("Miner", player.HotbarSlot).Damage)
	}
}

package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

func sendBlockTagQuery(c *servertest.Client, transaction, x, y, z int32) {
	c.Send(protocol.IDPlayBlockEntityTagQuery, func(b *protocol.Buffer) {
		b.PutVarInt(transaction)
		b.PutPosition(x, y, z)
	})
}

func sendEntityTagQuery(c *servertest.Client, transaction, entityID int32) {
	c.Send(protocol.IDPlayEntityTagQuery, func(b *protocol.Buffer) {
		b.PutVarInt(transaction)
		b.PutVarInt(entityID)
	})
}

// readTagQuery decodes a Tag Query Response into its transaction and tag.
func readTagQuery(t *testing.T, data []byte) (int32, nbt.Tag) {
	t.Helper()
	r := protocol.NewReader(data)
	transaction := r.ReadVarInt()
	rest := data[len(data)-r.Remaining():]
	if len(rest) == 1 && rest[0] == 0 {
		return transaction, nil
	}
	tag, _, err := nbt.DecodeNetwork(rest)
	if err != nil {
		t.Fatalf("tag query NBT: %v", err)
	}
	return transaction, tag
}

// TestTagQueryAnswersOperator has an operator query a chest, an empty cell
// and their own entity: each reply carries the query's transaction ID, and
// the block's compound has no id or position.
func TestTagQueryAnswersOperator(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	_, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.ChatCommand("setblock 1 63 1 minecraft:chest")
	c.ExpectCommandFeedback("commands.setblock.success", "1", "63", "1")

	sendBlockTagQuery(c, 41, 1, 63, 1)
	id, tag := readTagQuery(t, c.AwaitPlay(protocol.IDPlayTagQuery))
	comp, ok := tag.(nbt.Compound)
	if id != 41 || !ok {
		t.Fatalf("block reply = %d, %T; want transaction 41 and a compound", id, tag)
	}
	for _, k := range []string{"id", "x", "y", "z"} {
		if _, has := comp[k]; has {
			t.Errorf("block reply holds %q", k)
		}
	}

	sendBlockTagQuery(c, 42, 5, 63, 5)
	if id, tag = readTagQuery(t, c.AwaitPlay(protocol.IDPlayTagQuery)); id != 42 || tag != nil {
		t.Errorf("empty cell reply = %d, %v; want transaction 42 and TAG_End", id, tag)
	}

	c.ChatCommand("give Alice minecraft:stone")
	sendEntityTagQuery(c, 43, c.PlayerEntityID)
	id, tag = readTagQuery(t, c.AwaitPlay(protocol.IDPlayTagQuery))
	self, ok := tag.(nbt.Compound)
	if id != 43 || !ok {
		t.Fatalf("entity reply = %d, %T; want transaction 43 and a compound", id, tag)
	}
	if _, has := self["SelectedItem"]; has {
		t.Errorf("player reply holds SelectedItem")
	}

	sendEntityTagQuery(c, 44, 99999)
	sendBlockTagQuery(c, 45, 1, 63, 1)
	if id, _ = readTagQuery(t, c.AwaitPlay(protocol.IDPlayTagQuery)); id != 45 {
		t.Errorf("unknown entity was answered: next reply is transaction %d, want 45", id)
	}
}

// TestTagQueryIgnoresNonOperator has a player below permission level 2 send
// both queries: nothing comes back.
func TestTagQueryIgnoresNonOperator(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Visitor")

	sendBlockTagQuery(c, 1, 1, 63, 1)
	sendEntityTagQuery(c, 2, c.PlayerEntityID)
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayTagQuery {
			t.Errorf("a non-operator's tag query was answered")
		}
	})
}

// TestEntityTagQueryAnswersMobsInTheOwnDimension has an operator query a
// cow, then again from the Nether: the cow stands in another dimension, so
// the second query gets no reply.
func TestEntityTagQueryAnswersMobsInTheOwnDimension(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.ChatCommand("summon minecraft:cow 2.5 64 2.5")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)

	sendEntityTagQuery(c, 1, cow.EntityID)
	id, tag := readTagQuery(t, c.AwaitPlay(protocol.IDPlayTagQuery))
	comp, ok := tag.(nbt.Compound)
	if id != 1 || !ok || comp["variant"] == nil {
		t.Fatalf("cow reply = %d, %v; want transaction 1 and the cow's compound", id, tag)
	}

	c.ChatCommand("execute in minecraft:the_nether run tp @s 2.5 64 2.5")
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.PlayerDimension("Alice") == world.NetherName })
	sendEntityTagQuery(c, 2, cow.EntityID)
	sendEntityTagQuery(c, 3, c.PlayerEntityID)
	if id, _ = readTagQuery(t, c.AwaitPlay(protocol.IDPlayTagQuery)); id != 3 {
		t.Errorf("a cow in another dimension was answered: next reply is transaction %d, want 3", id)
	}
}

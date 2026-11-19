package server

import (
	"github.com/golem-mc/golem/registry"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/commandblock"
	"github.com/golem-mc/golem/internal/server/entity"
)

// TestCommandMinecartMetadataFollowsItsCommandAndOutput: the synched command
// and last output change with programming and with a run's output, and the
// delta carries each only when it changed.
func TestCommandMinecartMetadataFollowsItsCommandAndOutput(t *testing.T) {
	it := &minecartEntity{cart: entity.NewMinecart(7, 0.5, 64, 0.5), kind: registry.EntityTypeCommandBlockMinecart}
	it.command = entity.NewCommandCart()
	it.displayOffset = it.kindInfo().DisplayOffset

	fresh := minecartMetaOf(it)
	if fresh.cmd != "" || fresh.output != emptyCommandOutput {
		t.Fatalf("fresh meta = %q, %q; want an empty command and the empty component", fresh.cmd, fresh.output)
	}

	it.command.Program("say hi", true)
	programmed := minecartMetaOf(it)
	if programmed.cmd != "say hi" || programmed.output != emptyCommandOutput {
		t.Fatalf("programmed meta = %q, %q", programmed.cmd, programmed.output)
	}
	if delta := minecartDeltaMetadata(it.cart.ID, &fresh, &programmed); len(delta) == 0 {
		t.Fatal("programming sent no delta")
	}

	out := protocol.Text{Text: "hi"}
	it.command.End(10, commandblock.Run, 1, &out)
	ran := minecartMetaOf(it)
	if ran.output == emptyCommandOutput {
		t.Fatal("the run's output did not reach the metadata")
	}
	if ran.cmd != "say hi" {
		t.Fatalf("cmd = %q after a run", ran.cmd)
	}

	it.command.Program("say hi", false)
	if got := minecartMetaOf(it); got.output != emptyCommandOutput {
		t.Fatal("untracking output left the old output in the metadata")
	}
}

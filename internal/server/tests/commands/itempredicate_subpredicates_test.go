package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestCommandItemPredicatesMatchComponentPredicateTypes checks that '~' on a
// component with a predicate type of its own runs that predicate in
// /execute if items.
// vanilla: ItemPredicateArgument, DataComponentPredicates
func TestCommandItemPredicatesMatchComponentPredicateTypes(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	hold := func(stack string) {
		t.Helper()
		c.ChatCommand("item replace entity @s weapon.mainhand with " + stack)
		if _, chats := servertest.AwaitSlotUpdates(t, c, 1); len(chats) == 0 {
			c.ExpectPlay(protocol.IDPlaySystemChat)
		}
	}
	answers := func(predicate string, want bool) {
		t.Helper()
		command := "execute if items entity @s weapon.mainhand " + predicate
		if want {
			command += " run say hi"
		}
		c.ChatCommand(command)
		if want {
			if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
				t.Errorf("%s answered % X, want the say", predicate, got)
			}
			return
		}
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
			t.Errorf("%s answered % X, want the conditional failure", predicate, got)
		}
	}

	gold := `*[minecraft:trim~{material:"minecraft:gold"}]`
	hold(`minecraft:iron_helmet[trim={material:"minecraft:gold",pattern:"minecraft:coast"}]`)
	answers(gold, true)
	hold(`minecraft:iron_helmet[trim={material:"minecraft:iron",pattern:"minecraft:coast"}]`)
	answers(gold, false)

	diamond := `*[container~{items:{contains:[{items:"minecraft:diamond"}]}}]`
	hold(`minecraft:shulker_box[container=[{slot:0,item:{id:"minecraft:diamond"}}]]`)
	answers(diamond, true)
	hold(`minecraft:shulker_box`)
	answers(diamond, false)

	book := `*[written_book_content~{author:"x"}]`
	hold(`minecraft:written_book[written_book_content={title:"t",author:"x"}]`)
	answers(book, true)
	hold(`minecraft:written_book[written_book_content={title:"t",author:"y"}]`)
	answers(book, false)
}

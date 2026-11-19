package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/commandblock"
)

// CommandCartDelay is the ticks between two runs on an activator rail.
// vanilla: MinecartCommandBlock.ACTIVATION_DELAY
const CommandCartDelay = 4

// CommandCart is a command block minecart's BaseCommandBlock and its
// activator-rail bookkeeping.
type CommandCart struct {
	commandblock.State
	// Age counts the ticks the cart has run, and LastActivated is the age of
	// its last run. vanilla: Entity.tickCount, MinecartCommandBlock.lastActivated
	Age, LastActivated int

	// output is LastOutput as the network NBT the metadata carries, valid
	// while outputOf is the LastOutput it was built from.
	output   string
	outputOf *protocol.Text
	outputOK bool
}

// NewCommandCart is a cart with no command and no run yet.
func NewCommandCart() *CommandCart {
	return &CommandCart{State: commandblock.New()}
}

// Output is the last output as the network NBT the metadata carries; the
// empty text when there is none.
func (c *CommandCart) Output() string {
	if !c.outputOK || c.outputOf != c.LastOutput {
		var b protocol.Buffer
		if c.LastOutput == nil {
			b.PutText(protocol.Text{})
		} else {
			b.PutText(*c.LastOutput)
		}
		c.output, c.outputOf, c.outputOK = string(b.Bytes()), c.LastOutput, true
	}
	return c.output
}

// Activate books a run when four ticks have passed since the last, and
// reports whether one is due.
// vanilla: MinecartCommandBlock.activateMinecart
func (c *CommandCart) Activate() bool {
	if c.Age-c.LastActivated < CommandCartDelay {
		return false
	}
	c.LastActivated = c.Age
	return true
}

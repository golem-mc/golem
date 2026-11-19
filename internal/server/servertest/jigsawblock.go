package servertest

import "github.com/golem-mc/golem/internal/protocol"

// JigsawBlockPacket is the fields of the Set Jigsaw Block packet.
// vanilla: ServerboundSetJigsawBlockPacket
type JigsawBlockPacket struct {
	X, Y, Z                      int32
	Name, Target, Pool           string
	FinalState, Joint            string
	SelectionPriority, Placement int32
}

// SetJigsawBlock sends the Set Jigsaw Block packet.
func (c *Client) SetJigsawBlock(p JigsawBlockPacket) {
	c.T.Helper()
	c.Send(protocol.IDPlaySetJigsawBlockIn, func(b *protocol.Buffer) {
		b.PutPosition(p.X, p.Y, p.Z)
		b.PutString(p.Name)
		b.PutString(p.Target)
		b.PutString(p.Pool)
		b.PutString(p.FinalState)
		b.PutString(p.Joint)
		b.PutVarInt(p.SelectionPriority)
		b.PutVarInt(p.Placement)
	})
}

// JigsawGenerate sends the Jigsaw Generate packet.
// vanilla: ServerboundJigsawGeneratePacket
func (c *Client) JigsawGenerate(x, y, z, levels int32, keepJigsaws bool) {
	c.T.Helper()
	c.Send(protocol.IDPlayJigsawGenerateIn, func(b *protocol.Buffer) {
		b.PutPosition(x, y, z)
		b.PutVarInt(levels)
		b.PutBool(keepJigsaws)
	})
}

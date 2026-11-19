package servertest

import "github.com/golem-mc/golem/internal/protocol"

// StructureBlockPacket is the fields of the Program Structure Block packet.
// vanilla: ServerboundSetStructureBlockPacket
type StructureBlockPacket struct {
	X, Y, Z                          int32
	Action, Mode                     int32
	Name                             string
	Offset, Size                     [3]int8
	Mirror, Rotation                 int32
	Metadata                         string
	Integrity                        float32
	Seed                             int64
	IgnoreEntities, ShowAir, ShowBox bool
	Strict                           bool
}

// The packet's update types and modes, the ordinals of the wire enums.
const (
	StructureActionUpdate = 0
	StructureActionSave   = 1
	StructureActionLoad   = 2
	StructureActionScan   = 3

	StructureModeSave   = 0
	StructureModeLoad   = 1
	StructureModeCorner = 2
	StructureModeData   = 3
)

// ProgramStructureBlock sends the Program Structure Block packet.
func (c *Client) ProgramStructureBlock(p StructureBlockPacket) {
	c.T.Helper()
	c.Send(protocol.IDPlaySetStructureBlockIn, func(b *protocol.Buffer) {
		b.PutPosition(p.X, p.Y, p.Z)
		b.PutVarInt(p.Action)
		b.PutVarInt(p.Mode)
		b.PutString(p.Name)
		for _, v := range p.Offset {
			b.PutU8(uint8(v))
		}
		for _, v := range p.Size {
			b.PutU8(uint8(v))
		}
		b.PutVarInt(p.Mirror)
		b.PutVarInt(p.Rotation)
		b.PutString(p.Metadata)
		b.PutF32(p.Integrity)
		b.PutVarLong(p.Seed)
		var flags uint8
		for i, on := range []bool{p.IgnoreEntities, p.ShowAir, p.ShowBox, p.Strict} {
			if on {
				flags |= 1 << i
			}
		}
		b.PutU8(flags)
	})
}

// SetTestBlock sends the Set Test Block packet; mode is the wire ordinal.
func (c *Client) SetTestBlock(x, y, z, mode int32, message string) {
	c.T.Helper()
	c.Send(protocol.IDPlaySetTestBlockIn, func(b *protocol.Buffer) {
		b.PutPosition(x, y, z)
		b.PutVarInt(mode)
		b.PutString(message)
	})
}

// TestInstanceData is the test instance block's settings as the Test
// Instance Block Action packet carries them; Test "" sends none.
// vanilla: TestInstanceBlockEntity.Data
type TestInstanceData struct {
	Test             string
	Size             [3]int32
	Rotation, Status int32
	IgnoreEntities   bool
	ErrorMessage     *protocol.Text
}

// The Test Instance Block Action packet's actions, the wire ids.
const (
	TestInstanceInit   = 0
	TestInstanceQuery  = 1
	TestInstanceSet    = 2
	TestInstanceReset  = 3
	TestInstanceSave   = 4
	TestInstanceExport = 5
	TestInstanceRun    = 6
)

// TestInstanceBlockAction sends the Test Instance Block Action packet.
func (c *Client) TestInstanceBlockAction(x, y, z, action int32, d TestInstanceData) {
	c.T.Helper()
	c.Send(protocol.IDPlayTestInstanceBlockActionIn, func(b *protocol.Buffer) {
		b.PutPosition(x, y, z)
		b.PutVarInt(action)
		b.PutBool(d.Test != "")
		if d.Test != "" {
			b.PutString(d.Test)
		}
		for _, v := range d.Size {
			b.PutVarInt(v)
		}
		b.PutVarInt(d.Rotation)
		b.PutBool(d.IgnoreEntities)
		b.PutVarInt(d.Status)
		b.PutBool(d.ErrorMessage != nil)
		if d.ErrorMessage != nil {
			b.PutText(*d.ErrorMessage)
		}
	})
}

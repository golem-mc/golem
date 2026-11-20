package plugintest

import (
	"errors"
	"strings"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// Player is a connected test client.
type Player struct {
	s    *Server
	c    *servertest.Client
	w    *servertest.WindowedClient
	name string

	seen   seen
	closed bool
	kick   *string
}

// seen is what the player has been sent and the test has not read yet.
type seen struct {
	system     []string
	chat       []ChatLine
	actionBars []string
	titles     []string
	subtitles  []string
	explosions []Explosion
	sounds     []Sound
}

// ChatLine is a chat message from another player.
type ChatLine struct {
	Sender  plugin.UUID
	Message string
}

// Explosion is an Explosion packet.
type Explosion struct {
	X, Y, Z float64
	Radius  float32
	// Blocks is the number of blocks the blast destroys.
	Blocks int
}

// Sound is a Sound Effect packet.
type Sound struct {
	// Name is the sound event, such as "minecraft:entity.generic.explode".
	Name     string
	Category int32
	X, Y, Z  float64
	Volume   float32
	Pitch    float32
}

// Name is the player's name.
func (p *Player) Name() string { return p.name }

// Handle is the live plugin handle of the joined player.
func (p *Player) Handle() plugin.Player {
	p.s.t.Helper()
	h, ok := p.s.host.PlayerNamed(p.name)
	if !ok {
		p.s.t.Fatalf("plugintest: %s is not online", p.name)
	}
	return h
}

// Op makes the player an operator at the default level and waits for the
// server to have applied it.
func (p *Player) Op() {
	p.s.t.Helper()
	p.s.Console("op " + p.name)
	p.c.SettleKeeping()
}

// send runs an action the client performs; the server has read it once the
// call returns, and the tick that acts on it is the next reader's to run.
func (p *Player) send(act func(c *servertest.Client)) {
	p.s.t.Helper()
	act(p.c)
	p.c.SettleKeeping()
}

// Run sends a command (a line starting with "/") or a chat message as the
// player. Read the reply with Messages.
func (p *Player) Run(line string) {
	p.s.t.Helper()
	p.send(func(c *servertest.Client) {
		if cmd, ok := strings.CutPrefix(line, "/"); ok {
			c.ChatCommand(cmd)
		} else {
			c.Chat(line)
		}
	})
}

// PlaceBlock right-clicks the block at (x, y, z) with the main hand, against
// face (0 down, 1 up, 2 north, 3 south, 4 west, 5 east), the cursor at the
// face's centre.
func (p *Player) PlaceBlock(x, y, z, face int32) {
	p.s.t.Helper()
	p.send(func(c *servertest.Client) { c.PlaceBlock(x, y, z, face) })
}

// StartBreak sends the start of a dig of the block at (x, y, z). Creative
// breaks on it; a survival dig needs FinishBreak and enough ticks.
func (p *Player) StartBreak(x, y, z int32) {
	p.s.t.Helper()
	p.send(func(c *servertest.Client) { c.StartDigging(x, y, z) })
}

// FinishBreak sends the finished-digging report for the block at (x, y, z).
func (p *Player) FinishBreak(x, y, z int32) {
	p.s.t.Helper()
	p.send(func(c *servertest.Client) { c.StopDigging(x, y, z) })
}

// Break is StartBreak followed by FinishBreak.
func (p *Player) Break(x, y, z int32) {
	p.s.t.Helper()
	p.send(func(c *servertest.Client) { c.BreakBlock(x, y, z) })
}

// Attack hits the entity with the held item. Entity.ID gives the ID.
func (p *Player) Attack(entityID int32) {
	p.s.t.Helper()
	p.send(func(c *servertest.Client) { c.Attack(entityID) })
}

// Swing swings the main arm, a left click in the air when the player looks at
// no block in reach.
func (p *Player) Swing() {
	p.s.t.Helper()
	p.send(func(c *servertest.Client) { c.Swing() })
}

// Interact right-clicks the entity with the main hand, at its centre.
func (p *Player) Interact(entityID int32) {
	p.s.t.Helper()
	p.InteractAt(entityID, 0, 0, 0)
}

// InteractAt right-clicks the entity with the main hand at an offset from its
// position, which is what picks the part of an armour stand that is hit.
func (p *Player) InteractAt(entityID int32, dx, dy, dz float64) {
	p.s.t.Helper()
	p.send(func(c *servertest.Client) { c.InteractAt(entityID, protocol.HandMain, dx, dy, dz) })
}

// UseItem right-clicks in the air with the main hand.
func (p *Player) UseItem() {
	p.s.t.Helper()
	p.send(func(c *servertest.Client) { c.UseItemRightClicks(protocol.HandMain) })
}

// SelectHotbar makes hotbar slot 0-8 the held one.
func (p *Player) SelectHotbar(slot int) {
	p.s.t.Helper()
	p.send(func(c *servertest.Client) { c.SwapHotbarSlot(int16(slot)) })
}

// Click clicks a slot of the open window (the player's own inventory when
// none is open) with the window's own numbering. button and mode are the Click
// Container packet's: (0, 0) is a left click, (1, 0) a right click, (0, 1) a
// shift-click, (n, 2) a number key, (0, 4) a drop.
func (p *Player) Click(slot int16, button int8, mode int32) {
	p.s.t.Helper()
	p.send(func(*servertest.Client) { p.w.Click(slot, button, mode) })
}

// Drag spreads the cursor stack over slots with the left button (or the right
// one when right is set), as the packets of a drag do.
func (p *Player) Drag(right bool, slots ...int16) {
	p.s.t.Helper()
	start, add, end := int8(0), int8(1), int8(2)
	if right {
		start, add, end = 4, 5, 6
	}
	p.send(func(*servertest.Client) {
		p.w.Click(-999, start, 5)
		for _, slot := range slots {
			p.w.Click(slot, add, 5)
		}
		p.w.Click(-999, end, 5)
	})
}

// CloseWindow closes the open window.
func (p *Player) CloseWindow() {
	p.s.t.Helper()
	p.send(func(*servertest.Client) { p.w.CloseWindow() })
	p.w.Window = 0
}

// Disconnect leaves the way a client does and returns once the server has
// finished with the player: they are off the player list.
func (p *Player) Disconnect() {
	p.s.t.Helper()
	p.closed = true
	p.c.Disconnect()
}

// Kicked reports the reason the server disconnected the player with, once a
// read has seen it.
func (p *Player) Kicked() (reason string, ok bool) {
	if p.kick == nil {
		return "", false
	}
	return *p.kick, true
}

// Read runs ticks ticks and keeps what the player is sent for the readers
// below. It is what the others do first.
func (p *Player) Read(ticks int) {
	p.s.t.Helper()
	p.drain()
	p.s.RunTicks(ticks)
	p.drain()
}

// Messages runs ticks ticks and returns the system messages the player was
// sent that no earlier call returned, as plain text, oldest first. A player
// who has been kicked or has left gets what arrived before.
func (p *Player) Messages(ticks int) []string {
	p.s.t.Helper()
	p.Read(ticks)
	out := p.seen.system
	p.seen.system = nil
	return out
}

// Chat runs ticks ticks and returns the chat messages other players sent that
// no earlier call returned.
func (p *Player) Chat(ticks int) []ChatLine {
	p.s.t.Helper()
	p.Read(ticks)
	out := p.seen.chat
	p.seen.chat = nil
	return out
}

// ActionBars runs ticks ticks and returns the action bar texts sent, as plain text.
func (p *Player) ActionBars(ticks int) []string {
	p.s.t.Helper()
	p.Read(ticks)
	out := p.seen.actionBars
	p.seen.actionBars = nil
	return out
}

// Titles runs ticks ticks and returns the title and subtitle texts sent, as
// plain text.
func (p *Player) Titles(ticks int) (titles, subtitles []string) {
	p.s.t.Helper()
	p.Read(ticks)
	titles, subtitles = p.seen.titles, p.seen.subtitles
	p.seen.titles, p.seen.subtitles = nil, nil
	return titles, subtitles
}

// Explosions runs ticks ticks and returns the Explosion packets sent.
func (p *Player) Explosions(ticks int) []Explosion {
	p.s.t.Helper()
	p.Read(ticks)
	out := p.seen.explosions
	p.seen.explosions = nil
	return out
}

// Sounds runs ticks ticks and returns the Sound Effect packets sent. Other
// players' footsteps are included.
func (p *Player) Sounds(ticks int) []Sound {
	p.s.t.Helper()
	p.Read(ticks)
	out := p.seen.sounds
	p.seen.sounds = nil
	return out
}

// drain reads what the server has sent up to a ping the server answers in
// order, so what an earlier send caused has arrived. It stops quietly once the
// connection is closed by the server, which is how a kick ends.
func (p *Player) drain() {
	p.s.t.Helper()
	if p.closed {
		return
	}
	var ping protocol.Buffer
	ping.PutI64(servertest.TickBarrierPing)
	if err := p.c.Conn.WritePacket(protocol.IDPlayPingRequest, ping.Bytes()); err != nil {
		p.closed = true
		return
	}
	deadline := time.Now().Add(servertest.ExpectTimeout)
	for {
		id, data, err := p.c.ReadBy(deadline)
		if err != nil {
			if errors.Is(err, servertest.ErrWaitOver) {
				p.s.t.Fatalf("plugintest: no answer to the ping within %s", servertest.ExpectTimeout)
			}
			// The server closed the connection: a kick, or its end.
			p.closed = true
			return
		}
		switch {
		case servertest.IsPingAnswer(id, data, servertest.TickBarrierPing):
			return
		case id == protocol.IDPlayKeepAlive:
			p.c.AnswerKeepAlive(data)
		default:
			p.note(id, data)
		}
	}
}

// note files one packet under what the readers return.
func (p *Player) note(id int32, data []byte) {
	r := protocol.NewReader(data)
	switch id {
	case protocol.IDPlaySystemChat:
		if text, ok := protocol.ReadText(r); ok {
			p.seen.system = append(p.seen.system, text.Plain())
		}
	case protocol.IDPlayActionBarText:
		if text, ok := protocol.ReadText(r); ok {
			p.seen.actionBars = append(p.seen.actionBars, text.Plain())
		}
	case protocol.IDPlayTitleText:
		if text, ok := protocol.ReadText(r); ok {
			p.seen.titles = append(p.seen.titles, text.Plain())
		}
	case protocol.IDPlaySubtitleText:
		if text, ok := protocol.ReadText(r); ok {
			p.seen.subtitles = append(p.seen.subtitles, text.Plain())
		}
	case protocol.IDPlayPlayerChat:
		r.ReadVarInt() // global index
		sender := r.ReadUUID()
		r.ReadVarInt() // chain index
		r.ReadBool()   // signed
		message := r.ReadString(256)
		if r.Err() == nil {
			p.seen.chat = append(p.seen.chat, ChatLine{Sender: sender, Message: message})
		}
	case protocol.IDPlayExplosion:
		e := servertest.ReadExplosion(p.c, data)
		p.seen.explosions = append(p.seen.explosions, Explosion{X: e.X, Y: e.Y, Z: e.Z, Radius: e.Radius, Blocks: int(e.BlockCount)})
	case protocol.IDPlaySoundEffect:
		p.noteSound(r)
	case protocol.IDPlayDisconnect:
		reason := ""
		if text, ok := protocol.ReadText(r); ok {
			reason = text.Plain()
		}
		p.kick = &reason
	case protocol.IDPlayOpenScreen:
		p.w.Window = r.ReadVarInt()
	case protocol.IDPlayCloseContainer:
		p.w.Window = 0
	case protocol.IDPlayContainerContent, protocol.IDPlayContainerSlot:
		window := r.ReadVarInt()
		p.w.TrackState(window, r.ReadVarInt())
	}
}

func (p *Player) noteSound(r *protocol.Reader) {
	var s Sound
	if id := r.ReadVarInt(); id == 0 {
		s.Name = r.ReadString(256)
		if r.ReadBool() {
			r.ReadF32() // fixed range
		}
	} else if name, ok := servertest.SoundEventName(id - 1); ok {
		s.Name = name
	}
	s.Category = r.ReadVarInt()
	s.X, s.Y, s.Z = float64(r.ReadI32())/8, float64(r.ReadI32())/8, float64(r.ReadI32())/8
	s.Volume, s.Pitch = r.ReadF32(), r.ReadF32()
	if r.Err() == nil {
		p.seen.sounds = append(p.seen.sounds, s)
	}
}

// ShootTippedArrow launches an arrow from the player's eyes along their look
// direction, tipped with the potion. ProjectileLaunchEvent fires before the
// tipping, so a listener sees a plain arrow; an error means a listener
// cancelled the launch or the potion is unknown.
func (p *Player) ShootTippedArrow(potion plugin.Key) (plugin.Arrow, error) {
	p.s.t.Helper()
	var arrow plugin.Arrow
	var err error
	p.s.Run(func() {
		var proj plugin.Projectile
		if proj, err = p.Handle().LaunchProjectile(registry.EntityTypeArrow, plugin.Vec3{}); err != nil {
			return
		}
		a, ok := proj.(plugin.Arrow)
		if !ok || !a.SetPotion(potion) {
			proj.Remove()
			err = errors.New("plugintest: unknown potion " + potion.String())
			return
		}
		arrow = a
	})
	return arrow, err
}

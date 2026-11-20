package main

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// countingConn counts the bytes read off the wire, before decompression.
type countingConn struct {
	net.Conn
	read *atomic.Int64
}

func (c countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.read.Add(int64(n))
	return n, err
}

// behaviour is what a bot does each client tick, once the server has placed it.
type behaviour interface {
	// step returns the position to report this tick, and whether it moved.
	step(b *bot, tick int) (x, y, z float64, yaw float32, onGround, moved bool)
}

// observer is a behaviour that also sees every play packet, on the bot's
// reading goroutine.
type observer interface {
	observe(id int32, data []byte)
}

// bot is one offline-mode client in the play state.
type bot struct {
	name     string
	conn     *protocol.Conn
	raw      net.Conn
	port     uint16 // the server's, for the handshake
	bytesIn  atomic.Int64
	packets  atomic.Int64
	viewAsk  int8
	behave   behaviour
	onSample func(sample []int64) // debug samples, the observer's only
	idCounts map[int32]int        // packets by ID, when set (the observer's)
	trace    bool                 // LOADTEST_TRACE names this bot: log its world packets

	// tickMu keeps a teleport's confirm from landing between a tick's step
	// and the move it sends, which would report the old position after it.
	tickMu sync.Mutex

	mu        sync.Mutex
	placed    bool // the first Synchronize Player Position has been confirmed
	x, y, z   float64
	yaw       float32
	chunks    map[[2]int32]bool // loaded on the client
	chunkLog  []chunkArrival    // arrivals since the last mark
	markTime  time.Time
	markAfter bool // a teleport after mark() resets markTime to its arrival
	chunksIn  atomic.Int64
	dead      atomic.Bool

	err  atomic.Pointer[error]
	done chan struct{}
}

type chunkArrival struct {
	at   time.Duration // since markTime
	x, z int32
}

// dialBot connects, logs in and configures; the play loop starts with run.
func dialBot(addr, name string, viewAsk int8, behave behaviour) (*bot, error) {
	_, ps, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(ps, 10, 16)
	if err != nil {
		return nil, err
	}
	raw, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	b := &bot{name: name, raw: raw, port: uint16(port), viewAsk: viewAsk, behave: behave, chunks: map[[2]int32]bool{}, done: make(chan struct{})}
	b.conn = protocol.NewConn(countingConn{Conn: raw, read: &b.bytesIn})
	b.trace = os.Getenv("LOADTEST_TRACE") == name
	_ = raw.SetDeadline(time.Now().Add(3 * time.Minute))
	if err := b.login(); err != nil {
		raw.Close()
		return nil, fmt.Errorf("%s login: %w", name, err)
	}
	if err := b.configure(); err != nil {
		raw.Close()
		return nil, fmt.Errorf("%s configure: %w", name, err)
	}
	_ = raw.SetDeadline(time.Time{})
	return b, nil
}

func (b *bot) send(id int32, fill func(*protocol.Buffer)) {
	var buf protocol.Buffer
	if fill != nil {
		fill(&buf)
	}
	if err := b.conn.WritePacket(id, buf.Bytes()); err != nil {
		b.fail(err)
	}
}

func (b *bot) fail(err error) {
	b.err.CompareAndSwap(nil, &err)
	b.raw.Close()
}

// Err is why the bot stopped, nil while it plays.
func (b *bot) Err() error {
	if p := b.err.Load(); p != nil {
		return *p
	}
	return nil
}

func (b *bot) login() error {
	b.send(protocol.IDHandshake, func(w *protocol.Buffer) {
		w.PutVarInt(protocol.Version)
		w.PutString("localhost")
		w.PutI16(int16(b.port))
		w.PutVarInt(protocol.NextStateLogin)
	})
	b.send(protocol.IDLoginStart, func(w *protocol.Buffer) {
		w.PutString(b.name)
		w.PutUUID(protocol.OfflineUUID(b.name))
	})
	for {
		id, data, err := b.conn.ReadPacket()
		if err != nil {
			return err
		}
		switch id {
		case protocol.IDSetCompression:
			b.conn.EnableCompression(int(protocol.NewReader(data).ReadVarInt()))
		case protocol.IDLoginSuccess:
			b.send(protocol.IDLoginAcknowledged, nil)
			return nil
		case protocol.IDLoginDisconnect:
			return fmt.Errorf("disconnected: %s", protocol.NewReader(data).ReadString(32767))
		case protocol.IDEncryptionRequest:
			return errors.New("server wants encryption: online-mode must be off")
		}
	}
}

// configure answers the configuration phase in whatever order the server
// sends it, the way tools/probe does.
func (b *bot) configure() error {
	for {
		id, data, err := b.conn.ReadPacket()
		if err != nil {
			return err
		}
		switch id {
		case protocol.IDKnownPacks:
			r := protocol.NewReader(data)
			var reply protocol.Buffer
			n := r.ReadVarInt()
			reply.PutVarInt(n)
			for range n {
				reply.PutString(r.ReadString(32767))
				reply.PutString(r.ReadString(32767))
				reply.PutString(r.ReadString(32767))
			}
			if err := b.conn.WritePacket(protocol.IDKnownPacksIn, reply.Bytes()); err != nil {
				return err
			}
			b.send(protocol.IDClientInformation, func(w *protocol.Buffer) {
				w.PutString("en_us")
				w.PutI8(b.viewAsk)
				w.PutVarInt(0)   // chat mode
				w.PutBool(true)  // chat colors
				w.PutU8(0x7F)    // skin parts
				w.PutVarInt(1)   // main hand
				w.PutBool(false) // text filtering
				w.PutBool(true)  // listed
				w.PutVarInt(0)   // particles
			})
		case protocol.IDConfigKeepAlive:
			if err := b.conn.WritePacket(protocol.IDConfigKeepAliveIn, data); err != nil {
				return err
			}
		case protocol.IDConfigPing:
			if err := b.conn.WritePacket(protocol.IDConfigPong, data); err != nil {
				return err
			}
		case protocol.IDConfigCodeOfConduct:
			b.send(protocol.IDConfigAcceptCodeOfConduct, nil)
		case protocol.IDConfigFinish:
			b.send(protocol.IDConfigFinishAck, nil)
			return nil
		case protocol.IDConfigDisconnect:
			return errors.New("disconnected during configuration")
		}
	}
}

// run reads the play state until the connection closes, and ticks the
// behaviour at 20 Hz.
func (b *bot) run() {
	go b.tickLoop()
	defer close(b.done)
	for {
		id, data, err := b.conn.ReadPacket()
		if err != nil {
			b.fail(err)
			return
		}
		b.packets.Add(1)
		if b.trace {
			b.traceOf(id, data)
		}
		if o, ok := b.behave.(observer); ok {
			o.observe(id, data)
		}
		if b.idCounts != nil {
			b.mu.Lock()
			b.idCounts[id]++
			b.mu.Unlock()
		}
		switch id {
		case protocol.IDPlayKeepAlive:
			b.send(protocol.IDPlayKeepAliveIn, func(w *protocol.Buffer) { w.PutRaw(data) })
		case protocol.IDPlayPing:
			b.send(protocol.IDPlayPong, func(w *protocol.Buffer) { w.PutRaw(data) })
		case protocol.IDPlaySyncPosition:
			b.teleported(data)
		case protocol.IDPlayChunkBatchFinished:
			// The most a server takes: the bot never holds the stream back.
			b.send(protocol.IDPlayChunkBatchReceived, func(w *protocol.Buffer) { w.PutF32(64) })
		case protocol.IDPlayChunkData:
			r := protocol.NewReader(data)
			x, z := r.ReadI32(), r.ReadI32()
			b.chunksIn.Add(1)
			b.mu.Lock()
			b.chunks[[2]int32{x, z}] = true
			if !b.markTime.IsZero() {
				b.chunkLog = append(b.chunkLog, chunkArrival{at: time.Since(b.markTime), x: x, z: z})
			}
			b.mu.Unlock()
		case protocol.IDPlayUnloadChunk:
			r := protocol.NewReader(data)
			z, x := r.ReadI32(), r.ReadI32()
			b.mu.Lock()
			delete(b.chunks, [2]int32{x, z})
			b.mu.Unlock()
		case protocol.IDPlaySetHealth:
			if protocol.NewReader(data).ReadF32() <= 0 {
				b.dead.Store(true)
				b.send(protocol.IDPlayClientCommandIn, func(w *protocol.Buffer) {
					w.PutVarInt(protocol.ClientCommandPerformRespawn)
				})
			}
		case protocol.IDPlayDebugSample:
			if b.onSample != nil {
				r := protocol.NewReader(data)
				n := r.ReadVarInt()
				s := make([]int64, 0, max(n, 0))
				for i := int32(0); i < n && r.Err() == nil; i++ {
					s = append(s, r.ReadI64())
				}
				if r.Err() == nil {
					b.onSample(s)
				}
			}
		case protocol.IDPlayDisconnect:
			b.fail(errors.New("kicked"))
			return
		}
	}
}

func (b *bot) traceOf(id int32, data []byte) {
	r := protocol.NewReader(data)
	at := time.Now().Format("15:04:05.000")
	switch id {
	case protocol.IDPlayChunkData:
		fmt.Fprintf(os.Stderr, "%s %s chunk %d %d (%d bytes)\n", at, b.name, r.ReadI32(), r.ReadI32(), len(data))
	case protocol.IDPlayUnloadChunk:
		z, x := r.ReadI32(), r.ReadI32()
		fmt.Fprintf(os.Stderr, "%s %s unload %d %d\n", at, b.name, x, z)
	case protocol.IDPlaySyncPosition:
		r.ReadVarInt()
		fmt.Fprintf(os.Stderr, "%s %s teleport %.1f %.1f %.1f\n", at, b.name, r.ReadF64(), r.ReadF64(), r.ReadF64())
	case protocol.IDPlayCenterChunk:
		fmt.Fprintf(os.Stderr, "%s %s center %d %d\n", at, b.name, r.ReadVarInt(), r.ReadVarInt())
	case protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished:
		fmt.Fprintf(os.Stderr, "%s %s batch 0x%02X %x\n", at, b.name, id, data)
	}
}

func (b *bot) teleported(data []byte) {
	r := protocol.NewReader(data)
	tid := r.ReadVarInt()
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	r.ReadF64()
	r.ReadF64()
	r.ReadF64()
	yaw, pitch := r.ReadF32(), r.ReadF32()
	flags := r.ReadI32()
	b.tickMu.Lock()
	defer b.tickMu.Unlock()
	// Runs after b.mu is released and sends the resolved position.
	defer b.send(protocol.IDPlayConfirmTeleport, func(w *protocol.Buffer) {
		w.PutVarInt(tid)
		w.PutF64(x)
		w.PutF64(y)
		w.PutF64(z)
		w.PutF32(yaw)
		w.PutF32(pitch)
	})
	b.mu.Lock()
	defer b.mu.Unlock()
	if flags&0x01 != 0 {
		x += b.x
	}
	if flags&0x02 != 0 {
		y += b.y
	}
	if flags&0x04 != 0 {
		z += b.z
	}
	if flags&0x08 != 0 {
		yaw += b.yaw
	}
	b.x, b.y, b.z, b.yaw = x, y, z, yaw
	first := !b.placed
	b.placed = true
	if b.markAfter {
		b.markAfter = false
		b.markTime = time.Now()
		b.chunkLog = b.chunkLog[:0]
	}
	if first {
		go func() {
			b.send(protocol.IDPlayLoaded, nil)
		}()
	}
}

func (b *bot) tickLoop() {
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	tick := 0
	for {
		select {
		case <-b.done:
			return
		case <-t.C:
		}
		if b.Err() != nil {
			return
		}
		b.mu.Lock()
		placed := b.placed
		b.mu.Unlock()
		if !placed || b.behave == nil {
			continue
		}
		if b.dead.Swap(false) {
			continue
		}
		tick++
		b.tickMu.Lock()
		x, y, z, yaw, ground, moved := b.behave.step(b, tick)
		if moved || tick%20 == 0 {
			var flags uint8
			if ground {
				flags = 1
			}
			b.send(protocol.IDPlayPositionAndRotation, func(w *protocol.Buffer) {
				w.PutF64(x)
				w.PutF64(y)
				w.PutF64(z)
				w.PutF32(yaw)
				w.PutF32(0)
				w.PutU8(flags)
			})
		}
		b.send(protocol.IDPlayClientTickEnd, nil)
		b.tickMu.Unlock()
	}
}

// waitPlaced waits for the server to have put the bot in the world.
func (b *bot) waitPlaced(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := b.Err(); err != nil {
			return fmt.Errorf("%s: %w", b.name, err)
		}
		b.mu.Lock()
		placed := b.placed
		b.mu.Unlock()
		if placed {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("%s not placed within %s", b.name, timeout)
}

// waitChunks waits until the client holds n chunks.
func (b *bot) waitChunks(n int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		have := len(b.chunks)
		b.mu.Unlock()
		if have >= n {
			time.Sleep(2 * time.Second) // let the last ones finish loading server-side
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("%s: fewer than %d chunks within %s", b.name, n, timeout)
}

// pos is where the bot's body is.
func (b *bot) pos() (x, y, z float64, yaw float32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.x, b.y, b.z, b.yaw
}

func (b *bot) setPos(x, y, z float64, yaw float32) {
	b.mu.Lock()
	b.x, b.y, b.z, b.yaw = x, y, z, yaw
	b.mu.Unlock()
}

// markOnTeleport starts a new chunk log at the next teleport's arrival.
func (b *bot) markOnTeleport() {
	b.mu.Lock()
	b.markAfter = true
	b.markTime = time.Time{}
	b.chunkLog = nil
	b.mu.Unlock()
}

// arrivals returns the chunk log since the mark and the chunk the bot stands in.
func (b *bot) arrivals() ([]chunkArrival, int32, int32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]chunkArrival(nil), b.chunkLog...), int32(math.Floor(b.x / 16)), int32(math.Floor(b.z / 16))
}

// viewCount counts the distinct chunks logged since the mark within radius
// (Chebyshev) of the bot.
func (b *bot) viewCount(radius int32) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	cx, cz := int32(math.Floor(b.x/16)), int32(math.Floor(b.z/16))
	seen := map[[2]int32]bool{}
	for _, a := range b.chunkLog {
		if abs32(a.x-cx) <= radius && abs32(a.z-cz) <= radius {
			seen[[2]int32{a.x, a.z}] = true
		}
	}
	return len(seen)
}

// missingNear counts the chunks within radius (Chebyshev) of the bot that
// the client does not hold.
func (b *bot) missingNear(radius int32) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	cx, cz := int32(math.Floor(b.x/16)), int32(math.Floor(b.z/16))
	n := 0
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			if !b.chunks[[2]int32{cx + dx, cz + dz}] {
				n++
			}
		}
	}
	return n
}

func (b *bot) close() { b.raw.Close() }

// idle stands still.
type idle struct{}

func (idle) step(b *bot, _ int) (float64, float64, float64, float32, bool, bool) {
	x, y, z, yaw := b.pos()
	return x, y, z, yaw, true, false
}

// walker wanders between random points within radius of the origin, on
// flat ground at the height the server placed it.
type walker struct {
	radius, speed float64 // blocks, blocks per tick
	rng           *rand.Rand
	tx, tz        float64
	has           bool
}

func (w *walker) step(b *bot, _ int) (float64, float64, float64, float32, bool, bool) {
	x, y, z, _ := b.pos()
	if !w.has || math.Hypot(w.tx-x, w.tz-z) < w.speed {
		w.tx = (w.rng.Float64()*2 - 1) * w.radius
		w.tz = (w.rng.Float64()*2 - 1) * w.radius
		w.has = true
	}
	dx, dz := w.tx-x, w.tz-z
	d := math.Hypot(dx, dz)
	x += dx / d * w.speed
	z += dz / d * w.speed
	yaw := float32(math.Atan2(-dx, dz) * 180 / math.Pi)
	b.setPos(x, y, z, yaw)
	return x, y, z, yaw, true, true
}

// flyer flies in a straight line along its heading once moving is set.
type flyer struct {
	dx, dz, speed float64
	moving        atomic.Bool
}

func (f *flyer) step(b *bot, _ int) (float64, float64, float64, float32, bool, bool) {
	x, y, z, _ := b.pos()
	yaw := float32(math.Atan2(-f.dx, f.dz) * 180 / math.Pi)
	if !f.moving.Load() {
		return x, y, z, yaw, false, false
	}
	x += f.dx * f.speed
	z += f.dz * f.speed
	b.setPos(x, y, z, yaw)
	return x, y, z, yaw, false, true
}

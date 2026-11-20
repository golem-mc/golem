package main

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// llamas summons n llamas in the pen and times a walking bot's round trips:
// a command to its answer, a ping request to its response (golem answers it
// in line with the bot's moves, vanilla on its network thread), and the
// keep-alive round trip the server reports in the tab list. Run golem built
// from ./tools/loadtest/movelistener to put a plugin listener behind every
// move, as the Essentials plugin does.
func llamas(n int) func(*env) error {
	return func(e *env) error {
		if err := e.flatStart("6000", ground+10); err != nil {
			return err
		}
		e.pen()
		rng := rand.New(rand.NewPCG(1, 2))
		for range n {
			x, z := rng.Float64()*58-29, rng.Float64()*58-29
			e.cmd(fmt.Sprintf("summon llama %.2f %d %.2f {PersistenceRequired:1b}", x, ground, z))
		}
		e.sum.Extra["count_after_summon"] = e.count("@e[type=llama]")
		lat := &latencies{}
		_, err := e.spawnBots(1, "lat", 10, int8(e.o.view), func(i int) behaviour {
			return &timedWalker{radius: 24, speed: walk, rng: rand.New(rand.NewPCG(uint64(i), 7)), lat: lat}
		})
		if err != nil {
			return err
		}
		time.Sleep(e.o.warmup)
		lat.reset()
		e.measure("main", e.o.measure, measureOpts{profile: true})
		e.sum.Extra["command_rtt_ms"] = lat.summary(&lat.command)
		e.sum.Extra["ping_rtt_ms"] = lat.summary(&lat.ping)
		e.sum.Extra["keepalive_rtt_ms"] = lat.summary(&lat.keepAlive)
		e.sum.Extra["count_after_measure"] = e.count("@e[type=llama]")
		return nil
	}
}

// latencies collects round trips; a command or ping still unanswered when
// the next is due counts from its own send, so a backlog shows as growth.
type latencies struct {
	mu            sync.Mutex
	command, ping []time.Duration
	keepAlive     []time.Duration // as the server reports it
	commandSent   []time.Time     // in order: a server answers commands in order
}

func (l *latencies) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.command, l.ping, l.keepAlive = nil, nil, nil
}

// answered records the round trip of the oldest command still unanswered.
func (l *latencies) answered() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.commandSent) > 0 {
		l.command = append(l.command, time.Since(l.commandSent[0]))
		l.commandSent = l.commandSent[1:]
	}
}

func (l *latencies) summary(of *[]time.Duration) map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := slices.Clone(*of)
	slices.Sort(d)
	if len(d) == 0 {
		return map[string]any{"n": 0}
	}
	ms := func(q float64) float64 { return float64(d[int(q*float64(len(d)-1))].Microseconds()) / 1000 }
	return map[string]any{"n": len(d), "p50": ms(0.5), "p95": ms(0.95), "max": ms(1)}
}

// latencyOnly is a Player Info Update's action set when it carries only
// latencies.
const latencyOnly = 0x10

// timedWalker walks every tick (20 moves a second), sends a ping once a
// second and a command every 5 s: vanilla's spam throttle adds 20 a command
// and takes 1 a tick, so a command a second gets the bot kicked once ticks
// fall below 20 a second.
type timedWalker struct {
	walker
	lat *latencies
}

func (w *timedWalker) step(b *bot, tick int) (float64, float64, float64, float32, bool, bool) {
	switch {
	case tick%100 == 0:
		w.lat.mu.Lock()
		w.lat.commandSent = append(w.lat.commandSent, time.Now())
		w.lat.mu.Unlock()
		b.send(protocol.IDPlayChatCommand, func(p *protocol.Buffer) { p.PutString("list") })
	case tick%20 == 10:
		b.send(protocol.IDPlayPingRequest, func(p *protocol.Buffer) { p.PutI64(time.Now().UnixNano()) })
	}
	return w.walker.step(b, tick)
}

func (w *timedWalker) observe(b *bot, id int32, data []byte) {
	switch id {
	case protocol.IDPlayPlayerInfoUpdate:
		// The tab list's latency refresh: the server's own smoothed
		// keep-alive round trip, every 600 ticks.
		r := protocol.NewReader(data)
		if r.ReadU8() != latencyOnly {
			return
		}
		self := protocol.OfflineUUID(b.name)
		for n := r.ReadVarInt(); n > 0 && r.Err() == nil; n-- {
			id, ms := r.ReadUUID(), r.ReadVarInt()
			if id == self && r.Err() == nil {
				w.lat.mu.Lock()
				w.lat.keepAlive = append(w.lat.keepAlive, time.Duration(ms)*time.Millisecond)
				w.lat.mu.Unlock()
			}
		}
	case protocol.IDPlaySystemChat:
		w.lat.answered()
	case protocol.IDPlayPingResponse:
		sent := protocol.NewReader(data).ReadI64()
		w.lat.mu.Lock()
		w.lat.ping = append(w.lat.ping, time.Since(time.Unix(0, sent)))
		w.lat.mu.Unlock()
	}
}

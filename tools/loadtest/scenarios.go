package main

import (
	"fmt"
	"github.com/golem-mc/golem/internal/protocol"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type scenario struct {
	levelType string
	run       func(e *env) error
}

const (
	normal = "minecraft:normal"
	flat   = "minecraft:flat"
	// ground is the flat world's floor, where a body standing on it is.
	ground = -60
	walk   = 4.317 / 20 // blocks per tick
)

var scenarios = map[string]scenario{
	"idle":          {normal, idleScenario},
	"chunks-tp-1":   {normal, chunksTP(1)},
	"chunks-tp-10":  {normal, chunksTP(10)},
	"chunks-tp-25":  {normal, chunksTP(25)},
	"chunks-tp-50":  {normal, chunksTP(50)},
	"chunks-fly-10": {normal, chunksFly(10)},
	"chunks-fly-25": {normal, chunksFly(25)},
	"lifecycle-5":   {flat, lifecyclePlayers},
	"connection-50": {flat, connectionPacketsPlayers},
	"chat-50":       {flat, chatPlayers},
	"players-50":    {flat, players(50)},
	"players-100":   {flat, players(100)},
	"commands-50":   {flat, commandsScenario},
	"zombies-500":   {flat, mobs("zombie", 500)},
	"zombies-2000":  {flat, mobs("zombie", 2000)},
	"passive-2000":  {flat, mobs("passive", 2000)},
	"llamas-2000":   {flat, mobs("llama", 2000)},
	"items-2000":    {flat, items(2000)},
	"llamas-2000":   {flat, llamas(2000)},
	"redstone":      {flat, redstoneClocks},
	"hoppers":       {flat, hoppers},
	"fluids":        {flat, fluids},
	"tnt":           {flat, tnt},
}

// setup runs build commands and logs their replies, so a command one server
// does not know shows up in setup.log.
func (e *env) setup(commands ...string) {
	f, _ := os.OpenFile(filepath.Join(e.o.out, "setup.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	defer f.Close()
	for _, c := range commands {
		out := e.cmdOut(c)
		if len(out) > 300 {
			out = out[:300]
		}
		fmt.Fprintf(f, "%s => %q\n", c, out)
	}
}

// flatStart joins the creative observer above the origin, waits for the
// chunks around it (nothing stays loaded without a player), and sets the
// rules every flat scenario shares: no natural spawns, a fixed time of day
// and clear weather.
func (e *env) flatStart(daytime string, observerY float64) error {
	if err := e.joinObserver("creative", 0.5, observerY, 0.5); err != nil {
		return err
	}
	if err := e.observer.waitChunks(300, time.Minute); err != nil {
		return err
	}
	e.setup("gamerule spawn_mobs false", "gamerule advance_time false", "gamerule advance_weather false",
		"weather clear", "time set "+daytime)
	return nil
}

func idleScenario(e *env) error {
	time.Sleep(e.o.warmup)
	e.measure("empty", e.o.measure, measureOpts{})
	if err := e.joinObserver("survival", math.NaN(), 0, 0); err != nil {
		return err
	}
	time.Sleep(e.o.warmup / 2)
	e.measure("one_player", e.o.measure, measureOpts{profile: true})
	return nil
}

// viewFull is the chunk count a bot's view of 10 is taken as filled at:
// vanilla's round view holds fewer than the 441 of the square.
const viewFull = 300

// place teleports every bot to its own spot on a circle of radius r at
// y=200, and returns once the chunk streams have gone quiet or after limit.
func (e *env) place(bots []*bot, r float64, limit time.Duration) time.Duration {
	for _, b := range bots {
		b.markOnTeleport()
	}
	start := time.Now()
	for i, b := range bots {
		a := 2 * math.Pi * float64(i) / float64(len(bots))
		e.cmd(fmt.Sprintf("tp %s %.1f 200 %.1f", b.name, r*math.Cos(a)+0.5, r*math.Sin(a)+0.5))
	}
	// Done once every bot holds most of its view and nothing arrived for 5 s;
	// a server that blocks its loop on a teleport, or works off generation
	// a previous round left queued, goes quiet long before that, so quiet
	// alone ends the round only after two minutes.
	last, lastCount := time.Now(), int64(-1)
	for time.Since(start) < limit {
		time.Sleep(time.Second)
		var n int64
		full := true
		for _, b := range bots {
			n += b.chunksIn.Load()
			full = full && b.viewCount(int32(e.o.view)) >= viewFull
		}
		if n != lastCount {
			last, lastCount = time.Now(), n
		} else if quiet := time.Since(last); (full && quiet > 5*time.Second) || quiet > 2*time.Minute {
			break
		}
	}
	return time.Since(start)
}

type botViewTime struct {
	Name    string   `json:"name"`
	Seconds *float64 `json:"seconds"`
}

// viewTimes summarises how long each bot took to fill its view after its
// last teleport. Milestones use the full square at the configured view
// distance and include only bots that reached that milestone.
func (e *env) viewTimes(bots []*bot) map[string]any {
	var first, t50, t90, t100, finals []float64
	byBot := make([]botViewTime, len(bots))
	var total int
	var lastArrival time.Duration
	viewChunks := (2*e.o.view + 1) * (2*e.o.view + 1)
	for i, b := range bots {
		byBot[i].Name = b.name
		log, cx, cz := b.arrivals()
		seen := map[[2]int32]bool{}
		var times []time.Duration
		for _, a := range log {
			k := [2]int32{a.x, a.z}
			if seen[k] || abs32(a.x-cx) > int32(e.o.view) || abs32(a.z-cz) > int32(e.o.view) {
				continue
			}
			seen[k] = true
			times = append(times, a.at)
		}
		if len(times) == 0 {
			continue
		}
		at := func(samples []float64, p float64) []float64 {
			n := int(math.Ceil(p * float64(viewChunks)))
			if len(times) >= n {
				return append(samples, times[n-1].Seconds())
			}
			return samples
		}
		first = append(first, times[0].Seconds())
		t50, t90, t100 = at(t50, 0.5), at(t90, 0.9), at(t100, 1)
		if len(times) >= viewChunks {
			seconds := times[viewChunks-1].Seconds()
			byBot[i].Seconds = &seconds
		}
		finals = append(finals, float64(len(times)))
		total += len(times)
		lastArrival = max(lastArrival, times[len(times)-1])
	}
	med := func(v []float64) float64 { return statsOf(v).P50 }
	top := func(v []float64) float64 { return statsOf(v).Max }
	return map[string]any{
		"bots_complete":  len(t100),
		"t100_by_bot":    byBot,
		"first_median_s": med(first), "first_max_s": top(first),
		"view_chunks_median": med(finals),
		"t50_median_s":       med(t50), "t90_median_s": med(t90), "t100_median_s": med(t100),
		"t90_max_s": top(t90), "t100_max_s": top(t100),
		"chunks_total":            total,
		"chunks_per_s_until_last": float64(total) / max(lastArrival.Seconds(), 0.001),
	}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func chunksTP(n int) func(*env) error {
	return func(e *env) error {
		if err := e.joinObserver("creative", math.NaN(), 0, 0); err != nil {
			return err
		}
		time.Sleep(30 * time.Second)
		bots, err := e.spawnBots(n, "tp", 10, int8(e.o.view), func(int) behaviour { return idle{} })
		if err != nil {
			return err
		}
		e.setup("gamemode creative @a")
		time.Sleep(10 * time.Second)
		// A first round into fresh terrain warms the JIT and the generator.
		took := e.place(bots, 3000, 2*time.Minute)
		e.sum.Extra["warm"] = e.viewTimes(bots)
		e.sum.Extra["warm_round_s"] = took.Seconds()
		// Generation the warm round left queued would otherwise run into the
		// measured one.
		e.sum.Extra["settle_s"] = e.waitIdle(1.5, 4*time.Minute).Seconds()
		var mainTook time.Duration
		var done sync.WaitGroup
		done.Add(1)
		e.measure("main", e.o.measure, measureOpts{profile: true, profileDuration: e.o.measure, during: func(<-chan struct{}) {
			defer done.Done()
			mainTook = e.place(bots, 6000, 5*time.Minute)
		}})
		done.Wait()
		e.sum.Extra["main"] = e.viewTimes(bots)
		e.sum.Extra["main_round_s"] = mainTook.Seconds()
		return nil
	}
}

func chunksFly(n int) func(*env) error {
	return func(e *env) error {
		if err := e.joinObserver("creative", math.NaN(), 0, 0); err != nil {
			return err
		}
		time.Sleep(30 * time.Second)
		flyers := make([]*flyer, n)
		bots, err := e.spawnBots(n, "fly", 10, int8(e.o.view), func(i int) behaviour {
			a := 2 * math.Pi * float64(i) / float64(n)
			flyers[i] = &flyer{dx: math.Cos(a), dz: math.Sin(a), speed: 0.5}
			return flyers[i]
		})
		if err != nil {
			return err
		}
		e.setup("gamemode creative @a")
		time.Sleep(10 * time.Second)
		took := e.place(bots, 3000, 2*time.Minute)
		e.sum.Extra["place_s"] = took.Seconds()
		for _, f := range flyers {
			f.moving.Store(true)
		}
		time.Sleep(e.o.warmup / 2)
		var missing []float64
		w := e.measure("main", e.o.measure, measureOpts{profile: true, during: func(stop <-chan struct{}) {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					for _, b := range bots {
						missing = append(missing, float64(b.missingNear(2)))
					}
				}
			}
		}})
		w.Extra["missing_within_2_mean"] = statsOf(missing).Mean
		w.Extra["missing_within_2_p95"] = statsOf(missing).P95
		w.Extra["fly_blocks_per_s"] = 10.0
		return nil
	}
}

func players(n int) func(*env) error {
	return func(e *env) error {
		if err := e.flatStart("6000", ground+10); err != nil {
			return err
		}
		_, err := e.spawnBots(n, "walk", 10, int8(e.o.view), func(i int) behaviour {
			return &walker{radius: 32, speed: walk, rng: rand.New(rand.NewPCG(uint64(i), 7))}
		})
		if err != nil {
			return err
		}
		time.Sleep(e.o.warmup)
		e.measure("main", e.o.measure, measureOpts{profile: true})
		return nil
	}
}

// pen walls in the 61x61 floor around the origin with glass three high.
func (e *env) pen() {
	e.setup(fmt.Sprintf("fill -31 %d -31 31 %d 31 glass hollow", ground, ground+2),
		fmt.Sprintf("fill -30 %d -30 30 %d 30 air", ground, ground+2))
}

func (e *env) count(selector string) string {
	return strings.TrimSpace(e.cmdOut("execute if entity " + selector))
}

func mobs(kind string, n int) func(*env) error {
	return func(e *env) error {
		if err := e.flatStart("18000", ground+10); err != nil {
			return err
		}
		e.pen()
		rng := rand.New(rand.NewPCG(1, 2))
		passive := []string{"cow", "sheep", "pig", "chicken"}
		for i := range n {
			typ := kind
			if kind == "passive" {
				typ = passive[i%len(passive)]
			}
			x, z := rng.Float64()*58-29, rng.Float64()*58-29
			e.cmd(fmt.Sprintf("summon %s %.2f %d %.2f {PersistenceRequired:1b}", typ, x, ground, z))
		}
		e.sum.Extra["count_after_summon"] = e.count("@e[type=!player]")
		time.Sleep(e.o.warmup)
		e.measure("main", e.o.measure, measureOpts{profile: true})
		e.sum.Extra["count_after_measure"] = e.count("@e[type=!player]")
		return nil
	}
}

func items(n int) func(*env) error {
	return func(e *env) error {
		if err := e.flatStart("6000", ground+10); err != nil {
			return err
		}
		e.pen()
		kinds := []string{"stone", "dirt", "oak_planks", "cobblestone", "sand"}
		for i := range n {
			x, z := i%50-25, i/50-25
			e.cmd(fmt.Sprintf(`summon item %d.5 %d %d.5 {Item:{id:"minecraft:%s",count:1},PickupDelay:32767s,Age:-32768s}`,
				x, ground, z, kinds[i%len(kinds)]))
		}
		e.sum.Extra["count_after_summon"] = e.count("@e[type=item]")
		time.Sleep(e.o.warmup)
		e.measure("main", e.o.measure, measureOpts{profile: true})
		e.sum.Extra["count_after_measure"] = e.count("@e[type=item]")
		return nil
	}
}

// redstoneClocks builds 512 observer pair clocks, each driving a piston on
// both sides: a row is piston, observer facing east, observer facing west,
// piston, along x; 64 rows along z in 8 groups.
func redstoneClocks(e *env) error {
	if err := e.flatStart("6000", ground+10); err != nil {
		return err
	}
	var cmds []string
	for g := range 8 {
		x := -20 + 5*g
		cmds = append(cmds,
			fmt.Sprintf("fill %d %d -32 %d %d 31 piston[facing=up]", x, ground, x, ground),
			fmt.Sprintf("fill %d %d -32 %d %d 31 piston[facing=up]", x+3, ground, x+3, ground),
			fmt.Sprintf("fill %d %d -32 %d %d 31 observer[facing=east]", x+1, ground, x+1, ground),
		)
	}
	// The west-facing column last: placing it is the change the east-facing
	// observers see, which starts each clock.
	for g := range 8 {
		x := -20 + 5*g
		cmds = append(cmds, fmt.Sprintf("fill %d %d -32 %d %d 31 observer[facing=west]", x+2, ground, x+2, ground))
	}
	e.setup(cmds...)
	time.Sleep(e.o.warmup)
	e.measure("main", e.o.measure, measureOpts{profile: true})
	return nil
}

// hoppers builds 2048 rings of four hoppers, each hopper holding a stack
// that the ring passes round for ever: 8192 hoppers in a 128x64 field.
func hoppers(e *env) error {
	if err := e.flatStart("6000", ground+10); err != nil {
		return err
	}
	const x0, z0 = -64, -32
	stack := `{Items:[{Slot:0b,id:"minecraft:stone",count:64}]}`
	cmds := []string{
		fmt.Sprintf("setblock %d %d %d hopper[facing=east]%s", x0, ground, z0, stack),
		fmt.Sprintf("setblock %d %d %d hopper[facing=south]%s", x0+1, ground, z0, stack),
		fmt.Sprintf("setblock %d %d %d hopper[facing=west]%s", x0+1, ground, z0+1, stack),
		fmt.Sprintf("setblock %d %d %d hopper[facing=north]%s", x0, ground, z0+1, stack),
	}
	w, h := 2, 2
	for w < 128 || h < 64 {
		if w <= h || h >= 64 {
			cmds = append(cmds, fmt.Sprintf("clone %d %d %d %d %d %d %d %d %d", x0, ground, z0, x0+w-1, ground, z0+h-1, x0+w, ground, z0))
			w *= 2
		} else {
			cmds = append(cmds, fmt.Sprintf("clone %d %d %d %d %d %d %d %d %d", x0, ground, z0, x0+w-1, ground, z0+h-1, x0, ground, z0+h))
			h *= 2
		}
	}
	e.setup(cmds...)
	e.sum.Extra["hoppers"] = w * h
	time.Sleep(e.o.warmup)
	e.measure("main", e.o.measure, measureOpts{profile: true})
	return nil
}

// fluidField raises a stone platform at y=-50 over x0..x0+w-1, z -64..63
// and sets water sources on its north half and lava on its south half,
// every 8 blocks; the fluids spread over it and pour off its edges.
func fluidField(x0, w int) []string {
	cmds := []string{
		fmt.Sprintf("fill %d -50 -64 %d -50 -1 stone", x0, x0+w-1),
		fmt.Sprintf("fill %d -50 0 %d -50 63 stone", x0, x0+w-1),
	}
	for x := x0 + 4; x < x0+w; x += 8 {
		for z := -60; z < 64; z += 8 {
			fluid := "water"
			if z >= 0 {
				fluid = "lava"
			}
			cmds = append(cmds, fmt.Sprintf("setblock %d -49 %d %s", x, z, fluid))
		}
	}
	return cmds
}

func fluids(e *env) error {
	if err := e.flatStart("6000", -40); err != nil {
		return err
	}
	// A smaller field to the east first, so the measured one meets warm code.
	e.setup(fluidField(96, 32)...)
	time.Sleep(e.o.warmup)
	main := fluidField(-64, 128)
	e.sum.Extra["sources"] = len(main) - 2
	e.measure("main", e.o.measure, measureOpts{profile: true, during: func(<-chan struct{}) { e.setup(main...) }})
	return nil
}

func tnt(e *env) error {
	if err := e.flatStart("6000", ground+10); err != nil {
		return err
	}
	// A 6x6x6 warm-up block, 60 blocks away from the measured one.
	e.setup(fmt.Sprintf("fill 60 %d 60 65 %d 65 tnt", ground, ground+5), fmt.Sprintf("setblock 66 %d 60 redstone_block", ground))
	time.Sleep(e.o.warmup)
	e.setup(fmt.Sprintf("fill -8 %d 24 7 %d 39 tnt", ground, ground+15))
	var mu sync.Mutex
	var counts []string
	e.measure("main", e.o.measure, measureOpts{profile: true, during: func(stop <-chan struct{}) {
		start := time.Now()
		e.setup(fmt.Sprintf("setblock 8 %d 24 redstone_block", ground))
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Second):
			}
			c := e.count("@e[type=tnt]")
			mu.Lock()
			counts = append(counts, fmt.Sprintf("%.0f:%s", time.Since(start).Seconds(), c))
			mu.Unlock()
		}
	}})
	e.sum.Extra["tnt_counts"] = slices.Clone(counts)
	e.sum.Extra["items_after"] = e.count("@e[type=item]")
	return nil
}

// chatPlayers relays 50 unsigned lines a second to 50 recipients with no plugins.
func chatPlayers(e *env) error {
	if err := e.flatStart("6000", ground+10); err != nil {
		return err
	}
	_, err := e.spawnBots(50, "chat", 10, int8(e.o.view), func(int) behaviour { return &chatSpeaker{} })
	if err != nil {
		return err
	}
	time.Sleep(e.o.warmup)
	e.measure("main", e.o.measure, measureOpts{profile: true})
	return nil
}

type chatSpeaker struct{}

func (*chatSpeaker) step(b *bot, tick int) (float64, float64, float64, float32, bool, bool) {
	if tick%20 == 0 {
		b.send(protocol.IDPlayChatMessage, func(w *protocol.Buffer) {
			w.PutString("load test chat")
			w.PutI64(0)
			w.PutI64(0)
			w.PutBool(false)
			w.PutVarInt(0)
			w.PutRaw([]byte{0, 0, 0, 1})
		})
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.x, b.y, b.z, b.yaw, true, false
}

// connectionPacketsPlayers exercises common-packet hook gates without plugins.
func connectionPacketsPlayers(e *env) error {
	if err := e.flatStart("6000", ground+10); err != nil {
		return err
	}
	if _, err := e.spawnBots(50, "conn", 10, int8(e.o.view), func(int) behaviour { return &connectionSpeaker{} }); err != nil {
		return err
	}
	time.Sleep(e.o.warmup)
	// Startup spawns and random block work otherwise vary between comparison runs.
	e.setup("gamerule random_tick_speed 0", "kill @e[type=!minecraft:player]", "kill @e[type=!minecraft:player]")
	time.Sleep(time.Second) // let killed bodies and their drops leave the trackers
	e.measure("main", e.o.measure, measureOpts{profile: true})
	return nil
}

type connectionSpeaker struct{}

func (*connectionSpeaker) step(b *bot, tick int) (float64, float64, float64, float32, bool, bool) {
	if tick%20 == 0 {
		b.send(protocol.IDPlayClientInformation, func(w *protocol.Buffer) {
			w.PutString("en_us")
			w.PutI8(10)
			w.PutVarInt(0)
			w.PutBool(true)
			w.PutU8(127)
			w.PutVarInt(1)
			w.PutBool(false)
			w.PutBool(true)
			w.PutVarInt(0)
		})
		b.send(protocol.IDPlayPluginMessageIn, func(w *protocol.Buffer) { w.PutString("load:channel"); w.PutRaw([]byte{1, 2, 3}) })
		b.send(protocol.IDPlayResourcePackIn, func(w *protocol.Buffer) { w.PutUUID([16]byte{}); w.PutVarInt(3) })
		b.send(protocol.IDPlayCustomClickAction, func(w *protocol.Buffer) { w.PutString("load:click"); w.PutBytes([]byte{0}) })
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.x, b.y, b.z, b.yaw, true, false
}

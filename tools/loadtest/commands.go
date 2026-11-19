package main

import (
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

func commandsScenario(e *env) error {
	if err := e.flatStart("6000", ground+10); err != nil {
		return err
	}
	if _, err := e.spawnBots(50, "cmd", 10, int8(e.o.view), func(int) behaviour { return commandTraffic{} }); err != nil {
		return err
	}
	time.Sleep(e.o.warmup)
	e.measure("main", e.o.measure, measureOpts{profile: true})
	return nil
}

type commandTraffic struct{}

func (commandTraffic) step(b *bot, tick int) (float64, float64, float64, float32, bool, bool) {
	// One command per second stays below vanilla's command-spam threshold.
	if tick%20 == 0 {
		b.send(protocol.IDPlayChatCommand, func(w *protocol.Buffer) { w.PutString("list") })
		b.send(protocol.IDPlayCommandSuggestion, func(w *protocol.Buffer) {
			w.PutVarInt(int32(tick))
			w.PutString("/msg ")
		})
	}
	return idle{}.step(b, tick)
}

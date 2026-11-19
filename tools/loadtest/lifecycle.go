package main

import (
	"fmt"
	"time"
)

// lifecyclePlayers exercises joins, peer quits and operator kicks without plugins.
func lifecyclePlayers(e *env) error {
	if err := e.flatStart("6000", ground+10); err != nil {
		return err
	}
	e.setup("gamerule random_tick_speed 0", "kill @e[type=!minecraft:player]", "kill @e[type=!minecraft:player]", "tick freeze")
	time.Sleep(e.o.warmup)
	joins, kicks := 0, 0
	var failure error
	e.measure("main", e.o.measure, measureOpts{profile: true, during: func(stop <-chan struct{}) {
		pace := time.NewTicker(200 * time.Millisecond)
		defer pace.Stop()
		for {
			select {
			case <-stop:
				return
			case <-pace.C:
			}
			// Reuse saved profiles and the observer's already-generated chunks.
			name := fmt.Sprintf("Lifecycle%d", joins%10)
			b, err := dialBot(e.o.gameAddr(), name, 2, idle{})
			if err != nil {
				failure = err
				return
			}
			go b.run()
			if err = b.waitChunks(1, 30*time.Second); err == nil {
				joins++
				if joins%2 == 0 {
					_, err = e.rc.Run("kick " + name + " lifecycle comparison")
					kicks++
				} else {
					b.close()
				}
			}
			if err != nil {
				b.close()
				failure = err
				return
			}
			select {
			case <-b.done:
			case <-time.After(30 * time.Second):
				b.close()
				failure = fmt.Errorf("%s did not disconnect", name)
				return
			}
		}
	}})
	e.sum.Extra["lifecycle_joins"], e.sum.Extra["lifecycle_kicks"] = joins, kicks
	return failure
}

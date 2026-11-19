package plugin_test

import (
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// Economy is a service one plugin provides and others use.
type Economy interface {
	Balance(id plugin.UUID) float64
}

type warps struct {
	homes map[plugin.UUID]plugin.Location
}

func (w *warps) Enable(s plugin.Server) error {
	key := plugin.Key{Namespace: s.Name(), Path: "visits"}

	// Creative players may break anything; others not the spawn's bedrock.
	plugin.Listen(s, func(e *plugin.BlockBreakEvent) {
		if e.State.Block() == registry.BlockBedrock && e.Player.GameMode() != plugin.Creative {
			e.SetCancelled(true)
		}
	})
	plugin.Listen(s, func(e *plugin.PlayerJoinEvent) {
		n, _ := plugin.GetData[int32](e.Player.Data(), key)
		e.Player.Data().Set(key, n+1)
	}, plugin.ListenOptions{Priority: plugin.Monitor})

	if err := s.Commands().Register(plugin.Command{
		Name:       "sethome",
		Permission: "warps.sethome",
		Run: func(ctx *plugin.CommandContext) (int, error) {
			p, ok := ctx.Sender.(plugin.Player)
			if !ok {
				return 0, plugin.CommandError{Message: plugin.Plain("Only players have homes")}
			}
			w.homes[p.UUID()] = p.Location()
			p.SendMessage(plugin.Plain("Home set").Color("green"))
			return 1, nil
		},
	}); err != nil {
		return err
	}

	s.Scheduler().RunTimer(0, 20*60, func() {
		if eco, ok := plugin.Service[Economy](s); ok {
			for _, p := range s.Players() {
				p.SendActionBar(plugin.Plain("Balance: ").Append(plugin.Plain("…")))
				_ = eco.Balance(p.UUID())
			}
		}
	})
	return nil
}

func (w *warps) Disable() {}

// A plugin registers itself when the launcher's build imports its package.
func Example() {
	plugin.Register("warps", &warps{homes: map[plugin.UUID]plugin.Location{}}, plugin.SoftDepends("economy"))
}

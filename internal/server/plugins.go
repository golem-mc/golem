package server

import (
	"log/slog"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

// pluginHost is the plugin.Server the plugins see.
type pluginHost struct {
	srv  *Server
	host *pluginruntime.Host

	ticketOnce sync.Once
	// ticketMu orders ticket changes against the release when the plugin
	// closes; held is the tickets this plugin has outstanding.
	ticketMu     sync.Mutex
	held         map[pluginTicket]int
	ticketClosed bool
}

func (s *Server) loadPlugins(registrations []plugin.Registration) {
	s.pluginHelp.load()
	if len(registrations) == 0 {
		return
	}
	s.plugins = pluginruntime.New(registrations, func(h *pluginruntime.Host) plugin.Server {
		adapter := &pluginHost{srv: s, host: h}
		if s.pluginAdapter == nil {
			s.pluginAdapter = adapter
		}
		return adapter
	})
	s.plugins.BindEvents(bindPluginEvent)
	s.plugins.ChannelsChanged(s.pluginChannelsChanged)
	s.plugins.Load()
}
func (s *Server) enablePlugins() {
	if s.plugins != nil {
		s.plugins.Enable()
		for _, d := range s.allDims() {
			s.fireWorldLoad(d)
		}
	}
}
func (s *Server) disablePlugins() {
	if s.plugins != nil {
		s.plugins.Disable()
	}
}
func (h *pluginHost) Logger() *slog.Logger   { return h.host.Logger() }
func (h *pluginHost) Name() string           { return h.host.Name() }
func (h *pluginHost) DataDir() string        { return h.host.DataDir() }
func (h *pluginHost) Plugins() []plugin.Info { return h.host.Plugins() }
func (*pluginHost) Version() (string, int)   { return protocol.GameVersion, protocol.Version }
func (h *pluginHost) MaxPlayers() int        { return int(h.srv.maxPlayers.Load()) }
func (h *pluginHost) SetMaxPlayers(n int)    { h.srv.maxPlayers.Store(int32(max(n, 0))) }
func (h *pluginHost) SpawnProtection() int   { return int(h.srv.spawnProtection.Load()) }
func (h *pluginHost) Generator(world plugin.Key, spec string) (plugin.Generator, error) {
	return h.host.Generator(world, spec)
}
func (h *pluginHost) MOTD() plugin.Text {
	text, _ := pluginText(*h.srv.motd.Load())
	return text
}
func (h *pluginHost) SetMOTD(t plugin.Text) {
	text := pluginProtocolText(pluginruntime.CloneText(t))
	h.srv.motd.Store(&text)
}
func (h *pluginHost) Stop()                           { h.srv.RequestStop() }
func (h *pluginHost) Events() plugin.Events           { return h.host.Events() }
func (h *pluginHost) Scheduler() plugin.Scheduler     { return h.host.Scheduler() }
func (h *pluginHost) Services() plugin.Services       { return h.host.Services() }
func (h *pluginHost) Permissions() plugin.Permissions { return h.host.Permissions() }
func (h *pluginHost) Ticks() plugin.Ticks             { return pluginTicks{h.srv} }

type pluginTicks struct{ srv *Server }

func (t pluginTicks) Current() int64 { return t.srv.plugins.Current() }
func (t pluginTicks) Rate() float32  { return t.srv.tickRate.Rate() }
func (t pluginTicks) Frozen() bool   { return t.srv.tickRate.Frozen() }
func (t pluginTicks) TPS() [3]float64 {
	return [3]float64{t.srv.metrics.TPS(time.Minute), t.srv.metrics.TPS(5 * time.Minute), t.srv.metrics.TPS(15 * time.Minute)}
}
func (t pluginTicks) MSPT() float64 { _, n := t.srv.metrics.TickTimes(); return float64(n) / 1e6 }

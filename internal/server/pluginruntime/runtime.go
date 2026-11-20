package pluginruntime

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/plugin"
)

// Host owns one plugin's lifecycle and runtime resources.
type Host struct {
	api          plugin.Server
	runtime      *Runtime
	registration plugin.Registration
	enabled      atomic.Bool
	closed       atomic.Bool
	done         chan struct{}
	events       pluginEvents
	scheduler    pluginScheduler
	services     pluginServices
	permissions  pluginPermissions
	ownedMu      sync.Mutex
	owned        []func()
}

// Runtime owns the compiled-in plugins and their shared mechanisms.
type Runtime struct {
	current atomic.Int64
	// tickGoroutine is getg() of the tick loop's goroutine. Comparing g
	// pointers is sound: a g is reused only after its goroutine exits, and
	// the stored one belongs to the live tick loop.
	tickGoroutine atomic.Uintptr
	hosts         []*Host
	order         []*Host
	bus           pluginBus
	tasks         pluginTasks
	services      pluginServiceRegistry
	channels      pluginChannels
	permissions   permissionRegistry
	callsMu       sync.Mutex
	calls         []tickCall
	callsPending  atomic.Bool
	callsQueued   chan struct{} // holds one signal while calls wait
	stopped       chan struct{}
	disableOnce   sync.Once
	connections   connectionTasks
	bindEvent     func(plugin.Event, plugin.Server) func()
}

// New creates handles without invoking plugins; Load starts their lifecycle.
func New(registrations []plugin.Registration, adapter func(*Host) plugin.Server) *Runtime {
	r := &Runtime{stopped: make(chan struct{}), callsQueued: make(chan struct{}, 1)}
	byName := make(map[string]*Host)
	for _, reg := range registrations {
		if reg.Plugin == nil || reg.Name == "" || reg.Name == "." || reg.Name == ".." || strings.ContainsAny(reg.Name, `/\\`) || byName[reg.Name] != nil {
			slog.Error("Invalid plugin registration", "plugin", reg.Name)
			continue
		}
		h := &Host{runtime: r, registration: reg, done: make(chan struct{})}
		h.api = adapter(h)
		h.events.host, h.scheduler.host, h.services.host = h, h, h
		h.permissions.host = h
		byName[reg.Name] = h
		r.hosts = append(r.hosts, h)
	}
	return r
}

// Any reports whether a plugin is registered.
func (r *Runtime) Any() bool { return r != nil && len(r.hosts) != 0 }

// Load invokes loaders in stable dependency order before world construction.
func (r *Runtime) Load() {
	byName := make(map[string]*Host, len(r.hosts))
	for _, h := range r.hosts {
		byName[h.Name()] = h
	}
	// Stable topological ordering includes optional edges only when present.
	pending := slices.Clone(r.hosts)
ordering:
	for len(pending) > 0 {
		for _, h := range pending {
			reg := h.registration
			missing, waiting := "", false
			for _, name := range reg.Depends {
				d := byName[name]
				if d == nil || d.closed.Load() {
					missing = name
					break
				}
				waiting = waiting || slices.Contains(pending, d)
			}
			if missing == "" {
				for _, name := range reg.SoftDepends {
					waiting = waiting || slices.Contains(pending, byName[name])
				}
				for _, other := range pending {
					waiting = waiting || slices.Contains(other.registration.LoadBefore, reg.Name)
				}
				if waiting {
					continue
				}
			}
			pending = slices.DeleteFunc(pending, func(p *Host) bool { return p == h })
			if missing != "" {
				h.Logger().Error("Missing plugin dependency", "dependency", missing)
				h.cleanup()
				continue ordering
			}
			if loader, ok := reg.Plugin.(plugin.Loader); ok {
				if err := h.guard(func() error { return loader.Load(h.api) }); err != nil {
					h.Logger().Error("Error loading plugin", "error", err)
					h.cleanup()
					continue ordering
				}
			}
			r.order = append(r.order, h)
			continue ordering
		}
		for _, h := range pending {
			h.Logger().Error("Plugin dependency cycle")
			h.cleanup()
		}
		break
	}
}

// Enable starts plugins after world construction.
func (r *Runtime) Enable() {
	for _, h := range r.order {
		missing := false
		for _, name := range h.registration.Depends {
			for _, d := range r.hosts {
				if d.Name() == name && !d.enabled.Load() {
					missing = true
				}
			}
		}
		if missing {
			h.Logger().Error("Plugin dependency failed to enable")
			h.cleanup()
			continue
		}
		if err := h.guard(func() error { return h.registration.Plugin.Enable(h.api) }); err != nil {
			if errors.Is(err, plugin.ErrNameTaken) {
				h.Logger().Warn("Command name taken; registered under plugin prefix", "error", err)
			} else {
				h.Logger().Error("Error enabling plugin", "error", err)
				h.cleanup()
				continue
			}
		}
		h.enabled.Store(true)
		if r.bus.has(PluginEnable) {
			r.bus.fire(&plugin.PluginEnableEvent{Plugin: h.info()})
		}
	}
	if r.bus.has(ServerLoad) {
		r.bus.fire(&plugin.ServerLoadEvent{})
	}
}

// Disable stops plugins in reverse dependency order and joins workers.
// Calling it more than once is safe.
func (r *Runtime) Disable() {
	r.disableOnce.Do(func() {
		defer r.stopCalls()
		for _, h := range slices.Backward(r.order) {
			if h.enabled.Load() {
				if r.bus.has(PluginDisable) {
					r.bus.fire(&plugin.PluginDisableEvent{Plugin: h.info()})
				}
				if err := h.guard(func() error { h.registration.Plugin.Disable(); return nil }); err != nil {
					h.Logger().Error("Error disabling plugin", "error", err)
				}
			}
			h.cleanup()
		}
		r.tasks.stop()
	})
}

// cleanup releases the plugin's resources. A panic in one release is logged
// and does not stop the others or the server.
func (h *Host) cleanup() {
	if !h.closed.CompareAndSwap(false, true) {
		return
	}
	close(h.done)
	h.enabled.Store(false)
	steps := []func(){h.events.remove, h.scheduler.cancelAll, h.services.remove, h.removeChannels, h.permissions.remove}
	h.ownedMu.Lock()
	owned := h.owned
	h.owned = nil
	h.ownedMu.Unlock()
	slices.Reverse(owned)
	steps = append(steps, owned...)
	for _, step := range steps {
		if err := h.guard(func() error { step(); return nil }); err != nil {
			h.Logger().Error("Error releasing plugin resources", "error", err)
		}
	}
}

// Own releases a server adapter's resource when this plugin closes.
func (h *Host) Own(release func()) {
	h.ownedMu.Lock()
	if h.closed.Load() {
		h.ownedMu.Unlock()
		release()
		return
	}
	h.owned = append(h.owned, release)
	h.ownedMu.Unlock()
}

// Done closes when the plugin's resources must be released.
func (h *Host) Done() <-chan struct{} { return h.done }

func (h *Host) guard(fn func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = Recovered(p)
		}
	}()
	return fn()
}

// PanicError is a plugin's recovered panic. Its message is the panic's value;
// the console prints Stack, the goroutine's stack where it panicked, under
// the log line.
type PanicError struct {
	Value any
	stack []byte
}

// Recovered wraps p, just recovered, with the stack of the panic; call it in
// the deferred function that recovered.
func Recovered(p any) *PanicError { return &PanicError{Value: p, stack: debug.Stack()} }

func (e *PanicError) Error() string { return fmt.Sprintf("panic: %v", e.Value) }

// Stack is the stack trace of the panicking goroutine.
func (e *PanicError) Stack() string { return string(e.stack) }

// Logger answers the logger tagged with the plugin name.
func (h *Host) Logger() *slog.Logger { return slog.Default().With("plugin", h.Name()) }

// Name answers the registration name.
func (h *Host) Name() string { return h.registration.Name }

// DataDir creates and answers the plugin's data directory.
func (h *Host) DataDir() string {
	dir := filepath.Join("plugins", h.Name())
	if err := os.MkdirAll(dir, 0755); err != nil {
		h.Logger().Error("Create plugin data directory", "error", err)
	}
	return dir
}
func (h *Host) info() plugin.Info {
	info := plugin.Info{Name: h.Name(), Enabled: h.enabled.Load()}
	_, info.Generators = h.registration.Plugin.(plugin.GeneratorProvider)
	t := reflect.TypeOf(h.registration.Plugin)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if build, ok := debug.ReadBuildInfo(); ok {
		modules := append([]*debug.Module{&build.Main}, build.Deps...)
		for _, m := range modules {
			if (t.PkgPath() == m.Path || strings.HasPrefix(t.PkgPath(), m.Path+"/")) && len(m.Path) > len(info.Module) {
				info.Module, info.Version = m.Path, m.Version
			}
		}
	}
	return info
}

// Plugins answers a snapshot of all plugin identities and enable states.
func (h *Host) Plugins() []plugin.Info { return h.runtime.Plugins() }

// Plugins answers a snapshot of every registered plugin's identity and
// enable state; none without a runtime.
func (r *Runtime) Plugins() []plugin.Info {
	if r == nil {
		return nil
	}
	infos := make([]plugin.Info, 0, len(r.hosts))
	for _, p := range r.hosts {
		infos = append(infos, p.info())
	}
	return infos
}

// Generator resolves a "<plugin>[:<id>]" spec to a plugin's generator.
func (h *Host) Generator(world plugin.Key, spec string) (plugin.Generator, error) {
	name, id, _ := strings.Cut(spec, ":")
	for _, p := range h.runtime.hosts {
		if !strings.EqualFold(p.Name(), name) {
			continue
		}
		gp, ok := p.registration.Plugin.(plugin.GeneratorProvider)
		if !ok {
			return nil, fmt.Errorf("plugin %s provides no generators", p.Name())
		}
		if !p.enabled.Load() {
			return nil, fmt.Errorf("plugin %s is not enabled", p.Name())
		}
		g, err := gp.Generator(world, id)
		if err == nil && g == nil {
			return nil, fmt.Errorf("plugin %s has no generator %q", p.Name(), id)
		}
		return g, err
	}
	return nil, fmt.Errorf("no plugin named %q", name)
}

// Events answers this plugin's event registrations.
func (h *Host) Events() plugin.Events { return &h.events }

// Scheduler answers this plugin's task scheduler.
func (h *Host) Scheduler() plugin.Scheduler { return &h.scheduler }

// Services answers this plugin's service registry handle.
func (h *Host) Services() plugin.Services { return &h.services }

// Current answers the tick counter, including frozen ticks.
func (r *Runtime) Current() int64 { return r.current.Load() }

// StartTick runs due tasks before tick-start listeners.
func (r *Runtime) StartTick() {
	r.tickGoroutine.Store(getg())
	r.current.Add(1)
	r.runConnections()
	r.runTasks()
	if r.Has(ServerTickStart) {
		r.Fire(&plugin.ServerTickStartEvent{Tick: r.Current()})
	}
	r.drainCalls()
}

// BindTick makes the calling goroutine the tick goroutine, for a test that
// drives ticks by hand with the main loop stopped.
func (r *Runtime) BindTick() { r.tickGoroutine.Store(getg()) }

// OnTick reports whether the caller is the tick goroutine, the one that last
// ran StartTick.
func (r *Runtime) OnTick() bool { return r.tickGoroutine.Load() == getg() }

// Has is the allocation-free hook gate.
func (r *Runtime) Has(id int) bool { return r.bus.has(id) }

// Fire delivers an event without holding runtime locks.
func (r *Runtime) Fire(event plugin.Event) { r.bus.fire(event) }

// BindEvents installs the server boundary conversion for listener-owned handles.
func (r *Runtime) BindEvents(bind func(plugin.Event, plugin.Server) func()) { r.bindEvent = bind }

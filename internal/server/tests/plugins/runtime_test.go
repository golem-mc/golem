package plugins_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

type testPlugin struct {
	load, enable func(plugin.Server) error
	disable      func()
}

func (p *testPlugin) Load(s plugin.Server) error {
	if p.load != nil {
		return p.load(s)
	}
	return nil
}
func (p *testPlugin) Enable(s plugin.Server) error {
	if p.enable != nil {
		return p.enable(s)
	}
	return nil
}
func (p *testPlugin) Disable() {
	if p.disable != nil {
		p.disable()
	}
}

type service string

func (s service) String() string { return string(s) }

func TestPluginLifecycleOrderingAndFailureCleanup(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := servertest.BlockTestConfig()
	cfg.Universe = t.TempDir()
	cfg.LevelName = t.TempDir()
	var trace []string
	var handles []plugin.Server
	makePlugin := func(name string) *testPlugin {
		return &testPlugin{load: func(s plugin.Server) error {
			if _, err := os.Stat(cfg.LevelName + "/level.dat"); !os.IsNotExist(err) {
				t.Errorf("Load ran after world creation: %v", err)
			}
			trace = append(trace, "load "+name)
			return nil
		}, enable: func(s plugin.Server) error {
			if len(trace) < 3 || !strings.HasPrefix(trace[2], "load ") {
				t.Error("Enable preceded Load")
			}
			trace = append(trace, "enable "+name)
			handles = append(handles, s)
			plugin.Listen(s, func(e *plugin.PluginEnableEvent) { trace = append(trace, name+" saw enable "+e.Plugin.Name) })
			plugin.Listen(s, func(e *plugin.PluginDisableEvent) { trace = append(trace, name+" saw disable "+e.Plugin.Name) })
			plugin.Listen(s, func(*plugin.ServerLoadEvent) { trace = append(trace, name+" ready") })
			return nil
		}, disable: func() {
			trace = append(trace, "disable "+name)
			if name == "middle" {
				got, ok := plugin.Service[fmt.Stringer](handles[0])
				if !ok || got.String() != "low" {
					t.Errorf("disabled service did not reveal lower priority: %v %v", got, ok)
				}
			}
			if name == "last" {
				panic("disable")
			}
		}}
	}
	failedTask := plugin.Task(nil)
	failed := func(s plugin.Server) error {
		plugin.Provide[fmt.Stringer](s, service("failed"), 100)
		failedTask = s.Scheduler().RunTask(func() { t.Error("failed plugin task ran") })
		plugin.Listen(s, func(*plugin.ServerLoadEvent) { t.Error("failed plugin listener ran") })
		return errors.New("deliberate failure")
	}
	registrations := []plugin.Registration{
		{Name: "last", Plugin: makePlugin("last"), Depends: []string{"middle"}},
		{Name: "middle", Plugin: makePlugin("middle"), SoftDepends: []string{"first", "absent"}},
		{Name: "first", Plugin: makePlugin("first"), LoadBefore: []string{"last", "absent"}},
		{Name: "bad", Plugin: &testPlugin{enable: failed}},
		{Name: "bad-child", Plugin: &testPlugin{enable: func(plugin.Server) error { t.Error("failed dependency enabled"); return nil }}, Depends: []string{"bad"}},
		{Name: "missing", Plugin: makePlugin("missing"), Depends: []string{"absent"}},
		{Name: "cycle-a", Plugin: makePlugin("cycle-a"), Depends: []string{"cycle-b"}},
		{Name: "cycle-b", Plugin: makePlugin("cycle-b"), Depends: []string{"cycle-a"}},
		{Name: "load-panic", Plugin: &testPlugin{load: func(s plugin.Server) error { _ = failed(s); panic("load") }}},
		{Name: "enable-panic", Plugin: &testPlugin{enable: func(s plugin.Server) error { _ = failed(s); panic("enable") }}},
	}
	srv := servertest.NewTestServerWithPlugins(t, cfg, registrations)
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 1)
	if failedTask == nil || !failedTask.Cancelled() {
		t.Fatal("failed plugin task survived")
	}
	if len(handles) != 3 || handles[0].Name() != "first" || handles[1].Name() != "middle" || handles[2].Name() != "last" {
		t.Fatalf("enable order: %v", trace)
	}
	wantStartup := []string{"load first", "load middle", "load last", "enable first", "first saw enable first", "enable middle", "first saw enable middle", "middle saw enable middle", "enable last", "first saw enable last", "middle saw enable last", "last saw enable last", "first ready", "middle ready", "last ready"}
	if !slices.Equal(trace, wantStartup) {
		t.Fatalf("lifecycle event order: %v", trace)
	}
	h := handles[0]
	name, version := h.Version()
	if name != protocol.GameVersion || version != protocol.Version || h.Logger() == nil {
		t.Fatal("invalid server identity")
	}
	if got, want := h.DataDir(), filepath.Join("plugins", h.Name()); got != want {
		t.Errorf("DataDir=%q, want %q", got, want)
	}
	if _, err := os.Stat(h.DataDir()); err != nil {
		t.Fatal(err)
	}
	h.SetMaxPlayers(17)
	h.SetMOTD(plugin.Plain("plugin MOTD"))
	if h.MaxPlayers() != 17 || h.MOTD().Literal != "plugin MOTD" {
		t.Fatal("settings not changed")
	}
	infos := h.Plugins()
	for _, info := range infos {
		if info.Enabled != (info.Name == "first" || info.Name == "middle" || info.Name == "last") {
			t.Errorf("plugin state: %+v", info)
		}
	}
	plugin.Provide[fmt.Stringer](handles[0], service("low"), 0)
	plugin.Provide[fmt.Stringer](handles[2], service("high"), 10)
	if got, ok := plugin.Service[fmt.Stringer](h); !ok || got.String() != "high" {
		t.Fatalf("service: %v %v", got, ok)
	}
	if ps := h.Services().Providers(reflect.TypeFor[fmt.Stringer]()); len(ps) != 2 ||
		ps[0].Plugin != "last" || ps[0].Priority != 10 || ps[0].Impl.(fmt.Stringer).String() != "high" ||
		ps[1].Plugin != "first" || ps[1].Priority != 0 {
		t.Fatalf("providers (the failed plugin's priority-100 provider must be gone): %+v", ps)
	}
	if ps := plugin.Providers[fmt.Stringer](h); len(ps) != 2 || ps[0].Plugin != "last" || ps[0].Service.String() != "high" || ps[1].Plugin != "first" {
		t.Fatalf("typed providers: %+v", ps)
	}
	if ps := h.Services().Providers(reflect.TypeFor[io.Reader]()); ps == nil || len(ps) != 0 {
		t.Fatalf("unprovided providers: %#v", ps)
	}
	handles[2].Scheduler().RunLater(100, func() { t.Error("disabled task ran") })
	srv.Shutdown()
	if _, ok := plugin.Service[fmt.Stringer](h); ok {
		t.Error("service survived shutdown")
	}
	if !slices.Equal(trace[len(trace)-4:], []string{"middle saw disable middle", "disable middle", "first saw disable first", "disable first"}) {
		t.Fatalf("disable order or listener cleanup: %v", trace)
	}
	before := len(trace)
	srv.FirePluginEventForTest(&plugin.PluginDisableEvent{})
	if len(trace) != before {
		t.Error("disabled listeners survived")
	}
	if !h.Scheduler().RunTask(func() { t.Error("disabled handle scheduled task") }).Cancelled() {
		t.Error("disabled handle accepted task")
	}
	plugin.Provide[fmt.Stringer](h, service("late"), 100)
	if _, ok := plugin.Service[fmt.Stringer](h); ok {
		t.Error("disabled handle provided a service")
	}
}

func TestPluginEventPriorityCancellationAndPanicRollback(t *testing.T) {
	t.Parallel()
	var h plugin.Server
	var trace []string
	p := &testPlugin{enable: func(s plugin.Server) error { h = s; return nil }}
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "events", Plugin: p}})
	servertest.HoldTicks(t, srv)
	plugin.Listen(h, func(e *plugin.ServerListPingEvent) {
		trace = append(trace, "highest")
		if !e.Cancelled() || e.Max != 7 || e.Favicon[0] != 1 || e.MOTD.Children[0].Literal != "original" {
			t.Errorf("panic changes escaped: %+v", e)
		}
	}, plugin.ListenOptions{Priority: plugin.Highest})
	plugin.Listen(h, func(e *plugin.ServerListPingEvent) { trace = append(trace, "low"); e.SetCancelled(true); e.Max = 7 }, plugin.ListenOptions{Priority: plugin.Low})
	plugin.Listen(h, func(e *plugin.ServerListPingEvent) {
		trace = append(trace, "panic")
		e.SetCancelled(false)
		e.Max = 99
		e.Favicon[0] = 99
		e.MOTD.Children[0].Literal = "mutated"
		panic("listener")
	})
	plugin.Listen(h, func(*plugin.ServerListPingEvent) { t.Error("IgnoreCancelled listener ran") }, plugin.ListenOptions{Priority: plugin.High, IgnoreCancelled: true})
	removed := plugin.Listen(h, func(*plugin.ServerListPingEvent) { t.Error("unregistered listener ran") })
	removed.Unregister()
	plugin.Listen(h, func(e *plugin.ServerListPingEvent) { trace = append(trace, "monitor"); e.Max = 100 }, plugin.ListenOptions{Priority: plugin.Monitor})
	plugin.Listen(h, func(*plugin.ServerListPingEvent) { trace = append(trace, "monitor2") }, plugin.ListenOptions{Priority: plugin.Monitor})
	var once plugin.Listener
	once = plugin.Listen(h, func(*plugin.ServerListPingEvent) {
		once.Unregister()
		plugin.Listen(h, func(*plugin.ServerLoadEvent) {})
	}, plugin.ListenOptions{Priority: plugin.Lowest})
	event := &plugin.ServerListPingEvent{Favicon: []byte{1}, MOTD: plugin.Plain("").Append(plugin.Plain("original"))}
	h.Scheduler().RunTask(func() { srv.FirePluginEventForTest(event) })
	servertest.RunTicks(t, srv, 1)
	if !slices.Equal(trace, []string{"low", "panic", "highest", "monitor", "monitor2"}) || event.Max != 7 {
		t.Fatalf("priority/monitor: %v, max=%d", trace, event.Max)
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[int](), reflect.TypeFor[*plugin.Base](), reflect.TypeFor[*struct{ plugin.Base }]()} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("accepted invalid event %v", typ)
				}
			}()
			h.Events().Register(typ, func(plugin.Event) {}, plugin.ListenOptions{})
		}()
	}
}

func TestPluginSchedulerTicksCancellationAndDisable(t *testing.T) {
	t.Parallel()
	var h plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "tasks", Plugin: &testPlugin{enable: func(s plugin.Server) error { h = s; return nil }}}})
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)
	var trace []string
	plugin.Listen(h, func(e *plugin.ServerTickStartEvent) {
		trace = append(trace, "start")
		if e.Tick != h.Ticks().Current() {
			t.Error("start tick differs")
		}
	})
	plugin.Listen(h, func(e *plugin.ServerTickEndEvent) {
		trace = append(trace, "end")
		if e.Tick != h.Ticks().Current() || e.MSPT < 0 {
			t.Error("invalid end tick")
		}
	})
	base := h.Ticks().Current()
	var at []int64
	timer := h.Scheduler().RunTimer(2, 2, func() { at = append(at, h.Ticks().Current()-base) })
	h.Scheduler().RunTask(func() {
		trace = append(trace, "task")
		h.Scheduler().RunTask(func() { trace = append(trace, "nested") })
	})
	cancelled := h.Scheduler().RunLater(1, func() { t.Error("cancelled task ran") })
	cancelled.Cancel()
	cancelled.Cancel()
	panicTask := h.Scheduler().RunTimer(1, 1, func() { panic("task") })
	servertest.RunTicks(t, srv, 1)
	if !slices.Equal(trace, []string{"task", "start", "end"}) {
		t.Fatalf("task order: %v", trace)
	}
	if !panicTask.Cancelled() {
		t.Error("panicking timer remained scheduled")
	}
	servertest.RunTicks(t, srv, 3)
	if !slices.Equal(at, []int64{2, 4}) || trace[3] != "nested" || !h.Ticks().Frozen() || h.Ticks().Rate() != 20 {
		t.Fatalf("tick timing: %v %v", at, trace)
	}
	timer.Cancel()
	servertest.RunTicks(t, srv, 2)
	if len(at) != 2 {
		t.Error("cancelled timer repeated")
	}
	pending := h.Scheduler().RunLater(100, func() { t.Error("disabled task ran") })
	srv.Shutdown()
	if !pending.Cancelled() {
		t.Error("disable did not cancel pending task")
	}
}

func TestPluginAsyncSchedulerPoolAndCleanup(t *testing.T) {
	t.Parallel()
	var h plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "async", Plugin: &testPlugin{enable: func(s plugin.Server) error { h = s; return nil }}}})
	servertest.HoldTicks(t, srv)
	done := make(chan struct{})
	var count atomic.Int32
	timer := h.Scheduler().RunAsyncTimer(time.Millisecond, time.Millisecond, func() {
		if count.Add(1) == 2 {
			close(done)
		}
	})
	select {
	case <-done:
	case <-time.After(servertest.ExpectTimeout):
		t.Fatal("async timer did not run")
	}
	timer.Cancel()
	later := h.Scheduler().RunAsyncLater(time.Hour, func() { t.Error("disabled delayed async ran") })
	ready, release := make(chan struct{}, 4), make(chan struct{})
	for range 4 {
		h.Scheduler().RunAsync(func() { ready <- struct{}{}; <-release })
	}
	for range 4 {
		select {
		case <-ready:
		case <-time.After(servertest.ExpectTimeout):
			t.Fatal("async workers stalled")
		}
	}
	cancelled := h.Scheduler().RunAsync(func() { t.Error("cancelled queued async ran") })
	cancelled.Cancel()
	h.Scheduler().RunAsync(func() { panic("async") })
	close(release)
	completed := make(chan struct{})
	h.Scheduler().RunAsync(func() { close(completed) })
	select {
	case <-completed:
	case <-time.After(servertest.ExpectTimeout):
		t.Fatal("async pool stopped after panic")
	}
	srv.Shutdown()
	if !later.Cancelled() {
		t.Error("async task survived disable")
	}
}

func TestPluginLoadBeforeOrdering(t *testing.T) {
	t.Parallel()
	var order []string
	makePlugin := func(name string) *testPlugin {
		return &testPlugin{enable: func(plugin.Server) error { order = append(order, name); return nil }}
	}
	servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{
		{Name: "after", Plugin: makePlugin("after")},
		{Name: "before", Plugin: makePlugin("before"), LoadBefore: []string{"after"}},
	})
	if !slices.Equal(order, []string{"before", "after"}) {
		t.Fatalf("LoadBefore order: %v", order)
	}
}

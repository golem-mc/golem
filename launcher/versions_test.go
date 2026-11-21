package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A package two levels below its module resolves in about one proxy delay:
// the candidate paths are asked together, not one after another.
func TestPluginModuleAsksCandidatesConcurrently(t *testing.T) {
	f := newFixture(t, "")
	f.publish("example.com/mono", "v1.0.0", "", map[string]string{"mono.go": "package mono\n"})
	f.publish("example.com/mono/sub", "v1.1.0", "", map[string]string{"sub.go": "package sub\n"})

	const delay = 500 * time.Millisecond
	files := http.FileServer(http.Dir(f.proxy))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		files.ServeHTTP(w, r)
	}))
	defer srv.Close()
	t.Setenv("GOPROXY", srv.URL)

	// One candidate alone costs a few requests, so the yardstick is one
	// query, not the delay.
	one := time.Now()
	if _, err := (&launcher{}).queryModule(context.Background(), "example.com/mono/sub@latest"); err != nil {
		t.Fatal(err)
	}
	single := time.Since(one)

	l := &launcher{}
	start := time.Now()
	mod, v, err := l.pluginModule(context.Background(), "example.com/mono/sub/pkg", "latest")
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if mod != "example.com/mono/sub" || v != "v1.1.0" {
		t.Errorf("pluginModule = %s@%s, want example.com/mono/sub@v1.1.0", mod, v)
	}
	// Asked one after another it would be three queries.
	if took >= 2*single {
		t.Errorf("took %v, one query takes %v", took, single)
	}
}

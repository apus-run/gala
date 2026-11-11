package conf_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
)

func TestSnapshotLookupAndDecode(t *testing.T) {
	type server struct {
		Port int `conf:"port"`
	}
	type app struct {
		Server  server `conf:"server"`
		Version int    `conf:"version"`
	}
	raw := map[string]any{"server": map[string]any{"port": "0"}}
	src := &fakeSource{data: raw}
	c, err := conf.Open(t.Context(), app{Version: 7}, src)
	if err != nil {
		t.Fatal(err)
	}
	shot := c.Snapshot()
	if shot.Value != c.Current() || shot.Value.Version != 7 || shot.Exists("version") || !shot.Exists("server", "port") {
		t.Fatal("presence or snapshot mismatch")
	}
	port, err := conf.Get[int](shot.Tree, "server", "port")
	if err != nil || port != 0 {
		t.Fatal(port, err)
	}
	sub, err := shot.Cut("server")
	if err != nil {
		t.Fatal(err)
	}
	var srv server
	if err := sub.Decode(&srv); err != nil || srv.Port != 0 {
		t.Fatal(srv, err)
	}
	got, ok := shot.Lookup("server")
	if !ok {
		t.Fatal("root missing")
	}
	got.(map[string]any)["port"] = 999
	raw["server"].(map[string]any)["port"] = "bad"
	if err := c.Reload(t.Context()); err == nil || c.Current() != shot.Value {
		t.Fatal("failed reload published")
	}
	if port, _ := conf.Get[int](shot.Tree, "server", "port"); port != 0 {
		t.Fatal("aliased tree")
	}
	if _, err := conf.Get[string](shot.Tree, "missing"); !errors.Is(err, conf.ErrNotFound) {
		t.Fatal(err)
	}
	dst := struct {
		Port int
		Bad  int
	}{Port: 77}
	data, err := conf.OpenTree(t.Context(), &fakeSource{data: map[string]any{"Port": 1, "Bad": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := data.Current().Decode(&dst); err == nil || dst.Port != 77 {
		t.Fatal("partial decode")
	}
}

func TestDynamicTreeEditsExportAndReload(t *testing.T) {
	ctx := t.Context()
	s := &fakeSource{data: map[string]any{"a.b": []string{"x", "y"}, "": false, "big": json.Number("9007199254740993")}}
	c, err := conf.OpenTree(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Snapshot()
	if !reflect.DeepEqual(old.Keys(), []string{"", "a.b", "big"}) {
		t.Fatal(old.Keys())
	}
	if v, err := conf.Get[int64](old.Tree, "big"); err != nil || v != 9007199254740993 {
		t.Fatal(v, err)
	}
	if val, _ := old.Lookup("a.b", "0"); val != "x" || old.Exists("a", "b") || old.Exists("a.b", "01") {
		t.Fatal(val)
	}
	input := map[string][]int{"nums": {2, 3}}
	if err := c.Set(ctx, input, "new", "item"); err != nil {
		t.Fatal(err)
	}
	input["nums"][0] = 900
	if err := c.Delete(ctx, "a.b", "0"); err != nil {
		t.Fatal(err)
	}
	if val, _ := c.Current().Lookup("a.b", "0"); val != "y" {
		t.Fatal(val)
	}
	if err := c.Set(ctx, 3, "a.b", "3"); !errors.Is(err, conf.ErrNotFound) {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, "absent"); !errors.Is(err, conf.ErrNotFound) {
		t.Fatal(err)
	}
	snap := c.Snapshot()
	m, err := conf.Get[map[string]any](snap.Tree, "new", "item")
	if err != nil {
		t.Fatal(err)
	}
	list := m["nums"].([]any)
	if list[0] != 2 {
		t.Fatal(list)
	}
	list[0] = 800
	sub, err := snap.Cut("new")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.MergeAt(ctx, sub, "copy"); err != nil {
		t.Fatal(err)
	}
	if val, _ := c.Current().Lookup("copy", "item", "nums", "0"); val != 2 {
		t.Fatal(val)
	}
	_, err = snap.Marshal(func(v any) ([]byte, error) { delete(v.(map[string]any), "new"); return nil, nil })
	if err != nil || !snap.Exists("new") {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(sub)
	if err != nil || string(encoded) != `{"item":{"nums":[2,3]}}` {
		t.Fatal(string(encoded), err)
	}
	s.err = errors.New("unavailable")
	if err := c.Reload(ctx); err == nil || !c.Current().Exists("copy") {
		t.Fatal("failed reload discarded edits")
	}
	s.err = nil
	if err := c.Reload(ctx); err != nil || c.Current().Exists("copy") || c.Current().Exists("new") {
		t.Fatal("successful reload retained edits", err)
	}
	if !snap.Exists("new") || old.Exists("new") {
		t.Fatal("stale snapshot mutated")
	}
}

func TestSubscriptionsAfterSuccessfulPublish(t *testing.T) {
	ctx := t.Context()
	type config struct{ N int }
	low, high := &fakeSource{data: map[string]any{"N": 1}}, &fakeSource{data: map[string]any{"N": 2}}
	c, err := conf.Open(ctx, config{}, low, high)
	if err != nil {
		t.Fatal(err)
	}
	ch, stop := c.Subscribe("N")
	defer stop()
	quiet := func() {
		t.Helper()
		select {
		case got := <-ch:
			t.Fatal("unexpected change", got)
		default:
		}
	}
	quiet()
	low.data["N"] = 3
	if err = c.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	quiet()
	if err = c.Set(ctx, "bad", "N"); err == nil {
		t.Fatal("invalid edit")
	}
	quiet()
	if err = c.Delete(ctx, "N"); err != nil {
		t.Fatal(err)
	}
	deleted := <-ch
	if v, _ := deleted.Before.Lookup("N"); v != 2 || deleted.After.Exists("N") {
		t.Fatal(deleted)
	}
	if err = c.Set(ctx, 0, "N"); err != nil {
		t.Fatal(err)
	}
	added := <-ch
	if v, ok := added.After.Lookup("N"); v != 0 || !ok || added.Before.Exists("N") {
		t.Fatal(added)
	}
	for _, n := range []int{4, 5, 6} {
		if err = c.Set(ctx, n, "N"); err != nil {
			t.Fatal(err)
		}
	}
	last := <-ch
	if v, _ := last.After.Lookup("N"); v != 6 {
		t.Fatal(last)
	}
	stop()
	stop()
	if _, ok := <-ch; ok {
		t.Fatal("subscription open")
	}
}

func TestConcurrentSnapshotsAndSubscriptions(t *testing.T) {
	ctx := t.Context()
	c, err := conf.Open(ctx, struct{ N int }{}, &fakeSource{data: map[string]any{"N": 1}})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 1; i <= 300; i++ {
			if err := c.Set(ctx, i, "N"); err != nil {
				t.Error(err)
				return
			}
		}
	})
	for range 4 {
		wg.Go(func() {
			for range 200 {
				ch, stop := c.Subscribe("N")
				s := c.Snapshot()
				n, _ := s.Lookup("N")
				if n != s.Value.N {
					t.Error("mixed version")
					return
				}
				stop()
				for range ch {
				}
			}
		})
	}
	wg.Wait()
}

func TestLoadAndHooksAreTransactional(t *testing.T) {
	ctx := t.Context()
	base := &fakeSource{data: map[string]any{"N": 1}}
	c, err := conf.OpenWith(ctx, conf.Tree{}, conf.Options{Merge: conf.StrictMerge}, base)
	if err != nil {
		t.Fatal(err)
	}
	extra := &fakeSource{data: map[string]any{"N": "x"}}
	old := c.Current()
	if err = c.Load(ctx, extra); err == nil || c.Current() != old {
		t.Fatal("failed append committed")
	}
	if err = c.Reload(ctx); err != nil || extra.calls != 1 {
		t.Fatal(err, extra.calls)
	}
	extra.data = map[string]any{"M": 2}
	if err = c.Load(ctx, extra); err != nil {
		t.Fatal(err)
	}
	extra.data["M"] = 3
	if err = c.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.Current().Lookup("M"); v != 3 {
		t.Fatal(v)
	}
	var retained map[string]any
	custom, err := conf.OpenWith(ctx, conf.Tree{}, conf.Options{Merge: func(dst, src map[string]any) error { dst["N"] = src["N"]; retained = dst; return nil }}, base)
	if err != nil {
		t.Fatal(err)
	}
	retained["N"] = 99
	if v, _ := custom.Current().Lookup("N"); v != 1 {
		t.Fatal("merge aliases published value")
	}
}

func TestPlaceholderResolution(t *testing.T) {
	ctx := t.Context()
	src := &fakeSource{data: map[string]any{
		"server": map[string]any{"port": 8080}, "port": "${server.port}", "addr": "localhost:${port}", "list": []any{"${missing:local}"}, "empty": "", "joined": "${empty}${port}",
	}}
	c, err := conf.OpenWith(ctx, conf.Tree{}, conf.Options{Resolve: conf.Expand}, src)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]any{"port": 8080, "addr": "localhost:8080", "joined": "8080"} {
		if v, _ := c.Current().Lookup(name); v != want {
			t.Fatalf("%s=%#v", name, v)
		}
	}
	if v, _ := c.Current().Lookup("list", "0"); v != "local" {
		t.Fatal(v)
	}
	if err = c.Set(ctx, 9090, "server", "port"); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.Current().Lookup("addr"); v != "localhost:9090" {
		t.Fatal(v)
	}
	old := c.Current()
	for _, value := range []any{"${missing}", "${port}", "${list}", "${", "SECRET${}"} {
		if err = c.Set(ctx, value, "port"); err == nil || strings.Contains(err.Error(), "SECRET") || c.Current() != old {
			t.Fatal("invalid resolve", err)
		}
	}
	if err = c.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.Current().Lookup("addr"); v != "localhost:8080" {
		t.Fatal(v)
	}
}

func ExampleOpenTree() {
	ctx := context.Background()
	cfg, err := conf.OpenTree(ctx)
	if err != nil {
		panic(err)
	}
	changes, cancel := cfg.Subscribe("log", "level")
	defer cancel()
	if err := cfg.Set(ctx, "debug", "log", "level"); err != nil {
		panic(err)
	}
	change := <-changes
	level, err := conf.Get[string](change.After, "log", "level")
	if err != nil {
		panic(err)
	}
	fmt.Println(change.Before.Exists("log", "level"), level)
	if err := cfg.Reload(ctx); err != nil {
		panic(err)
	}
	fmt.Println(cfg.Current().Exists("log", "level"))
	// Output:
	// false debug
	// false
}

func TestEditResolutionAndLimits(t *testing.T) {
	ctx := t.Context()
	c, err := conf.OpenTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Set(ctx, 1, "x"); err != nil {
		t.Fatal(err)
	}
	old := c.Current()
	if err = c.Delete(ctx, "x", "missing"); !errors.Is(err, conf.ErrNotFound) {
		t.Fatal(err)
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	for _, bad := range []any{cycle, nil, make([]string, 65536)} {
		if err = c.Set(ctx, bad, "x"); err == nil || c.Current() != old {
			t.Fatal("oversized or invalid edit committed")
		}
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err = c.Set(canceled, 2, "x"); !errors.Is(err, context.Canceled) || c.Current() != old {
		t.Fatal(err)
	}
	// Each reference doubles a bounded scalar. Expansion must reject the
	// candidate before installing it, even though the input tree is small.
	raw := map[string]any{"v0": "abcdefgh"}
	for i := 1; i < 25; i++ {
		ref := fmt.Sprintf("${v%d}", i-1)
		raw[fmt.Sprintf("v%d", i)] = ref + ref
	}
	orig := raw["v24"]
	if err := conf.Expand(raw); err == nil || raw["v24"] != orig {
		t.Fatal("unbounded or partial expansion")
	}
	if err := conf.Expand(cycle); err == nil {
		t.Fatal("cyclic input")
	}
	if err = c.Delete(ctx); err != nil || len(c.Current().Keys()) != 0 {
		t.Fatal(err)
	}
}

func TestResolverOutputOwnership(t *testing.T) {
	var work map[string]any
	cfg, err := conf.OpenWith(t.Context(), conf.Tree{}, conf.Options{Resolve: func(m map[string]any) error {
		work = m
		return nil
	}}, &fakeSource{data: map[string]any{"map": map[string]int{"n": 1}}})
	if err != nil {
		t.Fatal(err)
	}
	work["map"].(map[string]any)["n"] = 7
	got, _ := cfg.Current().Lookup("map", "n")
	if got != 1 {
		t.Fatal("resolver retained published tree")
	}
}

func TestStrictMergeAtAndDefaultPlaceholders(t *testing.T) {
	ctx := t.Context()
	c, err := conf.OpenWith(ctx, conf.Tree{}, conf.Options{Merge: conf.StrictMerge}, &fakeSource{data: map[string]any{"target": map[string]any{"n": 1}, "literal": "${x}"}})
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := conf.OpenTree(ctx, &fakeSource{data: map[string]any{"n": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	old := c.Current()
	if err = c.MergeAt(ctx, *overlay.Current(), "target"); err == nil || c.Current() != old {
		t.Fatal("strict merge partially published")
	}
	if v, _ := old.Lookup("literal"); v != "${x}" {
		t.Fatal(v)
	}
}

func TestSubscribeSnapshotPublicationBoundary(t *testing.T) {
	ctx := t.Context()
	cfg, err := conf.Open(ctx, struct{ N int }{}, &fakeSource{data: map[string]any{"N": 0}})
	if err != nil {
		t.Fatal(err)
	}

	var readers sync.WaitGroup
	start := make(chan struct{})
	for range 4 {
		readers.Go(func() {
			<-start
			for range 250 {
				initial, changes, cancel := cfg.SubscribeSnapshot("N")
				fromTree, _ := initial.Lookup("N")
				if initial.Value.N != fromTree {
					t.Error("mixed initial snapshot")
				}
				cancel()
				for change := range changes {
					before, _ := conf.Get[int](change.Before, "N")
					after, _ := conf.Get[int](change.After, "N")
					if before < initial.Value.N || after <= initial.Value.N {
						t.Errorf("initial %d followed by %d -> %d", initial.Value.N, before, after)
					}
				}
			}
		})
	}
	close(start)
	for i := 1; i <= 500; i++ {
		if err := cfg.Set(ctx, i, "N"); err != nil {
			t.Fatal(err)
		}
	}
	readers.Wait()

	initial, changes, cancel := cfg.SubscribeSnapshot("N")
	defer cancel()
	if err := cfg.Set(ctx, 501, "N"); err != nil {
		t.Fatal(err)
	}
	event := <-changes
	before, _ := event.Before.Lookup("N")
	after, _ := event.After.Lookup("N")
	if before != initial.Value.N || after != 501 {
		t.Fatal("first change did not follow initial snapshot")
	}
}

func TestSubscriptionNaNEquality(t *testing.T) {
	type rate float32
	ctx := t.Context()
	c, err := conf.OpenTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ original, changed any }{
		{math.NaN(), 1.0},
		{rate(math.NaN()), float32(math.NaN())},
		{map[string]any{"items": []any{math.NaN(), rate(math.NaN())}}, map[string]any{"items": []any{math.NaN(), rate(1)}}},
	} {
		if err := c.Set(ctx, tc.original, "watched"); err != nil {
			t.Fatal(err)
		}
		_, ch, stop := c.SubscribeSnapshot("watched")
		if err := c.Set(ctx, 1, "other"); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ch:
			t.Fatal("unchanged NaN notified")
		default:
		}
		if err := c.Set(ctx, tc.changed, "watched"); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ch:
		default:
			t.Fatal("real change missed")
		}
		stop()
	}
}

func TestExpandDepthIndependentOfCache(t *testing.T) {
	// Visit the list in index order so each suffix is already memoized by the
	// time the next, longer chain needs it. A mixed string has two references.
	makeChain := func(n int) map[string]any {
		items := make([]any, n+1)
		items[0] = "v"
		for i := 1; i <= n; i++ {
			items[i] = fmt.Sprintf("${items.0}${items.%d}", i-1)
		}
		return map[string]any{"items": items}
	}
	if err := conf.Expand(makeChain(128)); err != nil {
		t.Fatal(err)
	}
	if err := conf.Expand(makeChain(129)); err == nil {
		t.Fatal("cached overlong chain accepted")
	}

	layer := map[string]any{"stable": 1}
	source := conf.SourceFunc(func(context.Context) (map[string]any, error) { return layer, nil })
	cfg, err := conf.OpenWith(t.Context(), conf.Tree{}, conf.Options{Resolve: conf.Expand}, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Set(t.Context(), 2, "stable"); err != nil {
		t.Fatal(err)
	}
	old := cfg.Current()
	_, ch, stop := cfg.SubscribeSnapshot()
	defer stop()
	layer = map[string]any{"k200": "v"}
	for i := 0; i < 200; i++ {
		layer[fmt.Sprint("k", i)] = fmt.Sprintf("${k%d}", i+1)
	}
	for range 50 {
		var ce *conf.Error
		if err := cfg.Reload(t.Context()); !errors.As(err, &ce) || ce.Stage != conf.StageResolve || cfg.Current() != old {
			t.Fatal("overlong reload published", err)
		}
	}
	select {
	case <-ch:
		t.Fatal("rejected reload notified")
	default:
	}
}

func TestPostPublicationFollowUpEdit(t *testing.T) {
	ctx := t.Context()
	cfg, err := conf.OpenTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, changes, stop := cfg.SubscribeSnapshot("input")
	defer stop()
	done := make(chan error, 1)
	go func() {
		for change := range changes {
			value, _ := change.After.Lookup("input")
			done <- cfg.Set(ctx, value, "derived")
			return
		}
	}()
	if err := cfg.Set(ctx, "new", "input"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if v, _ := cfg.Current().Lookup("derived"); v != "new" {
		t.Fatal(v)
	}
}

func TestExpandNamedStrings(t *testing.T) {
	type Text string
	input := map[string]any{
		"port":     json.Number("9007199254740993"),
		"name":     Text("host"),
		"direct":   Text("${port}"),
		"indirect": "${direct}",
		"mixed":    Text("${name}:${direct}"),
		"list":     []any{Text("${name}")},
	}
	if err := conf.Expand(input); err != nil {
		t.Fatal(err)
	}
	if input["direct"] != input["port"] || input["indirect"] != input["port"] ||
		input["mixed"] != "host:9007199254740993" || input["list"].([]any)[0] != Text("host") {
		t.Fatalf("unexpected named-string expansion: %#v", input)
	}
	for _, value := range []any{Text("${missing}"), Text("${x}")} {
		if err := conf.Expand(map[string]any{"x": value}); err == nil {
			t.Fatal("accepted unresolved named string")
		}
	}
}

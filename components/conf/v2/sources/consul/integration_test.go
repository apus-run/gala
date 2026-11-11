//go:build integration

package consul_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/consul"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
	"github.com/hashicorp/consul/api"
)

func live(t *testing.T, token string) *api.Client {
	t.Helper()
	cfg := api.DefaultConfig()
	cfg.Address = os.Getenv("CONF_TEST_CONSUL")
	cfg.Token = token
	if cfg.Address == "" || token == "" {
		t.Fatal("run testdata/ci/integration.sh with ACL-enabled Consul")
	}
	c, err := api.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func store(t *testing.T, ctx context.Context, kv *api.KV, key, value string) {
	t.Helper()
	if _, err := kv.Put(&api.KVPair{Key: key, Value: []byte(value)}, (&api.WriteOptions{}).WithContext(ctx)); err != nil {
		t.Fatal(err)
	}
}
func awaitSignal(t *testing.T, ctx context.Context, ch <-chan struct{}) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("watch closed")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func TestIntegrationDocument(t *testing.T) { testIntegrationMode(t, false) }
func TestIntegrationPrefix(t *testing.T)   { testIntegrationMode(t, true) }
func testIntegrationMode(t *testing.T, prefix bool) {
	kv := live(t, os.Getenv("CONF_TEST_CONSUL_TOKEN")).KV()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	key := "conf-test/" + t.Name()
	s := consul.New(kv, key, file.JSON)
	first, next := `{"Port":1}`, `{"Port":2}`
	if prefix {
		s = consul.NewPrefix(kv, key)
		store(t, ctx, kv, key+"/Other", "4")
		key += "/Port"
		first, next = "1", "2"
	}
	store(t, ctx, kv, key, first)
	c, err := conf.Open(ctx, struct{ Port, Other int }{}, s)
	if err != nil || c.Current().Port != 1 {
		t.Fatal(err)
	}
	ch, err := s.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, ctx, ch)
	store(t, ctx, kv, key, next)
	awaitSignal(t, ctx, ch)
	if err := c.Reload(ctx); err != nil || c.Current().Port != 2 {
		t.Fatal(err)
	}
	remove := func(k string) {
		t.Helper()
		if _, err := kv.Delete(k, (&api.WriteOptions{}).WithContext(ctx)); err != nil {
			t.Fatal(err)
		}
		awaitSignal(t, ctx, ch)
	}
	remove(key)
	if prefix {
		if err := c.Reload(ctx); err != nil || c.Current().Port != 0 || c.Current().Other != 4 {
			t.Fatal(err)
		}
		remove(strings.TrimSuffix(key, "Port") + "Other")
	}
	old := c.Current()
	if err := c.Reload(ctx); !errors.Is(err, fs.ErrNotExist) || c.Current() != old {
		t.Fatal(err)
	}
}
func TestIntegrationACLFilteredPrefix(t *testing.T) {
	admin := live(t, os.Getenv("CONF_TEST_CONSUL_TOKEN"))
	ctx := t.Context()
	policy, _, err := admin.ACL().PolicyCreate(&api.ACLPolicy{Name: fmt.Sprintf("partial-%d", time.Now().UnixNano()), Rules: `key_prefix "conf-acl/" { policy = "read" }
key "conf-acl/Hidden" { policy = "deny" }`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := admin.ACL().TokenCreate(&api.ACLToken{Policies: []*api.ACLTokenPolicyLink{{ID: policy.ID}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	store(t, ctx, admin.KV(), "conf-acl/Visible", "7")
	s := consul.NewPrefix(live(t, tok.SecretID).KV(), "conf-acl")
	cfg, err := conf.Open(ctx, struct{ Visible, Hidden int }{}, s)
	if err != nil {
		t.Fatal(err)
	}
	old := cfg.Current()
	store(t, ctx, admin.KV(), "conf-acl/Hidden", "9")
	err = cfg.Reload(ctx)
	if old.Visible != 7 || !errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) || cfg.Current() != old {
		t.Fatalf("partial response: %v", err)
	}
}

package authz

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	ra "github.com/apus-run/gala/components/authz/adapter/redis"
	"github.com/casbin/casbin/v3/model"
	"github.com/redis/go-redis/v9"
)

func TestFailedRedisSaveCannotRemoveDenyOnReload(t *testing.T) {
	checkFailedPolicySave(t, true)
}

// Keep the policy, failure point and reload sequence identical. Only the
// publication mechanism changes. The unsafe control reproduces the reviewed P1.
func TestAblationRedisSnapshotPublication(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(map[bool]string{false: "in_place", true: "staged"}[staged], func(t *testing.T) {
			checkFailedPolicySave(t, staged)
		})
	}
}

func checkFailedPolicySave(t *testing.T, staged bool) {
	t.Helper()
	s := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: s.Addr(), MaxRetries: -1})
	defer c.Close()
	adapter, err := ra.NewAdapter(&ra.Config{Client: c})
	if err != nil {
		t.Fatal(err)
	}
	text := `[request_definition]
r = sub, obj, act
[policy_definition]
p = sub, obj, act, eft
[policy_effect]
e = some(where (p.eft == allow)) && !some(where (p.eft == deny))
[matchers]
m = r.sub == p.sub && r.obj == p.obj && r.act == p.act
`
	m, err := model.NewModelFromString(text)
	if err != nil {
		t.Fatal(err)
	}
	m.AddPolicy("p", "p", []string{"alice", "doc", "read", "allow"})
	m.AddPolicy("p", "p", []string{"alice", "doc", "read", "deny"})
	if err := adapter.SavePolicy(m); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "model.conf")
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(WithModelFilePath(file), WithPolicyAdapter(adapter), WithMaxPolicyAge(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := a.Enforce("alice", "doc", "read"); err != nil || ok {
		t.Fatalf("before allowed=%v err=%v", ok, err)
	}
	count := 0
	s.Server().SetPreHook(func(p *server.Peer, cmd string, args ...string) bool {
		if strings.EqualFold(cmd, "rpush") {
			count++
			if count == 2 {
				p.WriteError("ERR injected mid-script failure")
				return true
			}
		}
		return false
	})
	var saveErr error
	if staged {
		saveErr = adapter.SavePolicy(m)
	} else {
		// Test-only control: the original in-place replacement algorithm.
		rows := make([]any, 0, len(m["p"]["p"].Policy))
		for _, rule := range m["p"]["p"].Policy {
			row, err := json.Marshal(ra.CasbinRule{PType: "p", V0: rule[0], V1: rule[1], V2: rule[2], V3: rule[3]})
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, row)
		}
		saveErr = c.Eval(context.Background(), `
            redis.call('del', KEYS[1])
            for i=1,#ARGV do redis.call('rpush', KEYS[1], ARGV[i]) end
            return 1
        `, []string{"casbin_rules"}, rows...).Err()
	}
	s.Server().SetPreHook(nil)
	if saveErr == nil || count != 2 {
		t.Fatalf("expected second RPUSH failure: count=%d err=%v", count, saveErr)
	}
	n, err := c.LLen(context.Background(), "casbin_rules").Result()
	wantRows := int64(1)
	if staged {
		wantRows = 2
	}
	if err != nil || n != wantRows {
		t.Fatalf("rules=%d err=%v", n, err)
	}
	if err := a.LoadPolicy(); err != nil {
		t.Fatal(err)
	}
	if ok, err := a.Enforce("alice", "doc", "read"); err != nil || ok == staged {
		t.Fatalf("staged=%v after allowed=%v err=%v", staged, ok, err)
	}
	t.Logf("staged=%v: stored rules=%d, allowed after reload=%v", staged, n, !staged)
}

package redisadapter

import (
	"reflect"
	"testing"

	"github.com/casbin/casbin/v3"
)

func TestFilteredOperationsMatchDecodedValues(t *testing.T) {
	for _, value := range []string{`alice"`, `alice\`, `alice&bob`, `<alice>`, `alice%[bob]`, `用户🙂`} {
		t.Run(value, func(t *testing.T) {
			a, _, _ := testRedisAdapter(t)
			target := []string{value, "data1", "read"}
			other := []string{value + "-other", "data1", "read"}
			for _, rule := range [][]string{target, other} {
				if err := a.AddPolicy("p", "p", rule); err != nil {
					t.Fatal(err)
				}
			}
			filtered := policyModel(t)
			if err := a.LoadFilteredPolicy(filtered, Filter{V0: []string{value}}); err != nil {
				t.Fatal(err)
			}
			if got := filtered["p"]["p"].Policy; !reflect.DeepEqual(got, [][]string{target}) {
				t.Fatalf("filtered policy=%v", got)
			}
			if err := a.LoadPolicy(policyModel(t)); err != nil {
				t.Fatal(err)
			}
			// The public Casbin API must update both storage and memory consistently.
			e, err := casbin.NewEnforcer("examples/rbac_model.conf", a)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.UpdateFilteredPolicies([][]string{{value, "data1", "write"}}, 0, value); err != nil {
				t.Fatal(err)
			}
			if err := e.LoadPolicy(); err != nil {
				t.Fatal(err)
			}
			if allowed, err := e.Enforce(value, "data1", "read"); err != nil || allowed {
				t.Fatalf("old policy retained after update: %v %v", allowed, err)
			}
			if allowed, err := e.Enforce(value, "data1", "write"); err != nil || !allowed {
				t.Fatalf("new policy absent: %v %v", allowed, err)
			}
			if _, err := e.RemoveFilteredPolicy(0, value); err != nil {
				t.Fatal(err)
			}
			if err := e.LoadPolicy(); err != nil {
				t.Fatal(err)
			}
			if allowed, err := e.Enforce(value, "data1", "write"); err != nil || allowed {
				t.Fatalf("revoked grant survived reload: %v %v", allowed, err)
			}
			if allowed, err := e.Enforce(other[0], "data1", "read"); err != nil || !allowed {
				t.Fatalf("unrelated grant changed: %v %v", allowed, err)
			}
		})
	}
}

func TestMalformedPolicyReturnsErrorBeforeFilteredMutation(t *testing.T) {
	for _, text := range []string{"null", "{}", `{"PType":""}`, `{"PType":false}`, `not json`} {
		t.Run(text, func(t *testing.T) {
			a, s, _ := testRedisAdapter(t)
			if err := a.AddPolicy("p", "p", []string{"alice", "data1", "read"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.RPush(a.key, text); err != nil {
				t.Fatal(err)
			}
			if err := a.LoadPolicy(policyModel(t)); err == nil {
				t.Fatal("malformed policy accepted")
			}
			if err := a.LoadFilteredPolicy(policyModel(t), Filter{V0: []string{"alice"}}); err == nil {
				t.Fatal("filtered load ignored malformed policy")
			}
			before, err := s.List(a.key)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.RemoveFilteredPolicy("p", "p", 0, "alice"); err == nil {
				t.Fatal("filtered delete accepted malformed policy")
			}
			if _, err := a.UpdateFilteredPolicies("p", "p", [][]string{{"bob", "data1", "read"}}, 0, "alice"); err == nil {
				t.Fatal("filtered update accepted malformed policy")
			}
			after, err := s.List(a.key)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("malformed policy caused partial mutation: %v", after)
			}
		})
	}
}

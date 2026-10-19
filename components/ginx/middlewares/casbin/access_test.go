package casbin

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"strings"
	"testing"
)

func TestAccessTruthTable(t *testing.T) {
	for bits := 0; bits < 8; bits++ {
		for _, p := range []Possession{PossessionAny, PossessionOwn, PossessionOwnOrAny} {
			t.Run(fmt.Sprintf("bits=%d/scope=%d", bits, p), func(t *testing.T) {
				anyGrant, own, owner := bits&1 != 0, bits&2 != 0, bits&4 != 0
				calls := 0
				b := NewBuilder().SetLookup(lookup).SetEnforcer(enforcerFunc(func(v ...any) (bool, error) {
					if len(v) != 3 || v[0] != "alice" || v[1] != "doc" {
						t.Fatal(v)
					}
					switch v[2] {
					case "read:any":
						return anyGrant, nil
					case "read:own":
						return own, nil
					default:
						t.Fatal(v)
						return false, nil
					}
				}))
				a := Access{Resource: "doc", Action: ActionRead, Possession: p}
				if p != PossessionAny {
					a.IsOwn = func(*gin.Context) (bool, error) { calls++; return owner, nil }
				}
				h, err := b.RequiresAccess([]Access{a})
				if err != nil {
					t.Fatal(err)
				}
				want := anyGrant
				if p == PossessionOwn {
					want = own && owner
				}
				if p == PossessionOwnOrAny {
					want = anyGrant || (own && owner)
				}
				status, _, downstream := serveAuthorization(h)
				wantStatus := 403
				if want {
					wantStatus = 204
				}
				if status != wantStatus || (downstream == 1) != want {
					t.Fatalf("status=%d downstream=%d want=%v", status, downstream, want)
				}
				wantCalls := 0
				if own && (p == PossessionOwn || (p == PossessionOwnOrAny && !anyGrant)) {
					wantCalls = 1
				}
				if calls != wantCalls {
					t.Fatalf("IsOwn calls=%d want=%d", calls, wantCalls)
				}
			})
		}
	}
}
func TestAccessErrorsAndIdentity(t *testing.T) {
	failure := errors.New("injected")
	for _, tc := range []struct {
		name, subject        string
		enforceErr, ownerErr error
		want                 int
	}{{name: "missing identity", want: 401}, {name: "enforce error", subject: "alice", enforceErr: failure, want: 500}, {name: "ownership error", subject: "alice", ownerErr: failure, want: 500}} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuilder().SetLookup(func(*gin.Context) (string, error) { return tc.subject, nil }).SetEnforcer(enforcerFunc(func(...any) (bool, error) { return true, tc.enforceErr }))
			h, err := b.RequiresAccess([]Access{{Resource: "doc", Action: "read", Possession: PossessionOwn, IsOwn: func(*gin.Context) (bool, error) { return true, tc.ownerErr }}})
			if err != nil {
				t.Fatal(err)
			}
			if status, _, n := serveAuthorization(h); status != tc.want || n != 0 {
				t.Fatal(status, n)
			}
		})
	}
}
func TestAccessValidationAndEmpty(t *testing.T) {
	b := NewBuilder().SetLookup(func(*gin.Context) (string, error) { t.Error("lookup on empty list"); return "", nil }).SetEnforcer(enforcerFunc(func(...any) (bool, error) { t.Error("enforce on empty list"); return false, nil }))
	h, err := b.RequiresAccess(nil)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, _ := serveAuthorization(h); code != 204 {
		t.Fatal(code)
	}
	for _, a := range []Access{{}, {Resource: "doc", Action: "read"}, {Resource: "doc", Action: "read:any", Possession: PossessionAny}, {Resource: "doc", Action: "read", Possession: PossessionOwn}, {Resource: "doc", Action: "read", Possession: PossessionAny, IsOwn: func(*gin.Context) (bool, error) { return true, nil }}} {
		if _, err := b.RequiresAccess([]Access{a}); err == nil || !strings.Contains(err.Error(), "access[0]") {
			t.Fatal(err)
		}
	}
}
func TestAccessListShortCircuitAndCapturedCopy(t *testing.T) {
	for _, rule := range []ValidationRule{MatchAllRule, AtLeastOneRule} {
		calls := 0
		b := NewBuilder().SetLookup(lookup).SetEnforcer(enforcerFunc(func(v ...any) (bool, error) {
			calls++
			if v[1] != "first" {
				t.Fatal("unexpected resource", v)
			}
			return rule == AtLeastOneRule, nil
		}))
		list := []Access{{Resource: "first", Action: "read", Possession: PossessionAny}, {Resource: "second", Action: "read", Possession: PossessionAny}}
		h, err := b.RequiresAccess(list, WithValidationRule(rule))
		if err != nil {
			t.Fatal(err)
		}
		list[0].Resource = "mutated"
		status, _, _ := serveAuthorization(h)
		if calls != 1 || (status == 204) != (rule == AtLeastOneRule) {
			t.Fatal(status, calls)
		}
	}
}

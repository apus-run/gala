package casbin

import (
	"errors"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

type roleEnforcer struct {
	enforcerFunc
	getRoles func(string, ...string) ([]string, error)
}

func (r roleEnforcer) GetRolesForUser(s string, d ...string) ([]string, error) {
	return r.getRoles(s, d...)
}
func serveAuthorization(h gin.HandlerFunc) (status int, body string, downstream int) {
	r := gin.New()
	r.GET("/", h, func(c *gin.Context) { downstream++; c.Status(204) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	return w.Code, w.Body.String(), downstream
}

func TestPermissionRulesAndShortCircuit(t *testing.T) {
	failure := errors.New("sensitive failure")
	for _, tc := range []struct {
		name          string
		rules         []string
		rule          ValidationRule
		status, calls int
	}{
		{"all", []string{"yes:read", "yes:write"}, MatchAllRule, 204, 2},
		{"all stops at denial", []string{"no:read", "error:read"}, MatchAllRule, 403, 1},
		{"all later denial", []string{"yes:read", "no:read"}, MatchAllRule, 403, 2},
		{"any", []string{"no:read", "yes:read"}, AtLeastOneRule, 204, 2},
		{"any stops at success", []string{"yes:read", "error:read"}, AtLeastOneRule, 204, 1},
		{"any denied", []string{"no:read", "no:write"}, AtLeastOneRule, 403, 2},
		{"all error", []string{"error:read", "yes:read"}, MatchAllRule, 500, 1},
		{"any error before success", []string{"error:read", "yes:read"}, AtLeastOneRule, 500, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			b := NewBuilder().SetLookup(lookup).SetEnforcer(enforcerFunc(func(values ...any) (bool, error) {
				calls++
				if len(values) != 3 || values[0] != "alice" {
					t.Error(values)
				}
				if values[1] == "error" {
					return false, failure
				}
				return values[1] == "yes", nil
			}))
			h, err := b.RequiresPermissions(tc.rules, WithValidationRule(tc.rule))
			if err != nil {
				t.Fatal(err)
			}
			status, body, downstream := serveAuthorization(h)
			if status != tc.status || calls != tc.calls || body != "" {
				t.Fatal(status, calls, body)
			}
			if (status == 204) != (downstream == 1) {
				t.Fatal("invalid downstream execution", downstream)
			}
		})
	}
}

func TestEmptyRequirementsSkipLookup(t *testing.T) {
	b := NewBuilder().SetLookup(func(*gin.Context) (string, error) { t.Error("unexpected lookup"); return "", nil }).
		SetEnforcer(enforcerFunc(func(...any) (bool, error) { t.Error("unexpected enforce"); return false, nil }))
	for _, rule := range []ValidationRule{MatchAllRule, AtLeastOneRule} {
		for _, build := range []func([]string, ...Option) (gin.HandlerFunc, error){b.RequiresPermissions, b.RequiresRoles} {
			h, err := build(nil, WithValidationRule(rule))
			if err != nil {
				t.Fatal(err)
			}
			if status, _, count := serveAuthorization(h); status != 204 || count != 1 {
				t.Fatal(status, count)
			}
		}
	}
}

func TestRoleAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name                string
		subject             string
		roles               []string
		rule                ValidationRule
		lookupErr, rolesErr error
		status              int
	}{
		{name: "all", subject: "alice", roles: []string{"reader", "admin"}, status: 204},
		{name: "any", subject: "alice", roles: []string{"unknown", "reader"}, rule: AtLeastOneRule, status: 204},
		{name: "missing role", subject: "alice", roles: []string{"unknown"}, status: 403},
		{name: "all missing one", subject: "alice", roles: []string{"reader", "unknown"}, status: 403},
		{name: "any all missing", subject: "alice", roles: []string{"unknown", "other"}, rule: AtLeastOneRule, status: 403},
		{name: "no identity", roles: []string{"reader"}, status: 401},
		{name: "lookup error", roles: []string{"reader"}, lookupErr: errors.New("lookup failure"), status: 500},
		{name: "role error", subject: "alice", roles: []string{"reader"}, rolesErr: errors.New("role failure"), status: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			b := NewBuilder().SetLookup(func(*gin.Context) (string, error) { return tc.subject, tc.lookupErr }).SetEnforcer(roleEnforcer{
				enforcerFunc: func(...any) (bool, error) { t.Error("roles called Enforce"); return false, nil },
				getRoles: func(subject string, domain ...string) ([]string, error) {
					calls++
					if subject != "alice" || len(domain) != 0 {
						t.Error(subject, domain)
					}
					return []string{"reader", "admin"}, tc.rolesErr
				},
			})
			h, err := b.RequiresRoles(tc.roles, WithValidationRule(tc.rule), WithPermissionParser(func(string) []string { t.Error("role parser invoked"); return nil }))
			if err != nil {
				t.Fatal(err)
			}
			status, body, count := serveAuthorization(h)
			if status != tc.status || body != "" {
				t.Fatal(status, body)
			}
			if (status == 204) != (count == 1) {
				t.Fatal(count)
			}
			wantCalls := 1
			if tc.subject == "" || tc.lookupErr != nil {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatal(calls)
			}
		})
	}
}

func TestRequiresRolesRejectsMissingRoleGetter(t *testing.T) {
	if h, err := NewBuilder().SetLookup(lookup).SetEnforcer(valueEnforcer{}).RequiresRoles([]string{"reader"}); h != nil || err == nil {
		t.Fatal("missing role capability accepted")
	}
}

func TestOptionsDefaultsAndIsolation(t *testing.T) {
	for _, constructor := range []func(...Option) *Options{NewOptions, Apply} {
		cfg := constructor(
			WithValidationRule(MatchAllRule),
			WithValidationRule(AtLeastOneRule),
			WithPermissionParser(PermissionParserWithSeparator("/")),
		)
		if cfg.validationRule != AtLeastOneRule || !reflect.DeepEqual(cfg.permissionParser("blog/create"), []string{"blog", "create"}) {
			t.Fatal("options were not applied in order")
		}
	}
	for _, factory := range []func() *Options{func() *Options { return NewOptions() }, DefaultOptions, func() *Options { return Apply() }} {
		defaults := factory()
		if defaults == factory() || defaults.validationRule != MatchAllRule || !reflect.DeepEqual(defaults.permissionParser("blog:create"), []string{"blog", "create"}) {
			t.Fatal("expected independent default options")
		}
		defaults.Apply(WithValidationRule(AtLeastOneRule))
		if factory().validationRule != MatchAllRule {
			t.Fatal("default options mutated")
		}
	}
	parser := PermissionParserWithSeparator(":")
	if got := parser("tenant:blog::create"); !reflect.DeepEqual(got, []string{"tenant", "blog", "", "create"}) {
		t.Fatal(got)
	}
}

func TestOptionsApplyPreservesExistingConfiguration(t *testing.T) {
	options := NewOptions(
		WithValidationRule(AtLeastOneRule),
		WithPermissionParser(PermissionParserWithSeparator("/")),
	)
	options.Apply(WithValidationRule(MatchAllRule))
	options.Apply()
	if options.validationRule != MatchAllRule || !reflect.DeepEqual(options.permissionParser("blog/create"), []string{"blog", "create"}) {
		t.Fatal("Apply reset or changed unrelated configuration")
	}
}

func TestOptionsValidateFinalConfiguration(t *testing.T) {
	b := NewBuilder().SetLookup(lookup).SetEnforcer(roleEnforcer{
		enforcerFunc: func(...any) (bool, error) { return true, nil },
		getRoles:     func(string, ...string) ([]string, error) { return nil, nil },
	})
	for _, tc := range []struct {
		name string
		opts []Option
		want string
	}{
		{name: "defaults"},
		{name: "any", opts: []Option{WithValidationRule(AtLeastOneRule)}},
		{name: "negative rule", opts: []Option{WithValidationRule(-1)}, want: "casbin middleware: invalid ValidationRule -1"},
		{name: "unknown rule", opts: []Option{WithValidationRule(2)}, want: "casbin middleware: invalid ValidationRule 2"},
		{name: "nil parser", opts: []Option{WithValidationRule(AtLeastOneRule), WithPermissionParser(nil)}, want: "casbin middleware: nil PermissionParser"},
		{name: "overridden rule", opts: []Option{WithValidationRule(-1), WithValidationRule(MatchAllRule)}},
		{name: "overridden parser", opts: []Option{WithPermissionParser(nil), WithPermissionParser(PermissionParserWithSeparator("/"))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := NewOptions(tc.opts...)
			rule, nilParser := options.validationRule, options.permissionParser == nil
			err := options.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != tc.want {
				t.Fatalf("error=%v want=%q", err, tc.want)
			}
			if options.validationRule != rule || (options.permissionParser == nil) != nilParser {
				t.Fatal("Validate modified configuration")
			}
			for _, build := range []func([]string, ...Option) (gin.HandlerFunc, error){b.RequiresPermissions, b.RequiresRoles} {
				handler, err := build([]string{"blog:create"}, tc.opts...)
				if tc.want == "" {
					if handler == nil || err != nil {
						t.Fatalf("valid configuration rejected: %v", err)
					}
				} else if handler != nil || err == nil || err.Error() != tc.want {
					t.Fatalf("handler returned error=%v want=%q", err, tc.want)
				}
			}
		})
	}
}

func TestPermissionAndRoleHandlerSnapshots(t *testing.T) {
	requests := []string{"blog/create"}
	roles := []string{"reader", "unknown"}
	b := NewBuilder().SetLookup(lookup).SetRequestValues(func(*gin.Context, string) ([]any, error) {
		t.Error("permission used route extraction")
		return nil, nil
	}).SetEnforcer(roleEnforcer{
		enforcerFunc: func(values ...any) (bool, error) {
			return reflect.DeepEqual(values, []any{"alice", "blog", "create"}), nil
		},
		getRoles: func(string, ...string) ([]string, error) { return []string{"reader"}, nil },
	})
	permission, err := b.RequiresPermissions(requests,
		WithPermissionParser(PermissionParserWithSeparator("/")),
	)
	if err != nil {
		t.Fatal(err)
	}
	role, err := b.RequiresRoles(roles,
		WithValidationRule(AtLeastOneRule),
	)
	if err != nil {
		t.Fatal(err)
	}
	requests[0] = "no/read"
	roles[0] = "unknown"
	b.SetLookup(func(*gin.Context) (string, error) { return "", nil })
	for _, h := range []gin.HandlerFunc{permission, role} {
		if status, _, count := serveAuthorization(h); status != 204 || count != 1 {
			t.Fatal(status, count)
		}
	}
}

func TestListResponsesAbortBeforeCallbacks(t *testing.T) {
	for _, mode := range []string{"permissions", "roles"} {
		for _, result := range []string{"unauthorized", "forbidden", "error", "skip"} {
			t.Run(mode+"/"+result, func(t *testing.T) {
				response := func(c *gin.Context) {
					if !c.IsAborted() {
						t.Error("callback before Abort")
					}
					c.Next()
					c.Status(418)
				}
				b := NewBuilder().SetLookup(func(*gin.Context) (string, error) {
					if result == "unauthorized" {
						return "", nil
					}
					return "alice", nil
				}).
					SetUnauthorized(response).SetForbidden(response).SetErrorHandler(func(c *gin.Context, err error) {
					if err == nil {
						t.Error("missing error")
					}
					response(c)
				}).
					SetSkip(func(*gin.Context) bool { return result == "skip" }).SetEnforcer(roleEnforcer{
					enforcerFunc: func(...any) (bool, error) {
						if result == "skip" {
							t.Error("unexpected enforce")
						}
						if result == "error" {
							return false, errors.New("error")
						}
						return false, nil
					},
					getRoles: func(string, ...string) ([]string, error) {
						if result == "skip" {
							t.Error("unexpected roles")
						}
						if result == "error" {
							return nil, errors.New("error")
						}
						return nil, nil
					},
				})
				build := b.RequiresPermissions
				if mode == "roles" {
					build = b.RequiresRoles
				}
				h, err := build([]string{"blog:read"})
				if err != nil {
					t.Fatal(err)
				}
				status, _, count := serveAuthorization(h)
				want := 418
				if result == "skip" {
					want = 204
				}
				if status != want || (status == 204) != (count == 1) {
					t.Fatal(status, count)
				}
			})
		}
	}
}

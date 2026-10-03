package casbin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

type enforcerFunc func(...any) (bool, error)

func (f enforcerFunc) Enforce(v ...any) (bool, error) { return f(v...) }

type pointerEnforcer struct{}

func (*pointerEnforcer) Enforce(...any) (bool, error) { return true, nil }

type valueEnforcer struct{}

func (valueEnforcer) Enforce(...any) (bool, error) { return true, nil }
func lookup(*gin.Context) (string, error)          { return "alice", nil }

func TestBuilderValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var ptr *pointerEnforcer
	var fn enforcerFunc
	for _, cfg := range []Builder{{}, {lookup: lookup}, {enforcer: ptr, lookup: lookup}, {enforcer: fn, lookup: lookup}, {enforcer: valueEnforcer{}}} {
		if h, err := cfg.Build(); h != nil || err == nil {
			t.Fatal("accepted invalid config")
		}
	}
	cfg := Builder{enforcer: valueEnforcer{}, lookup: lookup}
	if _, err := cfg.Build(); err != nil {
		t.Fatal(err)
	}
	if cfg.skip != nil || cfg.requestValues != nil || cfg.unauthorized != nil {
		t.Fatal("mutated config")
	}
}

func TestRequestBranches(t *testing.T) {
	sentinel := errors.New("SECRET internal error")
	for _, tc := range []struct {
		name                                 string
		subject                              string
		lookupErr                            error
		values                               []any
		valuesErr                            error
		customValues                         bool
		allow                                bool
		enforceErr                           error
		skip                                 bool
		status                               int
		lookupN, valuesN, enforceN, handlerN int
	}{
		{name: "allow", subject: "alice", allow: true, status: 200, lookupN: 1, valuesN: 1, enforceN: 1, handlerN: 1},
		{name: "missing", status: 401, lookupN: 1},
		{name: "lookup error", lookupErr: sentinel, status: 500, lookupN: 1},
		{name: "lookup error with subject", subject: "alice", lookupErr: sentinel, status: 500, lookupN: 1},
		{name: "forbidden", subject: "alice", status: 403, lookupN: 1, valuesN: 1, enforceN: 1},
		{name: "enforce error", subject: "alice", allow: true, enforceErr: sentinel, status: 500, lookupN: 1, valuesN: 1, enforceN: 1},
		{name: "values error", subject: "alice", customValues: true, valuesErr: sentinel, status: 500, lookupN: 1, valuesN: 1},
		{name: "empty values", subject: "alice", customValues: true, status: 500, lookupN: 1, valuesN: 1},
		{name: "custom values", subject: "alice", customValues: true, values: []any{"alice", "trusted-tenant", "report", "read"}, allow: true, status: 200, lookupN: 1, valuesN: 1, enforceN: 1, handlerN: 1},
		{name: "skip", skip: true, status: 200, handlerN: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, vn, en, hn := 0, 0, 0, 0
			cfg := Builder{
				lookup: func(*gin.Context) (string, error) { ln++; return tc.subject, tc.lookupErr },
				skip:   func(*gin.Context) bool { return tc.skip },
				requestValues: func(c *gin.Context, s string) ([]any, error) {
					vn++
					if tc.customValues {
						return tc.values, tc.valuesErr
					}
					return []any{s, c.Request.URL.Path, c.Request.Method}, nil
				},
				enforcer: enforcerFunc(func(values ...any) (bool, error) {
					en++
					want := []any{"alice", "/reports", "GET"}
					if tc.customValues {
						want = tc.values
					}
					if !reflect.DeepEqual(values, want) {
						t.Errorf("values=%v", values)
					}
					return tc.allow, tc.enforceErr
				}),
			}
			h, err := cfg.Build()
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.Use(h)
			r.GET("/reports", func(c *gin.Context) { hn++; c.Status(200) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/reports?page=2", nil))
			if w.Code != tc.status || ln != tc.lookupN || vn != tc.valuesN || en != tc.enforceN || hn != tc.handlerN {
				t.Fatalf("status=%d counts=%d/%d/%d/%d", w.Code, ln, vn, en, hn)
			}
			if w.Body.Len() != 0 {
				t.Fatal("default failure exposed body", w.Body.String())
			}
		})
	}
}

func TestDefaultValuesAndNoMethodBypass(t *testing.T) {
	for _, method := range []string{"GET", "OPTIONS"} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			h, err := (&Builder{lookup: lookup, enforcer: enforcerFunc(func(v ...any) (bool, error) {
				calls++
				if !reflect.DeepEqual(v, []any{"alice", "/reports/123", method}) {
					t.Error(v)
				}
				return true, nil
			})}).Build()
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.Use(h)
			r.Handle(method, "/reports/:id", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(method, "/reports/123?page=2", nil))
			if calls != 1 || w.Code != 204 {
				t.Fatal(calls, w.Code)
			}
		})
	}
}

func TestFailureCallbacksAlreadyAborted(t *testing.T) {
	sentinel := errors.New("internal")
	for _, kind := range []string{"unauthorized", "forbidden", "error"} {
		t.Run(kind, func(t *testing.T) {
			callbacks, downstream := 0, 0
			response := func(c *gin.Context) {
				callbacks++
				if !c.IsAborted() {
					t.Error("not aborted")
				}
				c.Next()
				c.String(418, "custom")
			}
			cfg := Builder{lookup: lookup, enforcer: valueEnforcer{}, unauthorized: response, forbidden: response, errorHandler: func(c *gin.Context, err error) {
				if !errors.Is(err, sentinel) {
					t.Error(err)
				}
				response(c)
			}}
			switch kind {
			case "unauthorized":
				cfg.lookup = func(*gin.Context) (string, error) { return "", nil }
			case "forbidden":
				cfg.enforcer = enforcerFunc(func(...any) (bool, error) { return false, nil })
			case "error":
				cfg.enforcer = enforcerFunc(func(...any) (bool, error) { return false, sentinel })
			}
			h, err := cfg.Build()
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.Use(h)
			r.GET("/", func(*gin.Context) { downstream++ })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if callbacks != 1 || downstream != 0 || w.Code != 418 || w.Body.String() != "custom" {
				t.Fatal(callbacks, downstream, w.Code, w.Body.String())
			}
		})
	}
}

func TestExactSkipAndEmptyFullPath(t *testing.T) {
	h, err := (&Builder{enforcer: valueEnforcer{}, lookup: lookup, skip: func(c *gin.Context) bool { return c.Request.URL.Path == "/healthz" }, requestValues: func(c *gin.Context, s string) ([]any, error) {
		if c.FullPath() == "" {
			return nil, errors.New("missing route template")
		}
		return []any{s, c.FullPath(), c.Request.Method}, nil
	}}).Build()
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(h)
	r.GET("/healthz", func(c *gin.Context) { c.Status(204) })
	for path, status := range map[string]int{"/healthz": 204, "/prefix/healthz": 500} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != status {
			t.Fatal(path, w.Code)
		}
	}
}

func TestBuilderHandlersKeepTheirSnapshot(t *testing.T) {
	b := NewBuilder().
		SetEnforcer(enforcerFunc(func(values ...any) (bool, error) {
			if !reflect.DeepEqual(values, []any{"alice", "trusted-resource"}) {
				t.Errorf("unexpected values: %v", values)
			}
			return true, nil
		})).
		SetLookup(lookup).
		SetSkip(func(*gin.Context) bool { return false }).
		SetRequestValues(func(_ *gin.Context, subject string) ([]any, error) { return []any{subject, "trusted-resource"}, nil })
	allowed, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	// Reusing a builder must not turn an existing handler into a bypass, change
	// its values, or replace its response callbacks.
	b.SetEnforcer(valueEnforcer{}).SetLookup(func(*gin.Context) (string, error) { return "", nil }).
		SetSkip(nil).SetRequestValues(nil).
		SetUnauthorized(func(c *gin.Context) { c.Status(418) }).
		SetForbidden(func(c *gin.Context) { c.Status(419) }).
		SetErrorHandler(func(c *gin.Context, _ error) { c.Status(420) })
	missing, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	b.SetLookup(lookup).SetEnforcer(enforcerFunc(func(...any) (bool, error) { return false, nil }))
	denied, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	b.SetEnforcer(enforcerFunc(func(...any) (bool, error) { return false, errors.New("failure") }))
	failed, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	b.SetSkip(func(*gin.Context) bool { return true })
	skipped, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	b.SetLookup(nil).SetSkip(nil).SetUnauthorized(nil).SetForbidden(nil).SetErrorHandler(nil)
	if _, err := b.Build(); err == nil {
		t.Fatal("invalid builder accepted")
	}
	for _, tc := range []struct {
		name    string
		handler gin.HandlerFunc
		status  int
	}{
		{"allowed", allowed, 204}, {"missing", missing, 418}, {"denied", denied, 419}, {"error", failed, 420}, {"skipped", skipped, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/", tc.handler, func(c *gin.Context) { c.Status(204) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d", w.Code, tc.status)
			}
		})
	}
}

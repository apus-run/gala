package luo

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMethodHelpersPreserveGroupMiddleware(t *testing.T) {
	cases := []struct {
		method   string
		register func(*Group, string, http.HandlerFunc)
	}{
		{http.MethodGet, (*Group).GET}, {http.MethodPost, (*Group).POST},
		{http.MethodPut, (*Group).PUT}, {http.MethodPatch, (*Group).PATCH},
		{http.MethodDelete, (*Group).DELETE}, {http.MethodHead, (*Group).HEAD},
		{http.MethodOptions, (*Group).OPTIONS}, {http.MethodConnect, (*Group).CONNECT},
		{http.MethodTrace, (*Group).TRACE}, {"PROPFIND", (*Group).ANY},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			root := New(http.NewServeMux())
			var events []string
			record := func(name string) func(http.Handler) http.Handler {
				return func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						events = append(events, name+":before")
						next.ServeHTTP(w, r)
						events = append(events, name+":after")
					})
				}
			}
			root.Use(record("root"))
			child := root.Mount("/api").With(record("child"))
			tc.register(child, "/items/{id}", func(w http.ResponseWriter, r *http.Request) {
				events = append(events, "handler")
				w.Header().Set("X-ID", r.PathValue("id"))
				w.WriteHeader(http.StatusNoContent)
			})
			w := httptest.NewRecorder()
			root.ServeHTTP(w, httptest.NewRequest(tc.method, "/api/items/42", nil))
			want := []string{"root:before", "child:before", "handler", "child:after", "root:after"}
			if w.Code != 204 || w.Header().Get("X-ID") != "42" || !reflect.DeepEqual(events, want) {
				t.Fatalf("code=%d headers=%v events=%v", w.Code, w.Header(), events)
			}
		})
	}
}

func TestMethodRoutingSemantics(t *testing.T) {
	root := New(http.NewServeMux())
	write := func(value string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("X-Route", value) }
	}
	root.ANY("/all", write("any"))
	root.GET("/all", write("get"))
	root.HEAD("/all", write("head"))
	root.GET("/get-only", write("get-only"))
	root.Method("PROPFIND", "/custom", write("custom"))
	root.Mount("/api").GET("/", write("root"))
	for _, tc := range []struct {
		method, path, route string
		code                int
	}{
		{"POST", "/all", "any", 200}, {"PROPFIND", "/all", "any", 200},
		{"GET", "/all", "get", 200}, {"HEAD", "/all", "head", 200},
		{"HEAD", "/get-only", "get-only", 200}, {"POST", "/get-only", "", 405},
		{"PROPFIND", "/custom", "custom", 200}, {"GET", "/custom", "", 405},
		{"GET", "/api/", "root", 200}, {"GET", "/api/missing", "", 404},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			root.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.code || w.Header().Get("X-Route") != tc.route {
				t.Fatalf("code=%d route=%q", w.Code, w.Header().Get("X-Route"))
			}
		})
	}
}

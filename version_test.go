package gala

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	info := Get()

	if info.Name != Name || info.Version != Version {
		t.Errorf("framework identity: got %q/%q, want %q/%q", info.Name, info.Version, Name, Version)
	}
	if info.GoVersion != runtime.Version() {
		t.Errorf("go version: got %q, want %q", info.GoVersion, runtime.Version())
	}
	if info.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Errorf("platform: got %q, want %q", info.Platform, runtime.GOOS+"/"+runtime.GOARCH)
	}
	// injected by ldflags, "unknown" without them
	if info.GitCommit == "" || info.BuildDate == "" {
		t.Errorf("build metadata must never be empty, got commit %q, date %q", info.GitCommit, info.BuildDate)
	}
}

func TestInfoJSONRoundTrip(t *testing.T) {
	info := Get()

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Info
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != *info {
		t.Errorf("round trip changed the info:\n got %+v\nwant %+v", got, *info)
	}
}

func TestInfoToJSON(t *testing.T) {
	info := Get()

	raw := info.ToJSON()
	if !json.Valid([]byte(raw)) {
		t.Fatalf("ToJSON() = %q, not valid JSON", raw)
	}

	var got Info
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != *info {
		t.Errorf("ToJSON round trip changed the info:\n got %+v\nwant %+v", got, *info)
	}
}

func TestInfoString(t *testing.T) {
	s := Get().String()

	for _, want := range []string{Name, Version, runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, missing %q", s, want)
		}
	}
}

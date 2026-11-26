package job

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestRunCLIInvocation(t *testing.T) {
	r := NewRegistry()
	var got Invocation
	if err := r.Register("report", func(ctx context.Context) error {
		got, _ = FromContext(ctx)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	args := []string{
		"--job", "report",
		"--job-data", `{"day":"today"}`,
		"--job-header", "X-A=1; X-B=two=equals",
	}
	if err := r.RunCLI(context.Background(), args, nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	if got.Name != "report" {
		t.Fatalf("name %q, want report", got.Name)
	}
	if string(got.Payload) != `{"day":"today"}` {
		t.Fatalf("payload %q", got.Payload)
	}
	if values := got.Header.Values("X-A"); !slices.Equal(values, []string{"1"}) {
		t.Fatalf("X-A %v, want [1]", values)
	}
	if values := got.Header.Values("X-B"); !slices.Equal(values, []string{"two=equals"}) {
		t.Fatalf("X-B %v, want [two=equals] (split on the first =)", values)
	}
}

func TestRunCLIHeaderParsing(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		want    map[string][]string
		wantErr bool
	}{
		{
			name: "single",
			flag: "X-A=1",
			want: map[string][]string{"X-A": {"1"}},
		},
		{
			name: "multiple and repeated keys",
			flag: " A = 1 ;B=2;A=3;;",
			want: map[string][]string{"A": {"1", "3"}, "B": {"2"}},
		},
		{
			name: "empty value",
			flag: "A=",
			want: map[string][]string{"A": {""}},
		},
		{
			name:    "missing equals",
			flag:    "A",
			wantErr: true,
		},
		{
			name:    "empty key",
			flag:    "=v",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := parseHeaderFlag(tt.flag)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseHeaderFlag(%q) = %v, want error", tt.flag, h)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseHeaderFlag(%q): %v", tt.flag, err)
			}
			for key, wantValues := range tt.want {
				if got := h.Values(key); !slices.Equal(got, wantValues) {
					t.Fatalf("%s: got %v, want %v", key, got, wantValues)
				}
			}
		})
	}
}

func TestRunCLIReadsStdin(t *testing.T) {
	r := NewRegistry()
	var got Invocation
	if err := r.Register("ingest", func(ctx context.Context) error {
		got, _ = FromContext(ctx)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := r.RunCLI(context.Background(), []string{"--job", "ingest", "--job-data", "-"}, strings.NewReader("from stdin")); err != nil {
		t.Fatalf("run: %v", err)
	}
	if string(got.Payload) != "from stdin" {
		t.Fatalf("payload %q, want %q", got.Payload, "from stdin")
	}
}

func TestRunCLIStdinNilMeansNoPayload(t *testing.T) {
	r := NewRegistry()
	var got Invocation
	if err := r.Register("ingest", func(ctx context.Context) error {
		got, _ = FromContext(ctx)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := r.RunCLI(context.Background(), []string{"--job", "ingest", "--job-data", "-"}, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Payload) != 0 {
		t.Fatalf("payload %q, want empty when stdin is nil", got.Payload)
	}
}

func TestRunCLIEmptyDataMeansNoPayload(t *testing.T) {
	r := NewRegistry()
	var got Invocation
	if err := r.Register("ingest", func(ctx context.Context) error {
		got, _ = FromContext(ctx)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := r.RunCLI(context.Background(), []string{"--job", "ingest"}, strings.NewReader("ignored")); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Payload) != 0 {
		t.Fatalf("payload %q, want empty", got.Payload)
	}
	if got.Header != nil {
		t.Fatalf("header %v, want nil without --job-header", got.Header)
	}
}

func TestRunCLIMissingJob(t *testing.T) {
	r := NewRegistry()

	err := r.RunCLI(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected an error without --job")
	}
	if !errors.Is(err, ErrEmptyName) {
		t.Fatalf("got %v, want ErrEmptyName", err)
	}
}

func TestRunCLIUnknownFlag(t *testing.T) {
	r := NewRegistry()

	if err := r.RunCLI(context.Background(), []string{"--nope"}, nil); err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
}

func TestRunCLIUnknownJob(t *testing.T) {
	r := NewRegistry()

	err := r.RunCLI(context.Background(), []string{"--job", "missing"}, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestRunCLIReturnsJobError(t *testing.T) {
	r := NewRegistry()
	jobErr := errors.New("job failed")
	if err := r.Register("fail", func(context.Context) error { return jobErr }); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := r.RunCLI(context.Background(), []string{"--job", "fail"}, nil); !errors.Is(err, jobErr) {
		t.Fatalf("got %v, want %v", err, jobErr)
	}
}

func TestRunCLIUnknownJobDoesNotReadStdin(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("known", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	reader := &trackingReader{}
	err := r.RunCLI(context.Background(), []string{"--job", "unknown", "--job-data", "-"}, reader)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("RunCLI() error = %v, want ErrNotFound", err)
	}
	if reader.reads != 0 {
		t.Fatalf("RunCLI() read stdin %d times for an unknown job, want 0", reader.reads)
	}
}

func TestRunCLIOversizedStdinIsRejected(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("known", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	big := io.LimitReader(repeatingReader{}, defaultMaxBodyBytes+1)
	err := r.RunCLI(context.Background(), []string{"--job", "known", "--job-data", "-"}, big)
	if err == nil || !strings.Contains(err.Error(), "stdin payload exceeds") {
		t.Fatalf("RunCLI() error = %v, want stdin payload size error", err)
	}
}

type trackingReader struct{ reads int }

func (r *trackingReader) Read([]byte) (int, error) {
	r.reads++
	return 0, io.EOF
}

type repeatingReader struct{}

func (repeatingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

package flagset_test

import (
	"flag"
	"strings"
	"testing"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/flagset"
)

func TestExplicitFlags(t *testing.T) {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.Int("Server.Port", 7, "")
	fs.String("Name", "default", "")
	fs.Bool("Debug", false, "")
	fs.Duration("Delay", 0, "")
	fs.Int("Other", 8, "")
	src := flagset.New(fs)
	if _, err := src.Load(t.Context()); err == nil {
		t.Fatal("unparsed flags")
	}
	if err := fs.Parse([]string{"-Server.Port=9", "-Name=", "-Debug", "-Delay=2ms"}); err != nil {
		t.Fatal(err)
	}
	type config struct {
		Server struct{ Port int }
		Name   string
		Debug  bool
		Delay  time.Duration
		Other  int
	}
	c, err := conf.Open(t.Context(), config{Name: "old", Other: 12}, src)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Current()
	if got.Server.Port != 9 || got.Name != "" || !got.Debug || got.Delay != 2*time.Millisecond || got.Other != 12 {
		t.Fatalf("%+v", got)
	}
}

type countedFlag int

func (f *countedFlag) String() string { (*f)++; return "SECRET" }
func (*countedFlag) Set(string) error { return nil }

func TestConflictsBeforeString(t *testing.T) {
	flags := flag.NewFlagSet("config", flag.ContinueOnError)
	var count countedFlag
	flags.Var(&count, "server", "")
	flags.Var(&count, "server.port", "")
	if err := flags.Parse([]string{"-server=x", "-server.port=y"}); err != nil {
		t.Fatal(err)
	}
	before := count
	_, err := flagset.New(flags).Load(t.Context())
	if err == nil || strings.Contains(err.Error(), "SECRET") || count != before {
		t.Fatal("unexpected value access", count, err)
	}
}

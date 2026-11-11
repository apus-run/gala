package conf_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing/fstest"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
)

func ExampleOpen() {
	type app struct {
		Port int `conf:"port"`
	}
	files := fstest.MapFS{"app.json": {Data: []byte(`{"port":9090}`)}}
	cfg, err := conf.Open(context.Background(), app{Port: 8080}, file.FS(files, "app.json", file.JSON))
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Current().Port)
	// Output: 9090
}

func ExampleConfig_Reload() {
	type app struct{ Port int }
	layer := map[string]any{"Port": 9090}
	src := conf.SourceFunc(func(context.Context) (map[string]any, error) { return layer, nil })
	ctx := context.Background()
	cfg, err := conf.Open(ctx, app{Port: 8080}, src)
	if err != nil {
		panic(err)
	}
	old := cfg.Current()
	delete(layer, "Port")
	if err := cfg.Reload(ctx); err != nil {
		panic(err)
	}
	fmt.Println(old.Port, cfg.Current().Port)
	// Output: 9090 8080
}

func ExampleSourceFunc() {
	src := file.FS(fstest.MapFS{}, "local.json", file.JSON)
	optional := conf.SourceFunc(func(ctx context.Context) (map[string]any, error) {
		layer, err := src.Load(ctx)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return layer, err
	})
	cfg, err := conf.Open(context.Background(), struct{ Port int }{Port: 8080}, optional)
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Current().Port)
	// Output: 8080
}

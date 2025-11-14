package file_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apus-run/gala/components/conf/file"
)

const (
	testJSON = `
{
    "server":{
        "http":{
            "addr":"0.0.0.0",
			"port":80,
            "timeout":0.5,
			"enable_ssl":true
        },
        "grpc":{
            "addr":"0.0.0.0",
			"port":10080,
            "timeout":0.2
        }
    },
    "data":{
        "database":{
            "driver":"mysql",
            "source":"root:root@tcp(127.0.0.1:3306)/test?parseTime=true"
        }
    },
	"endpoints":[
		"www.aaa.com",
		"www.bbb.org"
	],
    "foo":[
        {
            "name":"nihao",
            "age":18
        },
        {
            "name":"nihao",
            "age":18
        }
    ]
}`

	testJSONUpdate = `
{
    "server":{
        "http":{
            "addr":"0.0.0.0",
			"port":80,
            "timeout":0.5,
			"enable_ssl":true
        },
        "grpc":{
            "addr":"0.0.0.0",
			"port":10090,
            "timeout":0.2
        }
    },
    "data":{
        "database":{
            "driver":"mysql",
            "source":"root:root@tcp(127.0.0.1:3306)/test?parseTime=true"
        }
    },
	"endpoints":[
		"www.aaa.com",
		"www.bbb.org"
	],
    "foo":[
        {
            "name":"nihao",
            "age":18
        },
        {
            "name":"nihao",
            "age":18
        }
    ],
	"bar":{
		"event":"update"
	}
}`
)

type testConfigStruct struct {
	Server struct {
		HTTP struct {
			Addr      string  `json:"addr" yaml:"addr"`
			Port      int     `json:"port" yaml:"port"`
			Timeout   float64 `json:"timeout" yaml:"timeout"`
			EnableSSL bool    `json:"enable_ssl" yaml:"enableSSL"`
		} `json:"http" yaml:"http"`
		GRPC struct {
			Addr    string  `json:"addr" yaml:"addr"`
			Port    int     `json:"port" yaml:"port"`
			Timeout float64 `json:"timeout" yaml:"timeout"`
		} `json:"grpc" yaml:"grpc"`
	} `json:"server"`
	Data struct {
		Database struct {
			Driver string `json:"driver" yaml:"driver"`
			Source string `json:"source" yaml:"source"`
		} `json:"database" yaml:"database" yaml:"database"`
	} `json:"data" yaml:"data"`
	Endpoints []string `json:"endpoints" yaml:"endpoints"`
}

func TestFile(t *testing.T) {
	var (
		path = filepath.Join(os.TempDir(), "test_config")
		file = filepath.Join(path, "test.json")
		data = []byte(testJSON)
	)
	defer os.Remove(path)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Error(err)
	}
	if err := os.WriteFile(file, data, 0o666); err != nil {
		t.Error(err)
	}

	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Error(err)
	}

	testSource(t, file)
	testSource(t, path)
}

func testSource(t *testing.T, path string) {
	t.Logf("path: %s", path)
	s := file.NewSource(path)
	kvs, err := s.Load()
	if err != nil {
		t.Error(err)
	}
	for _, f := range kvs {
		t.Logf("文件名 Key: %s, Format: %s, Data: %s", f.Key, f.Format, f.Value)
	}
}

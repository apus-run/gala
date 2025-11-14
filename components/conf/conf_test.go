package conf_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/apus-run/gala/components/conf"
	"github.com/apus-run/gala/components/conf/file"
)

func TestLoad(t *testing.T) {
	// 在初始化模块的时候再读配置信息
	type DB struct {
		DSN string `yaml:"dsn"`
	}
	type Config struct {
		DB
	}
	var cfg Config
	var db DB

	t.Run("using yaml config", func(t *testing.T) {
		c := conf.New([]file.Source{
			file.NewSource("internal/testdata/dev.yaml"),
		})
		err := c.Load()

		assert.NoError(t, err)

		assert.NotNil(t, c)

		err = c.File("dev").UnmarshalKey("db", &db)
		if err != nil {
			t.Fatalf("unmarshal key error: %v", err)
		}
		assert.NoError(t, err)

		t.Logf("db: %+v", db)

		err = c.Scan("dev", &cfg)
		if err != nil {
			t.Fatalf("scan error: %v", err)
		}
		assert.NoError(t, err)

		t.Logf("cfg: %v", cfg)

		cf := c.File("dev")
		err = cf.Unmarshal(&cfg)
		if err != nil {
			t.Errorf("error: %v", err)
		}
		t.Logf("cfg: %v", cfg)
	})

	t.Run("test nil file handling", func(t *testing.T) {
		c := conf.New([]file.Source{})
		err := c.Load()
		assert.NoError(t, err)

		// Test Get with non-existent file
		value := c.Get("nonexistent", "key")
		assert.Nil(t, value)

		// Test Scan with non-existent file
		var cfg Config
		err = c.Scan("nonexistent", &cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config file not found")
	})
}

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
			EnableSSL bool    `json:"enable_ssl" yaml:"enable_ssl"`
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

func TestConfig(t *testing.T) {
	var (
		path  = filepath.Join(os.TempDir(), "test_config")
		a     = filepath.Join(path, "test.json")
		b     = filepath.Join(path, "config.json")
		data  = []byte(testJSON)
		data2 = []byte(testJSONUpdate)
	)
	defer os.Remove(path)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Error(err)
	}
	if err := os.WriteFile(a, data, 0o666); err != nil {
		t.Error(err)
	}

	if err := os.WriteFile(b, data2, 0o666); err != nil {
		t.Error(err)
	}

	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Error(err)
	}

	c := conf.New([]file.Source{file.NewSource(path)})

	testConfig(t, c)
}

func testConfig(t *testing.T, c conf.Conf) {
	var (
		httpAddr       = "0.0.0.0"
		httpTimeout    = 0.5
		grpcPort       = 10080
		endpoint1      = "www.aaa.com"
		databaseDriver = "mysql"
	)

	c.Load()
	c.Watch(func() {
		t.Log("Watch")
	})

	v := c.File("test").Get("server")
	t.Logf("app: %v", v)
	config := c.File("config").Get("server")
	t.Logf("app: %v", config)
	driver := c.File("config").GetString("data.database.driver")
	t.Logf("data.database.driver: %s", driver)

	if databaseDriver != driver {
		t.Fatal("databaseDriver is not equal to val")
	}

	var testConf testConfigStruct
	appConf := c.File("test")
	err := appConf.Unmarshal(&testConf)
	if err != nil {
		t.Errorf("error: %d", err)
	}
	t.Logf("AppConfig: %v", testConf)

	if httpAddr != testConf.Server.HTTP.Addr {
		t.Errorf("testConf.Server.HTTP.Addr want: %s, got: %s", httpAddr, testConf.Server.HTTP.Addr)
	}
	if httpTimeout != testConf.Server.HTTP.Timeout {
		t.Errorf("testConf.Server.HTTP.Timeout want: %.1f, got: %.1f", httpTimeout, testConf.Server.HTTP.Timeout)
	}
	if testConf.Server.HTTP.EnableSSL {
		t.Error("testConf.Server.HTTP.EnableSSL is not equal to true")
	}
	if grpcPort != testConf.Server.GRPC.Port {
		t.Errorf("testConf.Server.GRPC.Port want: %d, got: %d", grpcPort, testConf.Server.GRPC.Port)
	}
	if endpoint1 != testConf.Endpoints[0] {
		t.Errorf("testConf.Endpoints[0] want: %s, got: %s", endpoint1, testConf.Endpoints[0])
	}
	if len(testConf.Endpoints) != 2 {
		t.Error("len(testConf.Endpoints) is not equal to 2")
	}
}

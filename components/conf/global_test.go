package conf

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/components/conf/file"
)

func TestConfigLoading(t *testing.T) {
	originalPath := os.Getenv("CONFIG_PATH")
	defer func() {
		if originalPath != "" {
			os.Setenv("CONFIG_PATH", originalPath)
		} else {
			os.Unsetenv("CONFIG_PATH")
		}
	}()

	t.Run("环境变量", func(t *testing.T) {
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test.yaml")
		err := os.WriteFile(testFile, []byte(`app: test`), 0644)
		require.NoError(t, err)

		os.Setenv("CONFIG_PATH", testFile)
		err = loadConfig([]file.Source{file.NewSource(testFile)})
		assert.NoError(t, err)
		assert.NotNil(t, global)
	})

	t.Run("config目录", func(t *testing.T) {
		tempDir := t.TempDir()
		configDir := filepath.Join(tempDir, "config")
		err := os.MkdirAll(configDir, 0755)
		require.NoError(t, err)

		testFile := filepath.Join(configDir, "app.yaml")
		err = os.WriteFile(testFile, []byte(`app: test`), 0644)
		require.NoError(t, err)

		err = loadConfig([]file.Source{file.NewSource(testFile)})
		assert.NoError(t, err)
		assert.NotNil(t, global)
	})

	t.Run("空的sources", func(t *testing.T) {
		err := loadConfig([]file.Source{})
		assert.NoError(t, err)
	})
}

func TestGlobalFunctions(t *testing.T) {
	// 创建测试配置
	v := viper.New()
	v.Set("test_key", "test_value")
	v.Set("number", 42)
	v.Set("nested.key", "nested_value")
	v.Set("deep.a.b.c", "deep_value")
	v.SetConfigType("yaml")

	// 创建mock配置
	mockConf := &mockConf{files: map[string]*viper.Viper{"config": v}}

	// 设置全局配置
	original := global.GetConfig()
	global.SetConfig(mockConf)

	defer func() {
		global.SetConfig(original)
	}()

	t.Run("Get-基本键值", func(t *testing.T) {
		assert.Equal(t, "test_value", Get("test_key"))
		assert.Equal(t, 42, Get("number"))
	})

	t.Run("Get-嵌套键", func(t *testing.T) {
		assert.Equal(t, "nested_value", Get("nested.key"))
		assert.Equal(t, "deep_value", Get("deep.a.b.c"))
	})

	t.Run("Get-不存在的键", func(t *testing.T) {
		assert.Nil(t, Get("not_exist"))
	})

	t.Run("Set-设置新键", func(t *testing.T) {
		Set("new_key", "new_value")
		assert.Equal(t, "new_value", Get("new_key"))
	})

	t.Run("Set-更新现有键", func(t *testing.T) {
		Set("test_key", "updated_value")
		assert.Equal(t, "updated_value", Get("test_key"))
	})

	t.Run("Set-设置嵌套键", func(t *testing.T) {
		Set("nested.new_key", "nested_new_value")
		assert.Equal(t, "nested_new_value", Get("nested.new_key"))
	})
}

func TestUnmarshalKey(t *testing.T) {
	// 创建测试配置
	v := viper.New()
	v.Set("database.driver", "mysql")
	v.Set("database.host", "localhost")
	v.Set("database.port", 3306)
	v.SetConfigType("yaml")

	mockConf := &mockConf{files: map[string]*viper.Viper{"config": v}}
	original := global.GetConfig()
	global.SetConfig(mockConf)

	defer func() {
		global.SetConfig(original)
	}()

	t.Run("UnmarshalKey-空key", func(t *testing.T) {
		type Config struct {
			Database struct {
				Driver string
				Host   string
				Port   int
			}
		}

		var result Config
		err := UnmarshalKey("", &result)
		assert.NoError(t, err)
	})

	t.Run("UnmarshalKey-特定key", func(t *testing.T) {
		type DB struct {
			Driver string
			Host   string
			Port   int
		}
		v.Set("db", map[string]interface{}{
			"driver": "postgres",
			"host":   "localhost",
			"port":   5432,
		})

		var result DB
		err := UnmarshalKey("db", &result)
		assert.NoError(t, err)
	})

	t.Run("UnmarshalKey-错误情况", func(t *testing.T) {
		// 测试当配置为nil时的错误处理
		global.SetConfig(nil)
		var result map[string]any
		err := UnmarshalKey("", &result)
		assert.Error(t, err)
		assert.Equal(t, ErrConfigFileNotFound, err)
	})
}

func TestScan(t *testing.T) {
	// 创建测试配置
	v := viper.New()
	v.Set("app", "myapp")
	v.Set("version", "1.0.0")
	v.SetConfigType("yaml")

	mockConf := &mockConf{files: map[string]*viper.Viper{"config": v}}
	original := global.GetConfig()
	global.SetConfig(mockConf)

	defer func() {
		global.SetConfig(original)
	}()

	t.Run("Scan-成功", func(t *testing.T) {
		type AppConfig struct {
			App     string
			Version string
		}

		var result AppConfig
		err := Scan(&result)
		assert.NoError(t, err)
	})

	t.Run("Scan-配置为nil", func(t *testing.T) {
		global.SetConfig(nil)
		var result map[string]any
		err := Scan(&result)
		assert.Error(t, err)
		assert.Equal(t, ErrConfigFileNotFound, err)
	})
}

func TestLoad(t *testing.T) {
	// 创建测试配置
	v := viper.New()
	v.SetConfigType("yaml")

	mockConf := &mockConf{files: map[string]*viper.Viper{"config": v}}
	original := global.GetConfig()
	global.SetConfig(mockConf)

	defer func() {
		global.SetConfig(original)
	}()

	t.Run("Load-成功", func(t *testing.T) {
		err := Load()
		assert.NoError(t, err)
	})

	t.Run("Load-配置为nil", func(t *testing.T) {
		global.SetConfig(nil)
		err := Load()
		assert.Error(t, err)
		assert.Equal(t, ErrConfigNotInitialized, err)
	})
}

func TestWatch(t *testing.T) {
	// 创建测试配置
	v := viper.New()
	v.SetConfigType("yaml")

	callCount := atomic.Int32{}
	mockConf := &mockConf{
		files: map[string]*viper.Viper{"config": v},
		watchFunc: func() {
			callCount.Add(1)
		},
	}

	original := global.GetConfig()
	global.SetConfig(mockConf)

	defer func() {
		global.SetConfig(original)
	}()

	t.Run("Watch-设置回调", func(t *testing.T) {
		Watch(func() {
			callCount.Add(1)
		})

		// 触发watch回调
		mockConf.triggerWatch()
		assert.GreaterOrEqual(t, callCount.Load(), int32(1))
	})

	t.Run("Watch-配置为nil", func(t *testing.T) {
		global.SetConfig(nil)
		// 不应该panic
		Watch(func() {
			t.Log("callback executed")
		})
	})
}

func TestConfigAppliance(t *testing.T) {
	t.Run("SetConfig和GetConfig", func(t *testing.T) {
		original := global.GetConfig()

		mockConf := &mockConf{files: map[string]*viper.Viper{}}
		global.SetConfig(mockConf)

		conf := global.GetConfig()
		assert.Equal(t, mockConf, conf)

		// 恢复
		global.SetConfig(original)
	})
}

func TestInitializeConfig(t *testing.T) {
	// 保存原始状态
	originalGlobal := global.GetConfig()
	defer global.SetConfig(originalGlobal)

	t.Run("从环境变量CONFIG_PATH加载", func(t *testing.T) {
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test.yaml")
		err := os.WriteFile(testFile, []byte(`app: test`), 0644)
		require.NoError(t, err)

		// 重置 global 配置
		global.SetConfig(nil)

		err = loadConfig([]file.Source{file.NewSource(testFile)})
		assert.NoError(t, err)
		assert.NotNil(t, global.GetConfig())
	})

	t.Run("测试loadConfig with 空sources", func(t *testing.T) {
		global.SetConfig(nil)
		err := loadConfig([]file.Source{})
		assert.NoError(t, err)
	})

	t.Run("测试configAppliance的SetConfig和GetConfig", func(t *testing.T) {
		testConf := &mockConf{files: map[string]*viper.Viper{}}
		global.SetConfig(testConf)
		assert.Equal(t, testConf, global.GetConfig())
	})
}

// mockConf 是一个测试用的配置mock
type mockConf struct {
	files     map[string]*viper.Viper
	watchFunc func()
}

func (m *mockConf) File(filename string) *viper.Viper {
	return m.files[filename]
}

func (m *mockConf) Scan(filename string, obj any) error {
	if v := m.File(filename); v != nil {
		return v.Unmarshal(obj)
	}
	return assert.AnError
}

func (m *mockConf) Get(filename string, key string) any {
	if v := m.File(filename); v != nil {
		return v.Get(key)
	}
	return nil
}

func (m *mockConf) Set(filename string, key string, val any) {
	if v := m.File(filename); v != nil {
		v.Set(key, val)
	}
}

func (m *mockConf) Load() error {
	return nil
}

func (m *mockConf) Watch(fn func()) {
	m.watchFunc = fn
}

// triggerWatch 用于测试时手动触发watch
func (m *mockConf) triggerWatch() {
	if m.watchFunc != nil {
		m.watchFunc()
	}
}

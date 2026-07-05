package bumblebee_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newViper() *viper.Viper {
	return viper.New()
}

func writeTempConfig(t *testing.T, filename, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeTempConfig: %v", err)
	}
	return path
}

func TestDefaults(t *testing.T) {
	v := newViper()
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.host", "localhost")
	v.SetDefault("debug", false)
	v.SetDefault("timeout", 30*time.Second)

	if got := v.GetInt("server.port"); got != 8080 {
		t.Errorf("port: want 8080, got %d", got)
	}
	if got := v.GetString("server.host"); got != "localhost" {
		t.Errorf("host: want localhost, got %s", got)
	}
	if got := v.GetBool("debug"); got != false {
		t.Errorf("debug: want false, got %v", got)
	}
	if got := v.GetDuration("timeout"); got != 30*time.Second {
		t.Errorf("timeout: want 30s, got %v", got)
	}
}

func TestSetGet(t *testing.T) {
	v := newViper()
	v.Set("app.name", "viper tester")
	v.Set("app.version", "0.1.0")
	v.Set("workers", 4)

	assert.Equal(t, v.Get("app.name"), "viper tester")
}

func TestReadYaml(t *testing.T) {
	const yamlConfig = `
server:
  host: "0.0.0.0"
  port: 9090
database:
  dsn: "postgres://user:pass@localhost/mydb"
  max_conns: 20
feature_flags:
  - "dark_mode"
  - "beta_api"
`
	path := writeTempConfig(t, "config.yaml", yamlConfig)

	v := newViper()
	v.SetConfigFile(path)

	require.NoError(t, v.ReadInConfig())

	assert.Equal(t, v.GetString("server.host"), "0.0.0.0")
	assert.Equal(t, v.GetInt("server.port"), 9090)

	flags := v.GetStringSlice("feature_flags")
	assert.Equal(t, len(flags), 2)
	assert.Equal(t, flags[0], "dark_mode")
}

func TestReadJSON(t *testing.T) {
	const jsonConfig = `{
  "service": {
    "name": "alert-pipeline",
    "replicas": 3
  },
  "log_level": "info"
}`
	path := writeTempConfig(t, "config.json", jsonConfig)

	v := newViper()
	v.SetConfigFile(path)
	require.NoError(t, v.ReadInConfig())
	assert.Equal(t, v.GetString("service.name"), "alert-pipeline")
	assert.Equal(t, v.GetInt("service.replicas"), 3)
}

func TestReadTOML(t *testing.T) {
	const tomlConfig = `
[aws]
region = "ap-northeast-2"
account_id = "123456789012"

[eks]
cluster_name = "prod-cluster"
node_count = 5
`
	path := writeTempConfig(t, "config.toml", tomlConfig)

	v := newViper()
	v.SetConfigFile(path)

	require.NoError(t, v.ReadInConfig())
	assert.Equal(t, v.GetString("aws.region"), "ap-northeast-2")
	assert.Equal(t, v.GetInt("aws.account_id"), 123456789012)
}

func TestEnvVarsFlat(t *testing.T) {
	v := newViper()
	v.SetEnvPrefix("MYAPP")
	v.AutomaticEnv()

	t.Setenv("MYAPP_LOG_LEVEL", "debug")
	t.Setenv("MYAPP_WORKERS", "8")

	assert.Equal(t, v.GetString("log_level"), "debug")
	assert.Equal(t, v.GetString("workers"), "8")
}

func TestBindEnv(t *testing.T) {
	v := newViper()

	t.Setenv("AWS_REGION", "ap-northeast-2")
	require.NoError(t, v.BindEnv("aws_region", "AWS_REGION"))

	assert.Equal(t, v.GetString("aws_region"), "ap-northeast-2")

	t.Setenv("DB_HOST", "db.internal")
	require.NoError(t, v.BindEnv("database.host", "DB_HOST"))

	assert.Equal(t, v.GetString("database.host"), "db.internal")
}

func TestPriority(t *testing.T) {
	path := writeTempConfig(t, "config.yaml", "port: 7070\n")

	v := newViper()
	v.SetDefault("port", 3000)
	v.SetConfigFile(path)
	_ = v.ReadInConfig()

	t.Setenv("PRIO_PORT", "8888")
	require.NoError(t, v.BindEnv("port", "PRIO_PORT"))

	assert.Equal(t, v.GetInt("port"), 8888)

	v.Set("port", 9999)
	assert.Equal(t, v.GetInt("port"), 9999)
}

type AppConfig struct {
	Server struct {
		Host string `mapstructure:"host"`
		Port int    `mapstructure:"port"`
	} `mapstructure:"server"`

	Debug   bool   `mapstructure:"debug"`
	AppName string `mapstructure:"app_name"`
}

func TestUnmarshal(t *testing.T) {
	const cfg = `
server:
  host: "api.example.com"
  port: 443
debug: true
app_name: "copilot"
`

	path := writeTempConfig(t, "config.yaml", cfg)

	v := newViper()
	v.SetConfigFile(path)
	require.NoError(t, v.ReadInConfig())

	var out AppConfig
	require.NoError(t, v.Unmarshal(&out))

	assert.Equal(t, out.Server.Host, "api.example.com")
	assert.Equal(t, out.Server.Port, 443)
	assert.Equal(t, out.Debug, true)
}

func TestIsSetAndAllKeys(t *testing.T) {
	v := newViper()
	v.SetDefault("timeout", 10)
	v.Set("log_level", "warn")

	assert.True(t, v.IsSet("timeout"))
	assert.True(t, v.IsSet("log_level"))
	assert.False(t, v.IsSet("nonexistent_key"))

	keys := v.AllKeys()
	found := slices.Contains(keys, "log_level")

	assert.True(t, found)
}

func TestMergeInConfig(t *testing.T) {
	base := writeTempConfig(t, "base.yaml", "host: base-host\nport: 8080\n")
	override := writeTempConfig(t, "base.yaml", "port: 9090\nextra: added\n")

	v := newViper()
	v.SetConfigFile(base)
	require.NoError(t, v.ReadInConfig())

	v.SetConfigFile(override)
	require.NoError(t, v.MergeInConfig())

	assert.Equal(t, v.GetString("host"), "base-host")
	assert.Equal(t, v.GetInt("port"), 9090)
	assert.Equal(t, v.GetString("extra"), "added")
}

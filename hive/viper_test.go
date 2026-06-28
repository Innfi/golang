package bumblebee_test

import (
	"os"
	"path/filepath"
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

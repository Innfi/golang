package bumblebee_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
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

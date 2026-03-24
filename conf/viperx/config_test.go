package viperx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bang-go/micro/conf/envx"
	"github.com/spf13/viper"
)

func TestOpenLoadsEnvironmentSpecificConfig(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "application.dev.yaml", "server:\n  port: 8081\n")
	writeConfigFile(t, dir, "application.yaml", "server:\n  port: 8080\n")

	cfg, err := Open(&Config{
		Name:  "application",
		Type:  "yaml",
		Paths: []string{dir},
		Mode:  envx.Development,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if got, want := cfg.GetInt("server.port"), 8081; got != want {
		t.Fatalf("server.port = %d, want %d", got, want)
	}
}

func TestOpenFallsBackToBaseConfig(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "application.yaml", "server:\n  port: 8080\n")

	cfg, err := Open(&Config{
		Name:  "application",
		Type:  "yaml",
		Paths: []string{dir},
		Mode:  envx.Production,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if got, want := cfg.GetInt("server.port"), 8080; got != want {
		t.Fatalf("server.port = %d, want %d", got, want)
	}
}

func TestOpenAllowsEnvironmentOverrides(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "application.yaml", "server:\n  port: 8080\n")
	t.Setenv("SERVER_PORT", "9090")

	cfg, err := Open(&Config{
		Name:  "application",
		Type:  "yaml",
		Paths: []string{dir},
		Mode:  envx.Development,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if got, want := cfg.GetInt("server.port"), 9090; got != want {
		t.Fatalf("server.port = %d, want %d", got, want)
	}
}

func TestOpenLoadsExplicitFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "service.yaml")
	writeConfigFile(t, dir, "service.yaml", "server:\n  name: api\n")

	cfg, err := Open(&Config{File: file})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if got, want := cfg.GetString("server.name"), "api"; got != want {
		t.Fatalf("server.name = %q, want %q", got, want)
	}
}

func TestOpenCanBeEnvOnly(t *testing.T) {
	t.Setenv("APP_NAME", "micro")

	cfg, err := Open(&Config{
		Name:  "application",
		Type:  "yaml",
		Paths: []string{t.TempDir()},
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if got, want := cfg.GetString("app.name"), "micro"; got != want {
		t.Fatalf("app.name = %q, want %q", got, want)
	}
}

func TestOpenRequireFile(t *testing.T) {
	_, err := Open(&Config{
		Name:        "application",
		Type:        "yaml",
		Paths:       []string{t.TempDir()},
		RequireFile: true,
	})
	if err == nil {
		t.Fatal("Open() error = nil, want config file not found")
	}

	var notFound viper.ConfigFileNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Open() error = %T, want ConfigFileNotFoundError", err)
	}
}

func TestOpenUsesEnvPrefix(t *testing.T) {
	t.Setenv("MICRO_SERVER_PORT", "7070")

	cfg, err := Open(&Config{
		Name:      "application",
		Type:      "yaml",
		Paths:     []string{t.TempDir()},
		EnvPrefix: "MICRO",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if got, want := cfg.GetInt("server.port"), 7070; got != want {
		t.Fatalf("server.port = %d, want %d", got, want)
	}
}

func TestOpenNormalizesModeAndPaths(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "application.dev.yaml", "server:\n  port: 8082\n")

	cfg, err := Open(&Config{
		Name:  " application ",
		Type:  " yaml ",
		Paths: []string{"   ", dir, dir},
		Mode:  envx.Mode(" DEVELOPMENT "),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if got, want := cfg.GetInt("server.port"), 8082; got != want {
		t.Fatalf("server.port = %d, want %d", got, want)
	}
}

func TestOpenUsesActiveModeWhenConfigModeBlank(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "application.test.yaml", "server:\n  port: 18080\n")
	t.Setenv(envx.AppEnvKey, "test")

	cfg, err := Open(&Config{
		Name:  "application",
		Type:  "yaml",
		Paths: []string{dir},
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if got, want := cfg.GetInt("server.port"), 18080; got != want {
		t.Fatalf("server.port = %d, want %d", got, want)
	}
}

func TestOpenDoesNotMutateInputConfig(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "application.dev.yaml", "server:\n  port: 8083\n")

	conf := &Config{
		Name:      " application ",
		Type:      " yaml ",
		Paths:     []string{" ", dir, dir},
		Mode:      envx.Mode(" DEVELOPMENT "),
		EnvPrefix: " MICRO ",
	}

	_, err := Open(conf)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if got, want := conf.Name, " application "; got != want {
		t.Fatalf("conf.Name = %q, want %q", got, want)
	}
	if got, want := conf.Type, " yaml "; got != want {
		t.Fatalf("conf.Type = %q, want %q", got, want)
	}
	if got, want := conf.EnvPrefix, " MICRO "; got != want {
		t.Fatalf("conf.EnvPrefix = %q, want %q", got, want)
	}
	if got, want := conf.Mode, envx.Mode(" DEVELOPMENT "); got != want {
		t.Fatalf("conf.Mode = %q, want %q", got, want)
	}
	if got, want := conf.Paths, []string{" ", dir, dir}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("conf.Paths = %v, want %v", got, want)
	}
}

func writeConfigFile(t *testing.T, dir, name, content string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

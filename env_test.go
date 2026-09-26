package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := "" +
		"# comment\n" +
		"\n" +
		"TRACKANYTHING_TEST_HOST=127.0.0.1\n" +
		"export TRACKANYTHING_TEST_PORT=1025 # mailhog\n" +
		"TRACKANYTHING_TEST_FROM=\"Track Anything <hello@trackanything.io>\"\n" +
		"TRACKANYTHING_TEST_QUOTED='keep # this'\n" +
		"TRACKANYTHING_TEST_ALREADY=from-file\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		"TRACKANYTHING_TEST_HOST",
		"TRACKANYTHING_TEST_PORT",
		"TRACKANYTHING_TEST_FROM",
		"TRACKANYTHING_TEST_QUOTED",
	} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	t.Setenv("TRACKANYTHING_TEST_ALREADY", "from-env")

	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TRACKANYTHING_TEST_HOST"); got != "127.0.0.1" {
		t.Errorf("HOST = %q", got)
	}
	if got := os.Getenv("TRACKANYTHING_TEST_PORT"); got != "1025" {
		t.Errorf("PORT = %q", got)
	}
	if got := os.Getenv("TRACKANYTHING_TEST_FROM"); got != "Track Anything <hello@trackanything.io>" {
		t.Errorf("FROM = %q", got)
	}
	if got := os.Getenv("TRACKANYTHING_TEST_QUOTED"); got != "keep # this" {
		t.Errorf("QUOTED = %q", got)
	}
	if got := os.Getenv("TRACKANYTHING_TEST_ALREADY"); got != "from-env" {
		t.Errorf("existing variable overwritten: %q", got)
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	if err := loadDotEnv(filepath.Join(t.TempDir(), ".env")); err != nil {
		t.Fatal(err)
	}
}

func TestApplyDotEnvRejectsBadLines(t *testing.T) {
	err := applyDotEnv(strings.NewReader("not a pair\n"))
	if err == nil {
		t.Fatal("expected an error")
	}
}

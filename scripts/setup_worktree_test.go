package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runSetup(t *testing.T, main, worktree string) string {
	t.Helper()
	script := filepath.Join(repoRoot(t), ".cursor", "setup-worktree-unix.sh")
	destDir := filepath.Join(worktree, ".cursor")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(destDir, "setup-worktree-unix.sh")
	if err := os.WriteFile(copied, body, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", copied)
	cmd.Dir = worktree
	cmd.Env = append(os.Environ(),
		"ROOT_WORKTREE_PATH="+main,
		"SETUP_WORKTREE_SKIP_MOD=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	return string(out)
}

func TestSetupCopiesSafeEnvOnce(t *testing.T) {
	main := t.TempDir()
	worktree := t.TempDir()
	safe := "SMTP_HOST=\"127.0.0.1\"\nSMTP_PORT=1025 # mailhog\nENV=dev\nBASE_URL=http://localhost:8080\n"
	writeFile(t, filepath.Join(main, ".env"), safe, 0o644)
	writeFile(t, filepath.Join(main, ".env.example"), "SMTP_HOST=127.0.0.1\n", 0o644)

	first := runSetup(t, main, worktree)
	if !strings.Contains(first, "copied .env from the main checkout") {
		t.Fatalf("first run = %q", first)
	}
	got, err := os.ReadFile(filepath.Join(worktree, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != safe {
		t.Fatalf("copied .env = %q", got)
	}

	writeFile(t, filepath.Join(worktree, ".env"), "KEEP=1\n", 0o644)
	second := runSetup(t, main, worktree)
	if !strings.Contains(second, "leaving existing .env in place") {
		t.Fatalf("second run = %q", second)
	}
	got, err = os.ReadFile(filepath.Join(worktree, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "KEEP=1\n" {
		t.Fatalf("existing .env = %q", got)
	}
}

func TestSetupRefusesUnsafeEnv(t *testing.T) {
	main := t.TempDir()
	worktree := t.TempDir()
	const secret = "super-secret-value"
	const turnstile = "ts-live-secret"
	unsafe := "" +
		"ENV=prod\n" +
		"SMTP_PASS=" + secret + "\n" +
		"TURNSTILE_SECRET_KEY=" + turnstile + "\n" +
		"DB_PATH=/data/trackanything.db\n" +
		"BASE_URL=https://trackanything.io\n" +
		"SMTP_HOST=127.0.0.1\n"
	example := "SMTP_HOST=127.0.0.1\nSMTP_PORT=1025\n"
	writeFile(t, filepath.Join(main, ".env"), unsafe, 0o644)
	writeFile(t, filepath.Join(main, ".env.example"), example, 0o644)

	out := runSetup(t, main, worktree)
	if strings.Contains(out, secret) || strings.Contains(out, turnstile) || strings.Contains(out, "trackanything.io") {
		t.Fatalf("setup printed a secret or production location\n%s", out)
	}
	for _, name := range []string{"ENV", "SMTP_PASS", "TURNSTILE_SECRET_KEY", "DB_PATH", "BASE_URL"} {
		if !strings.Contains(out, name) {
			t.Fatalf("output did not name %s\n%s", name, out)
		}
	}
	if !strings.Contains(out, "copied .env.example to .env") {
		t.Fatalf("output = %q", out)
	}
	got, err := os.ReadFile(filepath.Join(worktree, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != example {
		t.Fatalf("worktree .env = %q", got)
	}
}

func TestSetupUsesExampleWhenEnvIsMissing(t *testing.T) {
	main := t.TempDir()
	worktree := t.TempDir()
	example := "# local\nSMTP_HOST=127.0.0.1\nSMTP_PORT=1025\n"
	writeFile(t, filepath.Join(main, ".env.example"), example, 0o644)

	out := runSetup(t, main, worktree)
	if !strings.Contains(out, "main checkout has no .env") || !strings.Contains(out, "copied .env.example") {
		t.Fatalf("output = %q", out)
	}
	got, err := os.ReadFile(filepath.Join(worktree, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != example {
		t.Fatalf("worktree .env = %q", got)
	}

	again := runSetup(t, main, worktree)
	if !strings.Contains(again, "leaving existing .env in place") {
		t.Fatalf("second run = %q", again)
	}
}

func TestSetupSkipsSymlinkAndMalformedEnv(t *testing.T) {
	main := t.TempDir()
	worktree := t.TempDir()
	const secret = "symlink-secret-value"
	target := filepath.Join(main, "real.env")
	writeFile(t, target, "SMTP_PASS="+secret+"\n", 0o644)
	if err := os.Symlink(target, filepath.Join(main, ".env")); err != nil {
		t.Fatal(err)
	}
	example := "SMTP_HOST=127.0.0.1\n"
	writeFile(t, filepath.Join(main, ".env.example"), example, 0o644)

	out := runSetup(t, main, worktree)
	if strings.Contains(out, secret) {
		t.Fatalf("setup printed a secret\n%s", out)
	}
	if !strings.Contains(out, "symlink") || !strings.Contains(out, "copied .env.example") {
		t.Fatalf("output = %q", out)
	}
	got, err := os.ReadFile(filepath.Join(worktree, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != example {
		t.Fatalf("worktree .env = %q", got)
	}

	worktree = t.TempDir()
	if err := os.Remove(filepath.Join(main, ".env")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(main, ".env"), "this is not valid\nSMTP_PASS="+secret+"\n", 0o644)
	out = runSetup(t, main, worktree)
	if strings.Contains(out, secret) {
		t.Fatalf("setup printed a secret\n%s", out)
	}
	if !strings.Contains(out, "not KEY=VALUE") || !strings.Contains(out, "copied .env.example") {
		t.Fatalf("output = %q", out)
	}
}

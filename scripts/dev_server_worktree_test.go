package scripts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func claimField(t *testing.T, claim, port, key string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(claim, "port-"+port, key))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(body))
}

func plantClaim(t *testing.T, claim, port, id, pid string) {
	t.Helper()
	dir := filepath.Join(claim, "port-"+port)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"instance_id": id,
		"worktree":    "/tmp/planted-worktree",
		"name":        "planted",
		"pid":         pid,
		"lstart":      "not-the-live-start",
		"command":     "not-the-live-command",
		"process_cwd": "/tmp/planted-cwd",
	}
	for name, body := range files {
		writeFile(t, filepath.Join(dir, name), body+"\n", 0o644)
	}
}

func TestSimultaneousWorktreesClaimDistinctPorts(t *testing.T) {
	root := t.TempDir()
	claim := filepath.Join(root, "claims")
	const n = 6
	envs := make([][]string, n)
	agents := make([]*agent, n)
	states := make([]string, n)
	server := httpServer(t, root)
	for i := 0; i < n; i++ {
		states[i] = filepath.Join(root, fmt.Sprintf("state%d", i))
		extra := baseExtra(t, states[i], 39000, 39039, server)
		extra["DEV_SERVER_PORT_CLAIM_DIR"] = claim
		extra["DEV_SERVER_ENV_OUT"] = filepath.Join(root, fmt.Sprintf("env-%d.txt", i))
		envs[i] = testEnv(t, extra)
		agents[i] = startAgent(t, envs[i], "alpha", false)
	}
	for _, a := range agents {
		a.ready(t)
	}

	seen := map[string]bool{}
	ports := make([]string, n)
	repo := repoRoot(t)
	personalDB := filepath.Join(repo, "data", "trackanything.db")
	for i := 0; i < n; i++ {
		ports[i] = instanceField(t, states[i], "alpha", "port")
		nport, err := strconv.Atoi(ports[i])
		if err != nil || nport < 39000 || nport > 39039 {
			t.Fatalf("port %s out of range", ports[i])
		}
		if seen[ports[i]] {
			t.Fatalf("duplicate port %s", ports[i])
		}
		seen[ports[i]] = true
		if ports[i] == "8080" {
			t.Fatal("agent claimed the personal server port")
		}
		got, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("env-%d.txt", i)))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(got)), "\n")
		wantDB := filepath.Join(states[i], "alpha", "data", "trackanything.db")
		if len(lines) != 3 || lines[1] != wantDB {
			t.Fatalf("DB_PATH = %q, want %s", lines, wantDB)
		}
		if lines[1] == personalDB || strings.HasPrefix(lines[1], filepath.Join(repo, "data")+string(os.PathSeparator)) {
			t.Fatalf("DB_PATH uses the personal database: %s", lines[1])
		}
		if claimField(t, claim, ports[i], "instance_id") == "" {
			t.Fatal("missing claim instance id")
		}
		if claimField(t, claim, ports[i], "name") != "alpha" {
			t.Fatalf("claim name = %s", claimField(t, claim, ports[i], "name"))
		}
		if claimField(t, claim, ports[i], "worktree") != repo {
			t.Fatalf("claim worktree = %s, want %s", claimField(t, claim, ports[i], "worktree"), repo)
		}
		if claimField(t, claim, ports[i], "pid") != instanceField(t, states[i], "alpha", "supervisor_pid") {
			t.Fatal("claim pid does not match the supervisor")
		}
		if _, err := os.Stat(filepath.Join(claim, "port-8080")); !os.IsNotExist(err) {
			t.Fatal("personal port was claimed")
		}
	}

	survivor := claimField(t, claim, ports[1], "instance_id")
	if _, err := runScript(t, envs[0], "stop", "alpha"); err != nil {
		t.Fatal(err)
	}
	agents[0].waitExit(t)
	if _, err := os.Stat(filepath.Join(claim, "port-"+ports[0])); !os.IsNotExist(err) {
		t.Fatalf("stopped server left its claim: %v", err)
	}
	if got := claimField(t, claim, ports[1], "instance_id"); got != survivor {
		t.Fatalf("stopping alpha released the other claim: %s then %s", survivor, got)
	}
	dialOK(t, "127.0.0.1:"+ports[1])
}

func TestStopDoesNotReleaseAnotherServersClaim(t *testing.T) {
	root := t.TempDir()
	claim := filepath.Join(root, "claims")
	server := httpServer(t, root)
	stateA := filepath.Join(root, "a")
	stateB := filepath.Join(root, "b")
	extraA := baseExtra(t, stateA, 39040, 39049, server)
	extraB := baseExtra(t, stateB, 39040, 39049, server)
	extraA["DEV_SERVER_PORT_CLAIM_DIR"] = claim
	extraB["DEV_SERVER_PORT_CLAIM_DIR"] = claim
	envA := testEnv(t, extraA)
	envB := testEnv(t, extraB)
	a := startAgent(t, envA, "alpha", false)
	b := startAgent(t, envB, "alpha", false)
	a.ready(t)
	b.ready(t)
	portA := instanceField(t, stateA, "alpha", "port")
	portB := instanceField(t, stateB, "alpha", "port")
	if portA == portB {
		t.Fatalf("both claimed %s", portA)
	}
	idB := claimField(t, claim, portB, "instance_id")

	inst := filepath.Join(stateA, "alpha", "instance.env")
	body, err := os.ReadFile(inst)
	if err != nil {
		t.Fatal(err)
	}
	replaced := false
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "claim_port=") {
			lines[i] = "claim_port=" + portB
			replaced = true
		}
	}
	if !replaced {
		t.Fatalf("instance has no claim_port\n%s", body)
	}
	writeFile(t, inst, strings.Join(lines, "\n"), 0o644)

	if _, err := runScript(t, envA, "stop", "alpha"); err != nil {
		t.Fatal(err)
	}
	a.waitExit(t)
	if got := claimField(t, claim, portB, "instance_id"); got != idB {
		t.Fatalf("stop rewrote the other claim id from %s to %s", idB, got)
	}
	dialOK(t, "127.0.0.1:"+portB)
	if _, err := os.Stat(filepath.Join(claim, "port-"+portB, "instance_id")); err != nil {
		t.Fatal(err)
	}
}

func TestAmbiguousClaimIsNotTakenOrDeleted(t *testing.T) {
	root := t.TempDir()
	claim := filepath.Join(root, "claims")
	sleepCmd := exec.Command("sleep", "30")
	if err := sleepCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sleepCmd.Process.Kill()
		_, _ = sleepCmd.Process.Wait()
	})
	plantClaim(t, claim, "39060", "planted-id", strconv.Itoa(sleepCmd.Process.Pid))

	state := filepath.Join(root, "state")
	extra := baseExtra(t, state, 39060, 39069, httpServer(t, root))
	extra["DEV_SERVER_PORT_CLAIM_DIR"] = claim
	env := testEnv(t, extra)
	a := startAgent(t, env, "alpha", false)
	a.ready(t)
	port := instanceField(t, state, "alpha", "port")
	if port == "39060" {
		t.Fatal("agent took the ambiguous claim's port")
	}
	if got := claimField(t, claim, "39060", "instance_id"); got != "planted-id" {
		t.Fatalf("ambiguous claim id = %s", got)
	}
	if _, err := runScript(t, env, "stop", "alpha"); err != nil {
		t.Fatal(err)
	}
	a.waitExit(t)
	if got := claimField(t, claim, "39060", "instance_id"); got != "planted-id" {
		t.Fatalf("stop removed the ambiguous claim: %s", got)
	}
	if !pidAlive(sleepCmd.Process.Pid) {
		t.Fatal("ambiguous claim owner was killed")
	}
}

func TestStaleClaimIsReclaimed(t *testing.T) {
	root := t.TempDir()
	claim := filepath.Join(root, "claims")
	sleepCmd := exec.Command("sleep", "30")
	if err := sleepCmd.Start(); err != nil {
		t.Fatal(err)
	}
	dead := sleepCmd.Process.Pid
	_ = sleepCmd.Process.Kill()
	_, _ = sleepCmd.Process.Wait()
	if pidAlive(dead) {
		t.Fatal("sleep pid is still alive")
	}
	plantClaim(t, claim, "39070", "stale-id", strconv.Itoa(dead))

	state := filepath.Join(root, "state")
	extra := baseExtra(t, state, 39070, 39079, httpServer(t, root))
	extra["DEV_SERVER_PORT_CLAIM_DIR"] = claim
	env := testEnv(t, extra)
	a := startAgent(t, env, "alpha", false)
	a.ready(t)
	if got := instanceField(t, state, "alpha", "port"); got != "39070" {
		t.Fatalf("port = %s, want the reclaimed port 39070", got)
	}
	if got := claimField(t, claim, "39070", "instance_id"); got == "stale-id" || got == "" {
		t.Fatalf("claim id = %q", got)
	}
	if claimField(t, claim, "39070", "pid") != strconv.Itoa(a.cmd.Process.Pid) {
		t.Fatal("reclaimed claim is not owned by the new server")
	}
}

func TestPersonalDevTargetUnchanged(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "dev:\n\tgo run github.com/air-verse/air@v1.67.4\n") {
		t.Fatal("make dev recipe changed")
	}
	script, err := os.ReadFile(filepath.Join(root, "scripts", "dev-server.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	if !strings.Contains(text, "PORT_MIN:-8100") || !strings.Contains(text, "READY_TIMEOUT:-60") {
		t.Fatal("dev-server defaults changed")
	}
	if !strings.Contains(text, "data/trackanything.db") {
		t.Fatal("personal database is no longer excluded from agent servers")
	}
}

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourcedEnvironmentReplacesBothDataDirectoryVariables(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "account.env")
	data := filepath.Join(root, "new data's directory")
	if err := os.WriteFile(path, environmentFile(Account{Email: "fixture@example.invalid", UserDataDir: data}, filepath.Join(root, "auth.json")), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", `. "$1"; printf '%s\n%s' "$USER_DATA_DIR" "$CODEX_USER_DATA_DIR"`, "sh", path)
	cmd.Env = []string{"PATH=/bin:/usr/bin", "CODEX_USER_DATA_DIR=/old-data", "USER_DATA_DIR=/another-data"}
	output, err := cmd.Output()
	if err != nil || string(output) != data+"\n"+data {
		t.Fatalf("sourced environment retained an old directory: %q, %v", output, err)
	}
}

func TestLaunchEnvironmentRemovesCredentialOverrides(t *testing.T) {
	env := codexLaunchEnvironment([]string{"PATH=/bin", "CODEX_HOME=/old", "CODEX_API_KEY=", "CODEX_ACCESS_TOKEN=old", "CODEX_REFRESH_TOKEN=old", "OPENAI_API_KEY=old", "CODEX_AUTH_JSON=old"}, "/original")
	got := strings.Join(env, "\n")
	if got != "PATH=/bin\nCODEX_HOME=/original" {
		t.Fatal("unexpected environment keys")
	}
}

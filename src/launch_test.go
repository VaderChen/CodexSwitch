package main

import (
	"strings"
	"testing"
)

func TestLaunchEnvironmentRemovesCredentialOverrides(t *testing.T) {
	env := codexLaunchEnvironment([]string{"PATH=/bin", "CODEX_HOME=/old", "CODEX_API_KEY=", "CODEX_ACCESS_TOKEN=old", "CODEX_REFRESH_TOKEN=old", "OPENAI_API_KEY=old", "CODEX_AUTH_JSON=old"}, "/original")
	got := strings.Join(env, "\n")
	if got != "PATH=/bin\nCODEX_HOME=/original" {
		t.Fatal("unexpected environment keys")
	}
}

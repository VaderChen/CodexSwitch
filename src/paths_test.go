package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemPathIdentity(t *testing.T) {
	roots := []string{t.TempDir()}
	if volume := os.Getenv("CODEX_SWITCH_CASE_TEST_ROOT"); volume != "" {
		root, err := os.MkdirTemp(volume, "paths-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(root) })
		roots = append(roots, root)
	}
	for _, root := range roots {
		t.Run(filepath.Base(root), func(t *testing.T) {
			actual, alias := filepath.Join(root, "Existing"), filepath.Join(root, "alias")
			if err := os.Mkdir(actual, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(actual, alias); err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(filepath.Join(root, "existing"))
			caseInsensitive := statErr == nil
			if statErr != nil && !os.IsNotExist(statErr) {
				t.Fatal(statErr)
			}
			sensitive, err := filesystemCaseSensitive(root)
			if err != nil || sensitive == caseInsensitive {
				t.Fatalf("filesystem rule mismatch: %v %v", sensitive, err)
			}
			t.Logf("case-sensitive filesystem: %v", sensitive)
			for _, c := range []struct {
				a, b string
				want bool
			}{
				{actual, alias, true},
				{actual, filepath.Join(root, "existing"), caseInsensitive},
				{filepath.Join(actual, "new", "file"), filepath.Join(alias, "new", "file"), true},
				{filepath.Join(actual, "new", "file"), filepath.Join(alias, "NEW", "FILE"), caseInsensitive},
				{filepath.Join(root, "one", "file"), filepath.Join(root, "two", "file"), false},
				{actual, filepath.Join(root, "missing"), false},
			} {
				got, err := sameFilesystemPath(c.a, c.b)
				if err != nil || got != c.want {
					t.Fatalf("%q / %q: %v %v; want %v", c.a, c.b, got, err, c.want)
				}
			}
			// Compare predictions for missing names with the volume's actual lookup.
			// Normalization and case sensitivity are independent filesystem rules.
			for _, pair := range [][2]string{
				{"caf\u00e9", "cafe\u0301"},
				{"caf\u00e9", "CAFE\u0301"},
				{"\u00c5ngstrom", "\u212bngstrom"},
				{"\u212aelvin", "Kelvin"},
				{"ffi", "\ufb03"},
				{"Stra\u00dfe", "STRASSE"},
				{"circle-1", "circle-\u2460"},
				{"plain", "pl\u00e4in"},
			} {
				a, b := filepath.Join(root, pair[0]), filepath.Join(root, pair[1])
				got, err := sameFilesystemPath(a, b)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(a, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				ai, err := os.Stat(a)
				if err != nil {
					t.Fatal(err)
				}
				bi, err := os.Stat(b)
				if err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				want := err == nil && os.SameFile(ai, bi)
				if err := os.Remove(a); err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Fatalf("missing Unicode names %q / %q: predicted %v; actual filesystem %v", pair[0], pair[1], got, want)
				}
			}
			if _, err := sameFilesystemPath("relative", actual); err == nil {
				t.Fatal("relative path accepted")
			}
			cycle := filepath.Join(root, "cycle")
			if err := os.Symlink(cycle, cycle); err != nil {
				t.Fatal(err)
			}
			if _, err := sameFilesystemPath(cycle, actual); err == nil {
				t.Fatal("symlink loop accepted")
			}
		})
	}
}

func TestTransactionRejectsAliasesBeforeCreatingFiles(t *testing.T) {
	root := t.TempDir()
	actual, alias := filepath.Join(root, "actual"), filepath.Join(root, "alias")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	for _, paths := range [][]string{
		{filepath.Join(actual, "new", "file"), filepath.Join(alias, "new", "file")},
		{filepath.Join(actual, "file"), filepath.Join(actual, "file.bak")},
		{filepath.Join(actual, "caf\u00e9"), filepath.Join(actual, "cafe\u0301")},
		{filepath.Join(actual, "caf\u00e9", "file"), filepath.Join(actual, "cafe\u0301", "file")},
		{filepath.Join(actual, "caf\u00e9"), filepath.Join(actual, "cafe\u0301.bak")},
	} {
		if err := replaceFiles([]fileUpdate{{paths[0], []byte("first")}, {paths[1], []byte("second")}}); err == nil {
			t.Fatal("aliased output accepted")
		}
	}
	entries, err := os.ReadDir(actual)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected transaction created files")
	}
	file, hardlink := filepath.Join(root, "file"), filepath.Join(root, "hardlink")
	if err := os.WriteFile(file, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(file, hardlink); err != nil {
		t.Fatal(err)
	}
	if err := validateFileTargets([]string{file, hardlink}); err == nil {
		t.Fatal("hardlink alias accepted")
	}
}

func TestCurrentAccountUsesDirectoryIdentity(t *testing.T) {
	root := t.TempDir()
	home, data := filepath.Join(root, "Home"), filepath.Join(root, "Data")
	for _, p := range []string{home, data} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "home")); os.IsNotExist(err) {
		t.Skip("requires case-insensitive volume")
	}
	a := Account{Email: "fixture@example.invalid", CodexHome: home, UserDataDir: data}
	if !accountIsRunning(a, []Account{{Email: a.Email, CodexHome: filepath.Join(root, "home"), UserDataDir: filepath.Join(root, "data")}}) {
		t.Fatal("same account not marked current")
	}
	if _, err := selectCodexInstances([]codexInstance{{Home: filepath.Join(root, "other"), Data: data, Started: 1}}, home, filepath.Join(root, "data")); err == nil {
		t.Fatal("shared data directory was not rejected")
	}
}

func TestMissingUnicodePathProbeFailsClosedAndCleansUp(t *testing.T) {
	root := t.TempDir()
	_, err := sameFilesystemPath(filepath.Join(root, "caf\u00e9"), filepath.Join(root, "cafe\u0301"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("Unicode comparison left temporary files")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0700) })
	if _, err := sameFilesystemPath(filepath.Join(root, "caf\u00e9"), filepath.Join(root, "cafe\u0301")); err == nil {
		t.Fatal("unverifiable Unicode path identity was accepted")
	}
}

func TestSymlinkParentTraversalMatchesSettingsAndProcesses(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "actual", "deep")
	if err := os.MkdirAll(deep, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "link")
	if err := os.Symlink(deep, alias); err != nil {
		t.Fatal(err)
	}
	if got, err := normalizeDirectory(alias); err != nil || got != alias {
		t.Fatalf("unambiguous symlink spelling was changed: %q %v", got, err)
	}
	t.Setenv("HOME", root)
	for _, name := range []string{"existing", "missing", "~"} {
		t.Run(name, func(t *testing.T) {
			if name == "existing" {
				if err := os.Mkdir(filepath.Join(root, "actual", name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			expectedParent, err := filepath.EvalSymlinks(filepath.Join(root, "actual"))
			if err != nil {
				t.Fatal(err)
			}
			expected := filepath.Join(expectedParent, name)
			raw := alias + "/../" + name
			canonical, err := canonicalProcessPath(raw)
			if err != nil || canonical != expected {
				t.Fatalf("process path %q: %q %v; want %q", raw, canonical, err, expected)
			}
			for _, setting := range []string{raw, "~/link/../" + name} {
				got, err := normalizeDirectory(setting)
				if err != nil || got != expected {
					t.Fatalf("setting %q: %q %v; want %q", setting, got, err, expected)
				}
			}
			home, data, err := processDirectories([]string{"Codex"}, map[string]string{"CODEX_HOME": raw, "USER_DATA_DIR": raw})
			if err != nil || home != expected || data != expected {
				t.Fatalf("process directories %q / %q: %v", home, data, err)
			}
			got, err := selectCodexInstances([]codexInstance{{Home: home, Data: data, Started: 1}}, expected, expected)
			if err != nil || len(got) != 1 {
				t.Fatalf("shared auth process skipped: %v %v", got, err)
			}
		})
	}
	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(root, "absent"), dangling); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{dangling, dangling + "/child", filepath.Join(root, "missing") + "/../existing"} {
		if _, err := canonicalProcessPath(raw); err == nil {
			t.Fatalf("ambiguous missing path accepted: %q", raw)
		}
		if _, err := normalizeDirectory(raw); err == nil {
			t.Fatalf("ambiguous setting accepted: %q", raw)
		}
	}
	t.Setenv("HOME", alias+"/..")
	home, data, err := accountDirectories(Account{})
	if err != nil {
		t.Fatal(err)
	}
	processHome, processData, err := processDirectories([]string{"Codex"}, map[string]string{"HOME": os.Getenv("HOME")})
	if err != nil || processHome != home || processData != data {
		t.Fatalf("HOME traversal differs for settings and process: %q / %q, %q / %q, %v", home, data, processHome, processData, err)
	}
	expectedParent, err := filepath.EvalSymlinks(filepath.Join(root, "actual"))
	if err != nil || home != filepath.Join(expectedParent, ".codex") || data != filepath.Join(expectedParent, "Library", "Application Support", "Codex") {
		t.Fatalf("HOME parent traversal selected incorrect defaults: %q / %q, %v", home, data, err)
	}
}

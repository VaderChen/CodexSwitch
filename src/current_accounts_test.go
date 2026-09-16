package main

import "testing"

func TestMultipleCurrentAccounts(t *testing.T) {
	running := []Account{
		{Email: "a@example.com", CodexHome: "/profiles/a", UserDataDir: "/data/a"},
		{Email: "b@example.com", CodexHome: "/profiles/b", UserDataDir: "/data/b"},
	}
	for _, a := range running {
		if !accountIsRunning(a, running) {
			t.Fatal("兩個獨立實例都應顯示使用中")
		}
	}
	for _, a := range []Account{
		{Email: "a@example.com", CodexHome: "/profiles/b", UserDataDir: "/data/b"},
		{Email: "a@example.com", CodexHome: "/profiles/a", UserDataDir: "/data/other"},
		{Email: "c@example.com", CodexHome: "/profiles/a", UserDataDir: "/data/a"},
	} {
		if accountIsRunning(a, running) {
			t.Fatal("帳號與目錄不符，不應顯示使用中")
		}
	}
	if accountIsRunning(running[0], nil) {
		t.Fatal("沒有執行中的實例時不應保留舊勾選")
	}
}

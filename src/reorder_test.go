package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReorderPersistsAndRejectsInvalidLists(t *testing.T) {
	s := &accountStore{path: filepath.Join(t.TempDir(), "accounts.json"), accounts: []Account{{ID: "a", Email: "a@example.com"}, {ID: "b", Email: "b@example.com"}, {ID: "c", Email: "c@example.com"}}}
	if err := s.reorder([]string{"c", "a", "b"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	var saved []Account
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved[0].ID != "c" || saved[1].ID != "a" || saved[2].ID != "b" {
		t.Fatal("排序未儲存")
	}
	for _, ids := range [][]string{{"a", "a", "b"}, {"a", "b"}, {"a", "b", "x"}} {
		if err = s.reorder(ids); err == nil {
			t.Fatal("應拒絕無效排序")
		}
		if s.list()[0].ID != "c" {
			t.Fatal("無效排序改動原列表")
		}
	}
	s.path = filepath.Join(t.TempDir(), "missing", "accounts.json")
	if err = s.reorder([]string{"a", "b", "c"}); err == nil {
		t.Fatal("應回報儲存錯誤")
	}
	if s.list()[0].ID != "c" {
		t.Fatal("儲存失敗未還原")
	}
}

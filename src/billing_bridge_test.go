package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type fakeBillingBackend struct {
	queryFn func(context.Context, Account, string) (billingHistory, error)
	urlFn   func(string, string) (string, error)
}

func (f fakeBillingBackend) query(ctx context.Context, a Account, cursor string) (billingHistory, error) {
	return f.queryFn(ctx, a, cursor)
}
func (f fakeBillingBackend) invoiceURL(accountID, ticket string) (string, error) {
	return f.urlFn(accountID, ticket)
}

type billingNotification struct {
	id     string
	result any
}

func billingBridgeFixture(t *testing.T, backend fakeBillingBackend) (*billingBridge, <-chan billingNotification) {
	t.Helper()
	notifications := make(chan billingNotification, 8)
	store := &accountStore{path: filepath.Join(t.TempDir(), "accounts.json"), accounts: []Account{{ID: "one", Email: "one@example.invalid"}}}
	b := newBillingBridge(store, backend, func(id string, result any) { notifications <- billingNotification{id, result} })
	t.Cleanup(b.close)
	return b, notifications
}

func receiveBillingNotification(t *testing.T, ch <-chan billingNotification) billingNotification {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("billing completion was not delivered")
		return billingNotification{}
	}
}

func TestBillingBridgeQueryIsAsynchronous(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	b, notifications := billingBridgeFixture(t, fakeBillingBackend{queryFn: func(ctx context.Context, a Account, cursor string) (billingHistory, error) {
		if a.ID != "one" || a.Email != "one@example.invalid" || cursor != "next-page" {
			t.Error("wrong account or cursor")
		}
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return billingHistory{}, nil
	}})
	if response := b.query("one", "next-page", "request-one"); response["pending"] != true {
		t.Fatal(response)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("query did not start")
	}
	if response := b.query("one", "", "request-one"); response["error"] == nil {
		t.Fatal("duplicate request accepted")
	}
	close(release)
	result := receiveBillingNotification(t, notifications)
	if result.id != "request-one" {
		t.Fatal("wrong completion id")
	}
	if _, ok := result.result.(billingHistory); !ok {
		t.Fatal("query result was not delivered")
	}
}

func TestBillingBridgeCancelledRequestCannotReplaceReusedID(t *testing.T) {
	var count atomic.Int32
	started, release, oldFinished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	b, notifications := billingBridgeFixture(t, fakeBillingBackend{queryFn: func(ctx context.Context, a Account, cursor string) (billingHistory, error) {
		if count.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				<-release
			}
			close(oldFinished)
			return billingHistory{}, errors.New("old response")
		}
		return billingHistory{}, nil
	}})
	if response := b.query("one", "", "reused"); response["pending"] != true {
		t.Fatal(response)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("query did not start")
	}
	b.cancelRequest("reused")
	if response := b.query("one", "", "reused"); response["pending"] != true {
		t.Fatal(response)
	}
	result := receiveBillingNotification(t, notifications)
	if _, ok := result.result.(billingHistory); !ok {
		t.Fatal("new result missing")
	}
	close(release)
	<-oldFinished
	select {
	case result := <-notifications:
		t.Fatalf("cancelled request delivered: %v", result)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestBillingBridgeBoundsRequestsAndCancelsOnClose(t *testing.T) {
	cancelled := make(chan struct{}, 4)
	b, notifications := billingBridgeFixture(t, fakeBillingBackend{queryFn: func(ctx context.Context, _ Account, _ string) (billingHistory, error) {
		<-ctx.Done()
		cancelled <- struct{}{}
		return billingHistory{}, ctx.Err()
	}})
	for _, id := range []string{"a", "b", "c", "d"} {
		if response := b.query("one", "", id); response["pending"] != true {
			t.Fatal(response)
		}
	}
	if response := b.query("one", "", "overflow"); response["error"] == nil {
		t.Fatal("unbounded query accepted")
	}
	b.close()
	for range 4 {
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatal("shutdown did not cancel query")
		}
	}
	if response := b.query("one", "", "after-close"); response["error"] == nil {
		t.Fatal("closed bridge accepted query")
	}
	select {
	case <-notifications:
		t.Fatal("shutdown delivered a stale result")
	default:
	}
}

func TestBillingBridgeOnlyOpensIssuedInvoiceForExistingAccount(t *testing.T) {
	var opens atomic.Int32
	b, notifications := billingBridgeFixture(t, fakeBillingBackend{urlFn: func(accountID, ticket string) (string, error) {
		if accountID != "one" || ticket != "issued-ticket" {
			return "", errors.New("請重新查詢帳單")
		}
		return "https://invoice.stripe.com/i/fixture", nil
	}})
	b.open = func(_ context.Context, target string) error {
		if target != "https://invoice.stripe.com/i/fixture" {
			t.Error("unexpected browser target")
		}
		opens.Add(1)
		return nil
	}
	if response := b.openInvoice("missing", "issued-ticket", "missing"); response["error"] == nil {
		t.Fatal("unknown account accepted")
	}
	if response := b.openInvoice("one", "forged", "forged"); response["pending"] != true {
		t.Fatal(response)
	}
	if receiveBillingNotification(t, notifications).result.(map[string]any)["error"] == nil || opens.Load() != 0 {
		t.Fatal("unissued invoice opened")
	}
	if response := b.openInvoice("one", "issued-ticket", "valid"); response["pending"] != true {
		t.Fatal(response)
	}
	if receiveBillingNotification(t, notifications).result.(map[string]any)["opened"] != true || opens.Load() != 1 {
		t.Fatal("issued invoice did not open")
	}
	if err := b.store.remove("one"); err != nil {
		t.Fatal(err)
	}
	if response := b.openInvoice("one", "issued-ticket", "removed"); response["error"] == nil {
		t.Fatal("deleted account retained download access")
	}
	if opens.Load() != 1 {
		t.Fatal("removed account opened browser")
	}
}

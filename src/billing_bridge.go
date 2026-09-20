package main

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

type billingBackend interface {
	query(context.Context, Account, string) (billingHistory, error)
	invoiceURL(string, string) (string, error)
}

type billingRequest struct {
	cancel context.CancelFunc
}

// Keep network and browser launch work outside the WebView callback thread.
type billingBridge struct {
	mu      sync.Mutex
	closed  bool
	pending map[string]*billingRequest
	ctx     context.Context
	cancel  context.CancelFunc
	store   *accountStore
	backend billingBackend
	notify  func(string, any)
	open    func(context.Context, string) error
}

func newBillingBridge(store *accountStore, backend billingBackend, notify func(string, any)) *billingBridge {
	ctx, cancel := context.WithCancel(context.Background())
	return &billingBridge{
		pending: make(map[string]*billingRequest), ctx: ctx, cancel: cancel,
		store: store, backend: backend, notify: notify,
		open: func(ctx context.Context, target string) error {
			if err := exec.CommandContext(ctx, "/usr/bin/open", target).Run(); err != nil {
				return errors.New("無法開啟帳單頁面，請稍後再試")
			}
			return nil
		},
	}
}

func (b *billingBridge) query(accountID, cursor, requestID string) map[string]any {
	return b.start(accountID, requestID, func(ctx context.Context, account Account) (any, error) {
		return b.backend.query(ctx, account, cursor)
	})
}

func (b *billingBridge) openInvoice(accountID, downloadID, requestID string) map[string]any {
	return b.start(accountID, requestID, func(ctx context.Context, account Account) (any, error) {
		target, err := b.backend.invoiceURL(account.ID, downloadID)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := b.open(ctx, target); err != nil {
			return nil, err
		}
		return map[string]any{"opened": true}, nil
	})
}

func (b *billingBridge) start(accountID, requestID string, work func(context.Context, Account) (any, error)) map[string]any {
	if requestID == "" || len(requestID) > 128 {
		return map[string]any{"error": "帳單請求識別碼無效"}
	}
	var account Account
	for _, candidate := range b.store.list() {
		if candidate.ID == accountID {
			account = candidate
			break
		}
	}
	if account.ID == "" {
		return map[string]any{"error": "找不到此帳號，請重新整理列表"}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return map[string]any{"error": "帳單服務已關閉"}
	}
	if _, exists := b.pending[requestID]; exists || len(b.pending) >= 4 {
		return map[string]any{"error": "帳單操作進行中，請稍候再試"}
	}
	ctx, cancel := context.WithTimeout(b.ctx, 25*time.Second)
	request := &billingRequest{cancel: cancel}
	b.pending[requestID] = request
	go func() {
		defer cancel()
		result, err := work(ctx, account)
		if err != nil {
			result = map[string]any{"error": err.Error()}
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.closed || b.pending[requestID] != request {
			return
		}
		delete(b.pending, requestID)
		b.notify(requestID, result)
	}()
	return map[string]any{"pending": true}
}

func (b *billingBridge) cancelRequest(requestID string) map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	if request := b.pending[requestID]; request != nil {
		request.cancel()
		delete(b.pending, requestID)
	}
	return map[string]any{"ok": true}
}

func (b *billingBridge) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.cancel()
	clear(b.pending)
}

package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 测试 InvalidationBroker 只向订阅者发送选定 domain，并保持全局 sequence 单调。
func TestInvalidationBrokerFiltersDomainsAndSequencesEvents(t *testing.T) {
	broker, err := NewInvalidationBroker(2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	events, unsubscribe, err := broker.Subscribe(t.Context(), []InvalidationDomain{InvalidationQuota}, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unsubscribe)
	if err := broker.Notify(t.Context(), InvalidationIndex); err != nil {
		t.Fatal(err)
	}
	if err := broker.Notify(t.Context(), InvalidationQuota); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Domain != InvalidationQuota || event.Sequence != 2 || event.Version != InvalidationContractVersion {
			t.Fatalf("event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for quota invalidation")
	}
}

// 测试 InvalidationBroker 在慢消费者队列满时合并到最新 hint，不阻塞业务调用。
func TestInvalidationBrokerCoalescesSlowSubscriber(t *testing.T) {
	broker, err := NewInvalidationBroker(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	events, unsubscribe, err := broker.Subscribe(t.Context(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unsubscribe)
	if err := broker.Notify(t.Context(), InvalidationIndex); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := broker.Notify(t.Context(), InvalidationHealth); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 100*time.Millisecond {
		t.Fatal("Notify blocked on slow subscriber")
	}
	event := <-events
	if event.Domain != InvalidationHealth || event.Sequence != 2 {
		t.Fatalf("coalesced event = %#v, want latest health sequence 2", event)
	}
}

// 测试 InvalidationBroker 在订阅 context 取消后关闭 channel，并拒绝非法 domain。
func TestInvalidationBrokerClosesCancelledSubscriptionAndRejectsInvalidDomain(t *testing.T) {
	broker, err := NewInvalidationBroker(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	ctx, cancel := context.WithCancel(t.Context())
	events, _, err := broker.Subscribe(ctx, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("subscription channel remains open after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not close after cancellation")
	}
	if err := broker.Notify(t.Context(), InvalidationDomain("secret")); !errors.Is(err, ErrInvalidation) {
		t.Fatalf("Notify(invalid) error = %v, want ErrInvalidation", err)
	}
}

// 测试手动取消订阅时同时终止 context 观察协程，避免长生命周期 context 泄漏等待者。
func TestInvalidationBrokerUnsubscribeReleasesContextObserver(t *testing.T) {
	broker, err := NewInvalidationBroker(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	events, unsubscribe, err := broker.Subscribe(context.Background(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	broker.mu.Lock()
	var observerDone <-chan struct{}
	for _, subscriber := range broker.subscribers {
		observerDone = subscriber.observerDone
	}
	broker.mu.Unlock()
	if observerDone == nil {
		t.Fatal("subscription did not expose observer completion")
	}

	unsubscribe()
	select {
	case <-observerDone:
	case <-time.After(time.Second):
		t.Fatal("context observer remains blocked after unsubscribe")
	}
	if _, ok := <-events; ok {
		t.Fatal("subscription channel remains open after unsubscribe")
	}
}

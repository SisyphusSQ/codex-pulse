package core

import (
	"context"
	"errors"
	"sync"
)

const InvalidationContractVersion = "query-invalidation-v2"

var ErrInvalidation = errors.New("core invalidation is invalid")

type InvalidationDomain string

const (
	InvalidationIndex    InvalidationDomain = "index"
	InvalidationQuota    InvalidationDomain = "quota"
	InvalidationHealth   InvalidationDomain = "health"
	InvalidationSettings InvalidationDomain = "settings"
)

type InvalidationEvent struct {
	Version  string
	Domain   InvalidationDomain
	Sequence uint64
}

type invalidationSubscriber struct {
	domains      map[InvalidationDomain]struct{}
	after        uint64
	events       chan InvalidationEvent
	stopObserver chan struct{}
	observerDone chan struct{}
}

// InvalidationBroker 把业务提交转换为有界、不可阻塞的重新查询提示。
type InvalidationBroker struct {
	mu          sync.Mutex
	capacity    int
	sequence    uint64
	nextID      uint64
	subscribers map[uint64]*invalidationSubscriber
	closed      bool
}

func NewInvalidationBroker(capacity int) (*InvalidationBroker, error) {
	if capacity <= 0 {
		return nil, ErrInvalidation
	}
	return &InvalidationBroker{
		capacity: capacity, subscribers: make(map[uint64]*invalidationSubscriber),
	}, nil
}

func (broker *InvalidationBroker) Subscribe(
	ctx context.Context,
	domains []InvalidationDomain,
	after uint64,
) (<-chan InvalidationEvent, func(), error) {
	if broker == nil || ctx == nil || ctx.Err() != nil {
		return nil, nil, ErrInvalidation
	}
	filter := make(map[InvalidationDomain]struct{}, len(domains))
	for _, domain := range domains {
		if !validInvalidationDomain(domain) {
			return nil, nil, ErrInvalidation
		}
		filter[domain] = struct{}{}
	}
	broker.mu.Lock()
	if broker.closed || after > broker.sequence {
		broker.mu.Unlock()
		return nil, nil, ErrInvalidation
	}
	broker.nextID++
	id := broker.nextID
	subscriber := &invalidationSubscriber{
		domains: filter, after: after, events: make(chan InvalidationEvent, broker.capacity),
		stopObserver: make(chan struct{}), observerDone: make(chan struct{}),
	}
	broker.subscribers[id] = subscriber
	broker.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			broker.mu.Lock()
			current, ok := broker.subscribers[id]
			if ok {
				delete(broker.subscribers, id)
				close(current.events)
				close(current.stopObserver)
			}
			broker.mu.Unlock()
		})
	}
	go func() {
		defer close(subscriber.observerDone)
		select {
		case <-ctx.Done():
			unsubscribe()
		case <-subscriber.stopObserver:
		}
	}()
	return subscriber.events, unsubscribe, nil
}

func (broker *InvalidationBroker) Notify(ctx context.Context, domain InvalidationDomain) error {
	if broker == nil || ctx == nil || !validInvalidationDomain(domain) {
		return ErrInvalidation
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if broker.closed {
		return ErrInvalidation
	}
	broker.sequence++
	event := InvalidationEvent{
		Version: InvalidationContractVersion, Domain: domain, Sequence: broker.sequence,
	}
	for _, subscriber := range broker.subscribers {
		if subscriber.after >= event.Sequence || !subscriber.accepts(domain) {
			continue
		}
		select {
		case subscriber.events <- event:
		default:
			// Invalidation 是重新查询提示。队列满时丢弃旧 hint 并保留最新 sequence，不能反压业务提交。
			select {
			case <-subscriber.events:
			default:
			}
			select {
			case subscriber.events <- event:
			default:
			}
		}
	}
	return nil
}

func (broker *InvalidationBroker) Close() {
	if broker == nil {
		return
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if broker.closed {
		return
	}
	broker.closed = true
	for id, subscriber := range broker.subscribers {
		delete(broker.subscribers, id)
		close(subscriber.events)
		close(subscriber.stopObserver)
	}
}

func (subscriber *invalidationSubscriber) accepts(domain InvalidationDomain) bool {
	if subscriber == nil || len(subscriber.domains) == 0 {
		return true
	}
	_, ok := subscriber.domains[domain]
	return ok
}

func validInvalidationDomain(domain InvalidationDomain) bool {
	switch domain {
	case InvalidationIndex, InvalidationQuota, InvalidationHealth, InvalidationSettings:
		return true
	default:
		return false
	}
}

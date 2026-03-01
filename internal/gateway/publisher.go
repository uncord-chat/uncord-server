package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/events"
)

const eventsChannel = "uncord.gateway.events"

// envelope is the JSON structure published to the gateway events channel. When Targets is non-empty, the hub delivers
// the event only to the listed user IDs instead of broadcasting to all identified clients.
type envelope struct {
	Type    string      `json:"t"`
	Data    any         `json:"d"`
	Targets []uuid.UUID `json:"targets,omitempty"`
}

// job is a single gateway event waiting to be published by a worker.
type job struct {
	eventType events.DispatchEvent
	data      any
	targets   []uuid.UUID
}

// Publisher serialises dispatch events and publishes them to a Valkey pub/sub channel for consumption by the gateway.
// It maintains an internal worker pool so that callers can enqueue events without blocking.
type Publisher struct {
	rdb     *redis.Client
	log     zerolog.Logger
	queue   chan job
	workers int
	timeout time.Duration
	dropped atomic.Int64
}

// NewPublisher creates a new gateway event publisher with a bounded worker pool. The workers parameter controls how many
// goroutines consume from the internal queue. The queueSize parameter sets the channel buffer length. The
// publishTimeout parameter caps how long each Valkey publish may take before being abandoned.
func NewPublisher(rdb *redis.Client, logger zerolog.Logger, workers, queueSize int, publishTimeout time.Duration) *Publisher {
	return &Publisher{
		rdb:     rdb,
		log:     logger,
		queue:   make(chan job, queueSize),
		workers: workers,
		timeout: publishTimeout,
	}
}

// Enqueue submits a gateway event for asynchronous publication. If the internal queue is full the event is dropped and a
// warning is logged. This method is safe to call from any goroutine.
func (p *Publisher) Enqueue(eventType events.DispatchEvent, data any) {
	p.enqueue(job{eventType: eventType, data: data})
}

// EnqueueTargeted submits a gateway event that will only be delivered to the specified user IDs. If the internal queue
// is full the event is dropped and a warning is logged.
func (p *Publisher) EnqueueTargeted(eventType events.DispatchEvent, data any, targets []uuid.UUID) {
	p.enqueue(job{eventType: eventType, data: data, targets: targets})
}

func (p *Publisher) enqueue(j job) {
	select {
	case p.queue <- j:
	default:
		p.dropped.Add(1)
		p.log.Warn().Str("event", string(j.eventType)).Msg("Gateway publish queue full, event dropped")
	}
}

// Run starts the worker pool and blocks until ctx is cancelled. After cancellation it closes the queue channel, drains
// remaining items, and waits for all workers to finish. Returns nil on clean shutdown or context.Canceled.
func (p *Publisher) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	for range p.workers {
		wg.Go(func() {
			for j := range p.queue {
				p.publishWithTimeout(j)
			}
		})
	}

	<-ctx.Done()
	close(p.queue)
	wg.Wait()

	if dropped := p.dropped.Load(); dropped > 0 {
		p.log.Warn().Int64("dropped", dropped).Msg("Gateway publish queue dropped events during lifetime")
	}

	return ctx.Err()
}

// publishWithTimeout publishes a single job with a bounded context timeout.
func (p *Publisher) publishWithTimeout(j job) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	if err := p.publish(ctx, j.eventType, j.data, j.targets); err != nil {
		p.log.Warn().Err(err).Str("event", string(j.eventType)).Msg("Worker publish failed")
	}
}

// Publish serialises the event as JSON and publishes it to the gateway events channel. This is the synchronous
// low-level method used internally by the worker pool and by the gateway Hub.
func (p *Publisher) Publish(ctx context.Context, eventType events.DispatchEvent, data any) error {
	return p.publish(ctx, eventType, data, nil)
}

func (p *Publisher) publish(ctx context.Context, eventType events.DispatchEvent, data any, targets []uuid.UUID) error {
	payload, err := json.Marshal(envelope{Type: string(eventType), Data: data, Targets: targets})
	if err != nil {
		return fmt.Errorf("marshal gateway event: %w", err)
	}
	if err := p.rdb.Publish(ctx, eventsChannel, payload).Err(); err != nil {
		return fmt.Errorf("publish gateway event: %w", err)
	}
	return nil
}

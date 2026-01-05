package pool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/bang-go/micro/telemetry/logger"
)

var (
	// ErrPoolClosed is returned when submitting to a closed pool.
	ErrPoolClosed = errors.New("pool closed")
	// ErrPoolFull is returned when the pool queue is full in non-blocking mode.
	ErrPoolFull = errors.New("pool full")
)

// Pool interface defines the worker pool behaviors.
type Pool interface {
	// Submit submits a task to the pool.
	Submit(task func()) error
	// Release closes the pool and waits for workers to finish.
	Release()
	// Running returns the number of currently running workers (processing tasks).
	Running() int
	// Cap returns the capacity (number of workers) of the pool.
	Cap() int
}

type pool struct {
	cap     int32
	running int32
	taskC   chan func()
	wg      sync.WaitGroup
	options *options
	closed  int32
}

// New creates a new fixed-size worker pool.
func New(size int, opts ...Option) (Pool, error) {
	if size <= 0 {
		return nil, fmt.Errorf("pool size must be positive")
	}

	o := &options{
		queueSize: size, // Default queue size equals pool size
	}
	for _, opt := range opts {
		opt(o)
	}

	if o.queueSize <= 0 {
		o.queueSize = size
	}

	// Default logger if not provided
	if o.logger == nil {
		o.logger = logger.New(logger.WithLevel("info"))
	}

	p := &pool{
		cap:     int32(size),
		taskC:   make(chan func(), o.queueSize),
		options: o,
	}

	p.wg.Add(size)
	for i := 0; i < size; i++ {
		go p.worker()
	}

	return p, nil
}

func (p *pool) Cap() int {
	return int(p.cap)
}

func (p *pool) Running() int {
	return int(atomic.LoadInt32(&p.running))
}

func (p *pool) worker() {
	defer p.wg.Done()

	for task := range p.taskC {
		atomic.AddInt32(&p.running, 1)
		p.runTask(task)
		atomic.AddInt32(&p.running, -1)
	}
}

func (p *pool) runTask(task func()) {
	defer func() {
		if r := recover(); r != nil {
			if p.options.panicHandler != nil {
				p.options.panicHandler(r)
			} else if p.options.logger != nil {
				p.options.logger.Error(context.Background(), "worker panic", "panic", r)
			} else {
				// Fallback to slog default, though options.logger should be set now
				slog.Error("worker panic", "panic", r)
			}
		}
	}()
	task()
}

func (p *pool) Submit(task func()) (err error) {
	if atomic.LoadInt32(&p.closed) == 1 {
		return ErrPoolClosed
	}

	// Catch panic if channel is closed while sending
	defer func() {
		if r := recover(); r != nil {
			err = ErrPoolClosed
		}
	}()

	if p.options.nonBlocking {
		select {
		case p.taskC <- task:
			return nil
		default:
			return ErrPoolFull
		}
	} else {
		p.taskC <- task
	}
	return nil
}

func (p *pool) Release() {
	if !atomic.CompareAndSwapInt32(&p.closed, 0, 1) {
		return
	}
	close(p.taskC)
	p.wg.Wait()
}

package pool

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/bang-go/micro/telemetry/logger"
)

var (
	ErrContextRequired = errors.New("pool: context is required")
	ErrPoolClosed      = errors.New("pool: closed")
	ErrPoolFull        = errors.New("pool: full")
	ErrNilTask         = errors.New("pool: task is required")
)

type Pool interface {
	Submit(task func()) error
	SubmitContext(context.Context, func()) error
	Release()
	Running() int
	Pending() int
	Cap() int
	IsClosed() bool
}

type pool struct {
	capacity int
	options  *options

	mu       sync.RWMutex
	cond     *sync.Cond
	queue    *list.List
	queueCap int
	closed   bool

	wg      sync.WaitGroup
	running int32
	once    sync.Once
}

func New(size int, opts ...Option) (Pool, error) {
	if size <= 0 {
		return nil, fmt.Errorf("pool: size must be positive")
	}

	config := &options{
		queueSize: size,
	}
	for _, opt := range opts {
		opt(config)
	}
	if config.queueSize <= 0 {
		config.queueSize = size
	}
	if config.logger == nil {
		config.logger = logger.New(logger.WithLevel("info"))
	}

	p := &pool{
		capacity: size,
		options:  config,
		queue:    list.New(),
		queueCap: config.queueSize,
	}
	p.cond = sync.NewCond(&p.mu)

	p.wg.Add(size)
	for i := 0; i < size; i++ {
		go p.worker()
	}

	return p, nil
}

func (p *pool) Submit(task func()) error {
	return p.SubmitContext(context.Background(), task)
}

func (p *pool) SubmitContext(ctx context.Context, task func()) error {
	if task == nil {
		return ErrNilTask
	}
	if ctx == nil {
		return ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrPoolClosed
	}

	if p.options.nonBlocking {
		if err := ctx.Err(); err != nil {
			return err
		}
		if p.queue.Len() >= p.queueCap {
			return ErrPoolFull
		}
		p.queue.PushBack(task)
		p.cond.Signal()
		return nil
	}

	stop := context.AfterFunc(ctx, func() {
		p.mu.Lock()
		p.cond.Broadcast()
		p.mu.Unlock()
	})
	defer stop()

	for !p.closed && p.queue.Len() >= p.queueCap && ctx.Err() == nil {
		p.cond.Wait()
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if p.closed {
		return ErrPoolClosed
	}

	p.queue.PushBack(task)
	p.cond.Signal()
	return nil
}

func (p *pool) Release() {
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.cond.Broadcast()
		p.mu.Unlock()
		p.wg.Wait()
	})
}

func (p *pool) Running() int {
	return int(atomic.LoadInt32(&p.running))
}

func (p *pool) Pending() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.queue.Len()
}

func (p *pool) Cap() int {
	return p.capacity
}

func (p *pool) IsClosed() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.closed
}

func (p *pool) worker() {
	defer p.wg.Done()

	for {
		task, ok := p.nextTask()
		if !ok {
			return
		}
		p.runTask(task)
	}
}

func (p *pool) nextTask() (func(), bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for p.queue.Len() == 0 && !p.closed {
		p.cond.Wait()
	}
	if p.queue.Len() == 0 {
		return nil, false
	}

	element := p.queue.Front()
	task := element.Value.(func())
	p.queue.Remove(element)
	p.cond.Signal()
	return task, true
}

func (p *pool) runTask(task func()) {
	atomic.AddInt32(&p.running, 1)
	defer atomic.AddInt32(&p.running, -1)

	defer func() {
		if recovered := recover(); recovered != nil {
			if p.options.panicHandler != nil {
				p.options.panicHandler(recovered)
				return
			}
			if p.options.logger != nil {
				p.options.logger.Error(context.Background(), "pool worker panic", "panic", recovered, "stack", string(debug.Stack()))
				return
			}
			slog.Error("pool worker panic", "panic", recovered, "stack", string(debug.Stack()))
		}
	}()

	task()
}

package pool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	_, err := New(0)
	if err == nil {
		t.Fatal("expected error for non-positive size")
	}
}

func TestPoolSubmitAndRelease(t *testing.T) {
	p, err := New(4)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var count int32
	var wg sync.WaitGroup
	const tasks = 32
	wg.Add(tasks)

	for i := 0; i < tasks; i++ {
		err := p.Submit(func() {
			atomic.AddInt32(&count, 1)
			wg.Done()
		})
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
	}

	wg.Wait()
	p.Release()

	if got := atomic.LoadInt32(&count); got != tasks {
		t.Fatalf("expected %d tasks to run, got %d", tasks, got)
	}
	if !p.IsClosed() {
		t.Fatal("expected pool to be closed after Release()")
	}
	if err := p.Submit(func() {}); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("expected ErrPoolClosed after release, got %v", err)
	}
}

func TestPoolSubmitContext(t *testing.T) {
	p, err := New(1, WithQueueSize(1))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer p.Release()

	blocker := make(chan struct{})
	if err := p.Submit(func() { <-blocker }); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := p.Submit(func() {}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = p.SubmitContext(ctx, func() {})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}

	close(blocker)
}

func TestPoolSubmitContextRequiresContext(t *testing.T) {
	p, err := New(1)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer p.Release()

	if err := p.SubmitContext(nil, func() {}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
}

func TestPoolNonBlocking(t *testing.T) {
	p, err := New(1, WithNonBlocking(true), WithQueueSize(1))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer p.Release()

	blocker := make(chan struct{})
	started := make(chan struct{})
	if err := p.Submit(func() {
		close(started)
		<-blocker
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	<-started
	if err := p.Submit(func() {}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := p.Submit(func() {}); !errors.Is(err, ErrPoolFull) {
		t.Fatalf("expected ErrPoolFull, got %v", err)
	}

	close(blocker)
}

func TestPoolNonBlockingHonorsCanceledContext(t *testing.T) {
	p, err := New(1, WithNonBlocking(true), WithQueueSize(1))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer p.Release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := p.SubmitContext(ctx, func() {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestPoolNilTask(t *testing.T) {
	p, err := New(1)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer p.Release()

	if err := p.Submit(nil); !errors.Is(err, ErrNilTask) {
		t.Fatalf("expected ErrNilTask, got %v", err)
	}
}

func TestPoolPanicHandler(t *testing.T) {
	var panicked int32
	p, err := New(1, WithPanicHandler(func(any) {
		atomic.StoreInt32(&panicked, 1)
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer p.Release()

	done := make(chan struct{})
	if err := p.Submit(func() {
		defer close(done)
		panic("boom")
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	<-done

	for i := 0; i < 50; i++ {
		if atomic.LoadInt32(&panicked) == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("expected panic handler to be invoked")
}

func TestPoolPendingAndRunning(t *testing.T) {
	p, err := New(1, WithQueueSize(4))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer p.Release()

	started := make(chan struct{})
	release := make(chan struct{})
	if err := p.Submit(func() {
		close(started)
		<-release
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	<-started

	if running := p.Running(); running != 1 {
		t.Fatalf("expected one running worker, got %d", running)
	}

	for i := 0; i < 3; i++ {
		if err := p.Submit(func() {}); err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
	}
	if pending := p.Pending(); pending != 3 {
		t.Fatalf("expected 3 pending tasks, got %d", pending)
	}

	close(release)
}

func TestPoolReleaseIsIdempotent(t *testing.T) {
	p, err := New(2)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.Release()
	p.Release()
}

func TestPoolReleaseUnblocksBlockingSubmit(t *testing.T) {
	p, err := New(1, WithQueueSize(1))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	blocker := make(chan struct{})
	started := make(chan struct{})
	if err := p.Submit(func() {
		close(started)
		<-blocker
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	<-started
	if err := p.Submit(func() {}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	result := make(chan error, 1)
	go func() {
		result <- p.Submit(func() {})
	}()

	time.Sleep(20 * time.Millisecond)
	releaseDone := make(chan struct{})
	go func() {
		p.Release()
		close(releaseDone)
	}()

	select {
	case err := <-result:
		if !errors.Is(err, ErrPoolClosed) {
			t.Fatalf("expected ErrPoolClosed, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Submit() did not unblock on Release()")
	}

	close(blocker)

	select {
	case <-releaseDone:
	case <-time.After(time.Second):
		t.Fatal("Release() did not complete")
	}
}

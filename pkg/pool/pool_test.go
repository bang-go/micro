package pool

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPool_Submit(t *testing.T) {
	p, err := New(10)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Release()

	var count int32
	var wg sync.WaitGroup
	n := 100

	wg.Add(n)
	for i := 0; i < n; i++ {
		err := p.Submit(func() {
			atomic.AddInt32(&count, 1)
			wg.Done()
		})
		if err != nil {
			t.Errorf("submit failed: %v", err)
		}
	}

	wg.Wait()
	if count != int32(n) {
		t.Errorf("expected %d, got %d", n, count)
	}
}

func TestPool_Panic(t *testing.T) {
	var panicked int32
	p, _ := New(1, WithPanicHandler(func(v interface{}) {
		atomic.StoreInt32(&panicked, 1)
	}))
	defer p.Release()

	var wg sync.WaitGroup
	wg.Add(1)
	p.Submit(func() {
		defer wg.Done()
		panic("oops")
	})

	wg.Wait()

	// Handler runs in the same goroutine before next task, so it should be visible immediately after wg.Done()
	// Actually wg.Done() is in task().
	// Panic handler is in defer of runTask().
	// So runTask() defer -> panic handler -> task() defer (Wait, no. task() defer runs first).
	// task() runs. Panic.
	// task() defers run.
	// runTask() defers run (recover).

	// So if I put wg.Done() in task defer, it runs before recover?
	// Yes. `defer` stack.
	// So when wg.Wait() returns, the panic handler MIGHT NOT have run yet?
	// No, recover happens after task's defers.
	// So panic handler runs AFTER wg.Done().

	// So checking `panicked` immediately might race.
	// I should verify panicked eventually.

	for i := 0; i < 100; i++ {
		if atomic.LoadInt32(&panicked) == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Error("panic handler not called")
}

func TestPool_NonBlocking(t *testing.T) {
	// Queue size 1, Pool size 1 (busy)
	p, _ := New(1, WithNonBlocking(true), WithQueueSize(1))
	defer p.Release()

	// Make worker busy
	start := make(chan struct{})
	done := make(chan struct{})

	p.Submit(func() {
		close(start)
		<-done
	})

	<-start
	// Now worker is busy.

	// Fill buffer (size 1)
	err := p.Submit(func() {})
	if err != nil {
		t.Errorf("expected success for buffer, got %v", err)
	}

	// Next should fail
	err = p.Submit(func() {})
	if !errors.Is(err, ErrPoolFull) {
		t.Errorf("expected ErrPoolFull, got %v", err)
	}

	close(done)
}

func TestPool_Release(t *testing.T) {
	p, _ := New(5)

	var count int32
	for i := 0; i < 50; i++ {
		p.Submit(func() {
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&count, 1)
		})
	}

	p.Release()

	if count != 50 {
		t.Errorf("Release did not wait for all tasks, got %d", count)
	}

	err := p.Submit(func() {})
	if !errors.Is(err, ErrPoolClosed) {
		t.Errorf("expected ErrPoolClosed after Release, got %v", err)
	}
}

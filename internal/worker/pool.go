package worker

import (
	"context"
	"log"
	"sync"
)

// Task identifies a single unit of work: generate a thumbnail for assetID,
// tracked under jobID.
type Task struct {
	AssetID string
	JobID   string
}

// Processor performs the actual work for a Task.
type Processor interface {
	Process(ctx context.Context, task Task)
}

// Pool runs a fixed number of goroutines that pull Tasks off a channel and
// hand them to a Processor. It is deliberately simple: an in-memory,
// single-process queue rather than a durable one, which is a known
// limitation documented in ACTUAL_IMPLEMENTATION.md.
type Pool struct {
	tasks     chan Task
	processor Processor
	wg        sync.WaitGroup
}

func NewPool(processor Processor) *Pool {
	return &Pool{
		tasks:     make(chan Task, 64),
		processor: processor,
	}
}

// Start launches the worker goroutines. It returns immediately; call Stop
// to drain and shut the pool down.
func (p *Pool) Start(ctx context.Context, size int) {
	if size < 1 {
		size = 1
	}
	for i := 0; i < size; i++ {
		p.wg.Add(1)
		go p.loop(ctx)
	}
}

func (p *Pool) loop(ctx context.Context) {
	defer p.wg.Done()
	for {
		select {
		case task, ok := <-p.tasks:
			if !ok {
				return
			}
			p.safeProcess(ctx, task)
		case <-ctx.Done():
			return
		}
	}
}

// safeProcess recovers from a panic in the processor so one bad task
// cannot take down the entire worker pool.
func (p *Pool) safeProcess(ctx context.Context, task Task) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("worker: recovered from panic processing job %s: %v", task.JobID, r)
		}
	}()
	p.processor.Process(ctx, task)
}

// Enqueue submits a task for processing. It blocks if the internal queue is
// full, applying backpressure to callers rather than growing unbounded.
func (p *Pool) Enqueue(task Task) {
	p.tasks <- task
}

// Stop closes the task queue and waits for in-flight tasks to finish.
func (p *Pool) Stop() {
	close(p.tasks)
	p.wg.Wait()
}

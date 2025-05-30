package executor

import (
	"fmt"
	"sync"
	"sync/atomic"

	"go.temporal.io/server/common"
	"go.temporal.io/server/common/metrics"
)

type (
	// Task defines the interface for a runnable task
	Task interface {
		// Run should execute the task and return well known status codes
		Run() TaskStatus
	}

	// Executor defines the interface for any executor which can
	// accept tasks and execute them based on some policy
	Executor interface {
		// Start starts the executor
		Start()
		// Stop stops the executor
		Stop()
		// Submit is a blocking call that accepts a task to execute
		Submit(task Task) bool
		// TaskCount returns the number of outstanding tasks in the executor
		TaskCount() int64
	}

	// fixedPoolExecutor is an implementation of an executor that uses fixed size
	// goroutine pool. This executor also supports deferred execution of tasks
	// for fairness
	fixedPoolExecutor struct {
		size           int
		maxDeferred    int
		runQ           *runQueue
		outstanding    int64
		status         int32
		metricsHandler metrics.Handler
		stopC          chan struct{}
		stopWG         sync.WaitGroup
	}

	// TaskStatus is the return code from a Task
	TaskStatus int
)

const (
	// TaskStatusDone indicates task is finished successfully
	TaskStatusDone TaskStatus = iota
	// TaskStatusDefer indicates task should be scheduled again for execution at later time
	TaskStatusDefer
	// TaskStatusErr indicates task is finished with errors
	TaskStatusErr
)

// NewFixedSizePoolExecutor returns an implementation of task executor that maintains
// a fixed size pool of goroutines.The returned executor also allows task processing to
// to be deferred for fairness. To defer processing of a task, simply return TaskStatsDefer
// from your task.Run method. When a task is deferred, it will be added to the tail of a
// deferredTaskQ which in turn will be processed after the current runQ is drained
func NewFixedSizePoolExecutor(size int, maxDeferred int, metricsHandler metrics.Handler, operation string) Executor {
	stopC := make(chan struct{})
	return &fixedPoolExecutor{
		size:           size,
		maxDeferred:    maxDeferred,
		runQ:           newRunQueue(size, stopC),
		metricsHandler: metricsHandler.WithTags(metrics.OperationTag(operation)),
		stopC:          stopC,
	}
}

// Start starts the executor
func (e *fixedPoolExecutor) Start() {
	if !atomic.CompareAndSwapInt32(&e.status, common.DaemonStatusInitialized, common.DaemonStatusStarted) {
		return
	}
	for i := 0; i < e.size; i++ {
		e.stopWG.Add(1)
		go e.worker()
	}
}

// Stop stops the executor
func (e *fixedPoolExecutor) Stop() {
	if !atomic.CompareAndSwapInt32(&e.status, common.DaemonStatusStarted, common.DaemonStatusStopped) {
		return
	}
	close(e.stopC)
	e.stopWG.Wait()
}

// Submit is a blocking call that accepts a task for execution
func (e *fixedPoolExecutor) Submit(task Task) bool {
	if !e.alive() {
		return false
	}
	added := e.runQ.add(task)
	if added {
		atomic.AddInt64(&e.outstanding, 1)
	}
	return added
}

// TaskCount returns the total of number of tasks currently outstanding
func (e *fixedPoolExecutor) TaskCount() int64 {
	return atomic.LoadInt64(&e.outstanding)
}

func (e *fixedPoolExecutor) worker() {
	defer e.stopWG.Done()
	for e.alive() {
		task, ok := e.runQ.remove()
		if !ok {
			return
		}

		status := task.Run()
		switch status {
		case TaskStatusDone:
			atomic.AddInt64(&e.outstanding, -1)
			metrics.ExecutorTasksDoneCount.With(e.metricsHandler).Record(1)
		case TaskStatusDefer:
			if e.runQ.deferredCount() < e.maxDeferred {
				e.runQ.addAndDefer(task)
				metrics.ExecutorTasksDeferredCount.With(e.metricsHandler).Record(1)
			} else {
				atomic.AddInt64(&e.outstanding, -1)
				metrics.ExecutorTasksDroppedCount.With(e.metricsHandler).Record(1)
			}
		case TaskStatusErr:
			atomic.AddInt64(&e.outstanding, -1)
			metrics.ExecutorTasksErrCount.With(e.metricsHandler).Record(1)
		default:
			panic(fmt.Sprintf("unknown task status: %v", status))
		}
	}
}

func (e *fixedPoolExecutor) alive() bool {
	return atomic.LoadInt32(&e.status) == common.DaemonStatusStarted
}

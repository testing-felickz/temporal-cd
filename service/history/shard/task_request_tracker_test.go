package shard

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/server/service/history/tasks"
	"go.temporal.io/server/service/history/tests"
)

type (
	taskRequestTrackerSuite struct {
		suite.Suite
		*require.Assertions

		tracker *taskRequestTracker
	}
)

func TestTaskRequestTrackerSuite(t *testing.T) {
	s := &taskRequestTrackerSuite{}
	suite.Run(t, s)
}

func (s *taskRequestTrackerSuite) SetupTest() {
	s.Assertions = require.New(s.T())

	s.tracker = newTaskRequestTracker(tasks.NewDefaultTaskCategoryRegistry())
}

func (s *taskRequestTrackerSuite) TestTrackAndMinTaskKey() {
	now := time.Now()

	_ = s.tracker.track(s.convertKeysToTasks(map[tasks.Category][]tasks.Key{
		tasks.CategoryTransfer: {
			tasks.NewImmediateKey(123),
			tasks.NewImmediateKey(125),
		},
		tasks.CategoryTimer: {
			tasks.NewKey(now, 124),
			tasks.NewKey(now.Add(time.Minute), 122),
		},
	}))
	s.assertMinTaskKey(tasks.CategoryTransfer, tasks.NewImmediateKey(123))
	s.assertMinTaskKey(tasks.CategoryTimer, tasks.NewKey(now, 124))

	_ = s.tracker.track(s.convertKeysToTasks(map[tasks.Category][]tasks.Key{
		tasks.CategoryTransfer: {
			tasks.NewImmediateKey(130),
		},
		tasks.CategoryTimer: {
			tasks.NewKey(now.Add(-time.Minute), 131),
		},
	}))
	s.assertMinTaskKey(tasks.CategoryTransfer, tasks.NewImmediateKey(123))
	s.assertMinTaskKey(tasks.CategoryTimer, tasks.NewKey(now.Add(-time.Minute), 131))

	_, ok := s.tracker.minTaskKey(tasks.CategoryVisibility)
	s.False(ok)
}

func (s *taskRequestTrackerSuite) TestRequestCompletion() {
	completionFunc1 := s.tracker.track(s.convertKeysToTasks(map[tasks.Category][]tasks.Key{
		tasks.CategoryTransfer: {
			tasks.NewImmediateKey(123),
			tasks.NewImmediateKey(125),
		},
	}))
	completionFunc2 := s.tracker.track(s.convertKeysToTasks(map[tasks.Category][]tasks.Key{
		tasks.CategoryTransfer: {
			tasks.NewImmediateKey(122),
		},
	}))
	completionFunc3 := s.tracker.track(s.convertKeysToTasks(map[tasks.Category][]tasks.Key{
		tasks.CategoryTransfer: {
			tasks.NewImmediateKey(127),
		},
	}))
	s.assertMinTaskKey(tasks.CategoryTransfer, tasks.NewImmediateKey(122))

	completionFunc2(nil)
	s.assertMinTaskKey(tasks.CategoryTransfer, tasks.NewImmediateKey(123))

	completionFunc3(serviceerror.NewNotFound("not found error guarantees task is not inserted"))
	s.assertMinTaskKey(tasks.CategoryTransfer, tasks.NewImmediateKey(123))

	completionFunc1(errors.New("random error means task may still be inserted in the future"))
	s.assertMinTaskKey(tasks.CategoryTransfer, tasks.NewImmediateKey(123))

	s.tracker.drain()
}

func (s *taskRequestTrackerSuite) TestDrain() {
	// drain should not block if there is no inflight request
	s.tracker.drain()

	completionFunc1 := s.tracker.track(s.convertKeysToTasks(map[tasks.Category][]tasks.Key{
		tasks.CategoryTransfer: {
			tasks.NewImmediateKey(123),
		},
	}))
	completionFunc2 := s.tracker.track(s.convertKeysToTasks(map[tasks.Category][]tasks.Key{
		tasks.CategoryTransfer: {
			tasks.NewImmediateKey(122),
		},
	}))
	completionFunc3 := s.tracker.track(s.convertKeysToTasks(map[tasks.Category][]tasks.Key{
		tasks.CategoryTransfer: {
			tasks.NewImmediateKey(127),
		},
	}))

	for _, completionFn := range []taskRequestCompletionFn{
		completionFunc1,
		completionFunc2,
		completionFunc3,
	} {
		go func(completionFn taskRequestCompletionFn) {
			completionFn(nil)
		}(completionFn)
	}

	s.tracker.drain()
}

func (s *taskRequestTrackerSuite) TestClear() {
	_ = s.tracker.track(s.convertKeysToTasks(map[tasks.Category][]tasks.Key{
		tasks.CategoryTransfer: {
			tasks.NewImmediateKey(123),
			tasks.NewImmediateKey(125),
		},
	}))
	completionFn := s.tracker.track(s.convertKeysToTasks(map[tasks.Category][]tasks.Key{
		tasks.CategoryTransfer: {
			tasks.NewImmediateKey(122),
		},
	}))
	completionFn(errors.New("some random error"))
	s.assertMinTaskKey(tasks.CategoryTransfer, tasks.NewImmediateKey(122))

	s.tracker.clear()
	_, ok := s.tracker.minTaskKey(tasks.CategoryTransfer)
	s.False(ok)
	s.tracker.drain()
}

func (s *taskRequestTrackerSuite) assertMinTaskKey(
	category tasks.Category,
	expectedKey tasks.Key,
) {
	actualKey, ok := s.tracker.minTaskKey(category)
	s.True(ok)
	s.Zero(expectedKey.CompareTo(actualKey))
}

func (s *taskRequestTrackerSuite) convertKeysToTasks(
	keysByCategory map[tasks.Category][]tasks.Key,
) map[tasks.Category][]tasks.Task {
	tasksByCategory := make(map[tasks.Category][]tasks.Task)
	for category, keys := range keysByCategory {
		tasksByCategory[category] = make([]tasks.Task, 0, len(keys))
		for _, key := range keys {
			fakeTask := tasks.NewFakeTask(
				tests.WorkflowKey,
				category,
				time.Time{},
			)
			fakeTask.SetTaskID(key.TaskID)
			fakeTask.SetVisibilityTime(key.FireTime)
			tasksByCategory[category] = append(tasksByCategory[category], fakeTask)
		}
	}

	return tasksByCategory
}

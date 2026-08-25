package insight

import (
	"context"
	"sync"
)

// The insight report is built from independent reads. Running them one after another
// makes the page wait for the sum of every query, but running all of them at once
// would let a couple of concurrent readers exhaust the connection pool. The steps
// therefore run concurrently with a small fixed ceiling.

// queryConcurrency is how many reads of one report may be in flight at once. The
// pool holds twenty connections, so this leaves room for other requests.
const queryConcurrency = 4

// runParallel executes every step with at most limit running concurrently. It
// returns the first error and cancels the remaining steps, because a partial report
// would be presented as a complete one.
func runParallel(ctx context.Context, limit int, steps ...func(context.Context) error) error {
	if limit < 1 {
		limit = 1
	}
	stepCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	tokens := make(chan struct{}, limit)
	var wait sync.WaitGroup
	var once sync.Once
	var firstErr error

	for _, step := range steps {
		if step == nil {
			continue
		}
		wait.Add(1)
		go func(run func(context.Context) error) {
			defer wait.Done()
			select {
			case tokens <- struct{}{}:
				defer func() { <-tokens }()
			case <-stepCtx.Done():
				return
			}
			if err := run(stepCtx); err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
			}
		}(step)
	}
	wait.Wait()
	if firstErr != nil {
		return firstErr
	}
	// A cancelled parent must not look like a successful empty report.
	return ctx.Err()
}

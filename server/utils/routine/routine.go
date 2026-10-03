package routine

import (
	"errors"
	"fmt"
	"sync"
)

func Go(parallel int, fns []func()) error {
	if parallel <= 0 {
		return fmt.Errorf("parallel must be positive")
	}
	if len(fns) == 0 {
		return nil
	}

	jobs := make(chan func())
	var workers sync.WaitGroup
	for range min(parallel, len(fns)) {
		workers.Go(func() {
			for fn := range jobs {
				if fn != nil {
					fn()
				}
			}
		})
	}
	for _, fn := range fns {
		jobs <- fn
	}
	close(jobs)
	workers.Wait()
	return nil
}

func GoE(parallel int, fns []func() error) error {
	if parallel <= 0 {
		return fmt.Errorf("parallel must be positive")
	}
	if len(fns) == 0 {
		return nil
	}

	type job struct {
		index int
		fn    func() error
	}
	jobs := make(chan job)
	errs := make([]error, len(fns))
	var workers sync.WaitGroup
	for range min(parallel, len(fns)) {
		workers.Go(func() {
			for current := range jobs {
				if current.fn == nil {
					errs[current.index] = fmt.Errorf("function %d is nil", current.index)
					continue
				}
				errs[current.index] = current.fn()
			}
		})
	}
	for i, fn := range fns {
		jobs <- job{index: i, fn: fn}
	}
	close(jobs)
	workers.Wait()
	return errors.Join(errs...)
}

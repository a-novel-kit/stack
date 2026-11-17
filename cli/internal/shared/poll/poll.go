package poll

import (
	"context"
	"errors"
	"time"
)

// ErrTimeout is the error Until returns once its timeout passes.
var ErrTimeout = errors.New("timed out")

// Until calls probe at once, then every interval, until it reports done or
// fails, ctx ends, or timeout passes. It returns probe's error, ctx's error, or
// ErrTimeout, so a caller words its own timeout. probe runs under a context
// that ends with the wait; a probe the wait cut short reports the wait's end,
// not its own failure.
func Until(ctx context.Context, interval, timeout time.Duration, probe func(ctx context.Context) (bool, error)) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		done, err := probe(waitCtx)
		switch {
		case done && err == nil:
			return nil
		case waitCtx.Err() != nil:
			return ended(ctx)
		case err != nil:
			return err
		}

		select {
		case <-waitCtx.Done():
			return ended(ctx)
		case <-ticker.C:
		}
	}
}

// ended is the error of a wait whose context ended: ctx's own error when the
// caller cancelled it, ErrTimeout otherwise.
func ended(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrTimeout
}

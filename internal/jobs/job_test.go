package jobs

import (
	"context"
	"errors"
	"testing"
)

func TestRunnerReportsProgressAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	collector := new(Collector)
	result := (Runner{}).Start(ctx, func(ctx context.Context, report func(Progress)) error {
		for i := 1; i <= 3; i++ {
			report(Progress{Completed: i, Total: 3})
			if i == 2 {
				cancel()
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		return nil
	}, collector.Report)

	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("result error = %v, want context.Canceled", err)
	}
	if got := collector.Latest().Completed; got != 2 {
		t.Fatalf("latest progress = %d, want 2", got)
	}
}

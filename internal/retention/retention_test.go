package retention

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	counts          []int64
	calls           int
	batchSizes      []int
	cutoffs         []time.Time
	deleteErr       error
	checkpointCalls int
	optimizeCalls   int
	checkpointErr   error
	optimizeErr     error
}

func (f *fakeStore) DeleteResultsBefore(_ context.Context, cutoff time.Time, batchSize int) (int64, error) {
	f.calls++
	f.cutoffs = append(f.cutoffs, cutoff)
	f.batchSizes = append(f.batchSizes, batchSize)
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	if len(f.counts) == 0 {
		return 0, nil
	}
	count := f.counts[0]
	f.counts = f.counts[1:]
	return count, nil
}

func (f *fakeStore) Checkpoint(context.Context) error {
	f.checkpointCalls++
	return f.checkpointErr
}

func (f *fakeStore) Optimize(context.Context) error {
	f.optimizeCalls++
	return f.optimizeErr
}

func TestCutoffPolicies(t *testing.T) {
	now := time.Date(2024, time.March, 31, 8, 9, 10, 11, time.FixedZone("test", 2*60*60))
	forever, err := Cutoff(Policy{Mode: ModeForever}, now)
	if err != nil || forever != nil {
		t.Fatalf("forever cutoff = %v, %v", forever, err)
	}
	days, err := Cutoff(Policy{Mode: ModeDays, Value: 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.UTC().AddDate(0, 0, -30); !days.Equal(want) {
		t.Fatalf("days cutoff = %s, want %s", days, want)
	}
	months, err := Cutoff(Policy{Mode: ModeMonths, Value: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	wantMonth := time.Date(2024, time.February, 29, 6, 9, 10, 11, time.UTC)
	if !months.Equal(wantMonth) {
		t.Fatalf("month cutoff = %s, want %s", months, wantMonth)
	}
	for _, policy := range []Policy{{Mode: ModeDays}, {Mode: ModeMonths}, {Mode: "years", Value: 1}, {Mode: ModeForever, Value: -1}} {
		if _, err := Cutoff(policy, now); err == nil {
			t.Fatalf("Cutoff(%+v) unexpectedly succeeded", policy)
		}
	}
}

func TestCleanupUsesBoundedBatchesAndReportsLimit(t *testing.T) {
	store := &fakeStore{counts: []int64{2, 2, 2}}
	cleaner, err := New(store, nil, Config{BatchSize: 2, MaxBatches: 2})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 5, 10, 0, 0, 0, time.UTC)
	metrics, err := cleaner.Run(context.Background(), Policy{Mode: ModeDays, Value: 7}, now)
	if err != nil {
		t.Fatal(err)
	}
	if store.calls != 2 || metrics.Batches != 2 || metrics.DeletedResults != 4 || !metrics.LimitReached {
		t.Fatalf("unexpected bounded cleanup: calls=%d metrics=%+v", store.calls, metrics)
	}
	for i := range store.batchSizes {
		if store.batchSizes[i] != 2 {
			t.Fatalf("batch %d size = %d", i, store.batchSizes[i])
		}
		wantCutoff := now.AddDate(0, 0, -7)
		if !store.cutoffs[i].Equal(wantCutoff) {
			t.Fatalf("batch %d cutoff = %s, want %s", i, store.cutoffs[i], wantCutoff)
		}
	}
}

func TestCleanupStopsAfterShortBatchAndCanMaintain(t *testing.T) {
	store := &fakeStore{counts: []int64{3, 1}}
	cleaner, err := New(store, nil, Config{BatchSize: 3, MaxBatches: 10, RunMaintenance: true})
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := cleaner.RunBefore(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Mode != ModeManual || metrics.Batches != 2 || metrics.DeletedResults != 4 || metrics.LimitReached ||
		!metrics.MaintenancePerformed || store.checkpointCalls != 1 || store.optimizeCalls != 1 {
		t.Fatalf("unexpected cleanup metrics/store calls: metrics=%+v store=%+v", metrics, store)
	}
}

func TestForeverIsNoOp(t *testing.T) {
	store := &fakeStore{}
	cleaner, err := New(store, nil, Config{})
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := cleaner.Run(context.Background(), Policy{Mode: ModeForever}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if store.calls != 0 || metrics.DeletedResults != 0 || metrics.Cutoff != nil {
		t.Fatalf("forever policy performed work: store=%+v metrics=%+v", store, metrics)
	}
}

func TestCleanupReturnsPartialMetricsOnErrorsAndCancellation(t *testing.T) {
	deleteFailure := errors.New("database busy")
	store := &fakeStore{deleteErr: deleteFailure}
	cleaner, err := New(store, nil, Config{BatchSize: 10, MaxBatches: 2})
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := cleaner.RunBefore(context.Background(), time.Now())
	if !errors.Is(err, deleteFailure) || metrics.Batches != 1 {
		t.Fatalf("delete failure = metrics %+v, error %v", metrics, err)
	}

	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	metrics, err = cleaner.RunBefore(cancelledContext, time.Now())
	if !errors.Is(err, context.Canceled) || metrics.Batches != 0 {
		t.Fatalf("cancelled cleanup = metrics %+v, error %v", metrics, err)
	}
}

func TestMaintenanceErrorsAreJoinedAfterDeletion(t *testing.T) {
	checkpointFailure := errors.New("checkpoint failed")
	optimizeFailure := errors.New("optimize failed")
	store := &fakeStore{counts: []int64{0}, checkpointErr: checkpointFailure, optimizeErr: optimizeFailure}
	cleaner, err := New(store, nil, Config{RunMaintenance: true})
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := cleaner.RunBefore(context.Background(), time.Now())
	if !errors.Is(err, checkpointFailure) || !errors.Is(err, optimizeFailure) || metrics.MaintenancePerformed {
		t.Fatalf("maintenance failure = metrics %+v, error %v", metrics, err)
	}
	if store.checkpointCalls != 1 || store.optimizeCalls != 1 {
		t.Fatalf("maintenance calls: checkpoint=%d optimize=%d", store.checkpointCalls, store.optimizeCalls)
	}
}

func TestNewValidatesBoundsAndMaintenanceCapability(t *testing.T) {
	if _, err := New(nil, nil, Config{}); err == nil {
		t.Fatal("New(nil) unexpectedly succeeded")
	}
	store := &fakeStore{}
	for _, config := range []Config{{BatchSize: -1}, {BatchSize: 5001}, {MaxBatches: -1}, {MaxBatches: 10001}} {
		if _, err := New(store, nil, config); err == nil {
			t.Fatalf("New with config %+v unexpectedly succeeded", config)
		}
	}
}

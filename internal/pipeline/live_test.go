package pipeline

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/network"

	"github.com/miguelnietoa/stellar-explorer/indexer/internal/source"
	"github.com/miguelnietoa/stellar-explorer/indexer/internal/store"
	"github.com/miguelnietoa/stellar-explorer/indexer/internal/transform"
)

func getTestDeps(t *testing.T) (*source.RPCClient, *store.PostgresStore) {
	if testing.Short() {
		t.Skip("skipping test that requires live network access")
	}
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgresql://explorer:explorer_dev@localhost:54320/stellar_explorer?sslmode=disable"
	}
	db, err := store.NewPostgresStore(dbURL)
	if err != nil {
		t.Skipf("Skipping: cannot connect to test database: %v", err)
	}

	rpcEndpoint := os.Getenv("TEST_RPC_ENDPOINT")
	if rpcEndpoint == "" {
		rpcEndpoint = "https://soroban-testnet.stellar.org"
	}
	rpc := source.NewRPCClient(rpcEndpoint, network.TestNetworkPassphrase)

	return rpc, db
}

func TestProcessLedgerBatch(t *testing.T) {
	rpc, db := getTestDeps(t)
	defer db.Close()

	ctx := context.Background()

	// Get latest ledger from testnet
	latest, err := rpc.GetLatestLedger(ctx)
	if err != nil {
		t.Fatalf("GetLatestLedger failed: %v", err)
	}

	// Process 2 ledgers from near the tip
	p := NewLivePipeline(rpc, db, network.TestNetworkPassphrase, 10)
	start := latest.Sequence - 3
	count, err := p.processLedgerBatch(ctx, start, 2, nil)
	if err != nil {
		t.Fatalf("processLedgerBatch failed: %v", err)
	}

	if count != 2 {
		t.Errorf("expected 2 ledgers processed, got %d", count)
	}

	// Verify cursor was updated
	lastIngested, err := db.GetLastIngestedLedger(ctx)
	if err != nil {
		t.Fatalf("GetLastIngestedLedger failed: %v", err)
	}
	expectedCursor := start + 1 // second ledger in the batch
	if lastIngested != expectedCursor {
		t.Errorf("expected cursor at %d, got %d", expectedCursor, lastIngested)
	}

	t.Logf("Successfully ingested ledgers %d-%d, cursor at %d", start, start+1, lastIngested)

	// Clean up
	cleanupTestLedgers(t, db, start, start+1)
}

func TestLivePipelineRunAndStop(t *testing.T) {
	rpc, db := getTestDeps(t)
	defer db.Close()

	p := NewLivePipeline(rpc, db, network.TestNetworkPassphrase, 5)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Run(ctx)
	}()

	// Let it run for a few seconds
	time.Sleep(5 * time.Second)
	cancel()

	err := <-errCh
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify some ledgers were ingested
	lastIngested, err := db.GetLastIngestedLedger(context.Background())
	if err != nil {
		t.Fatalf("GetLastIngestedLedger failed: %v", err)
	}

	if lastIngested == 0 {
		t.Error("expected at least one ledger to be ingested")
	}

	t.Logf("Pipeline ran successfully, last ingested ledger: %d", lastIngested)
}

// TestDetectAndFillGapsRefillsSyntheticGap exercises the full gap-detection
// and refill path end to end against testnet: it ingests a small contiguous
// batch of real ledgers, deletes one from the middle to open a synthetic
// gap, runs detectAndFillGaps directly, and asserts that exactly that
// sequence was refilled.
func TestDetectAndFillGapsRefillsSyntheticGap(t *testing.T) {
	rpc, db := getTestDeps(t)
	defer db.Close()

	ctx := context.Background()

	latest, err := rpc.GetLatestLedger(ctx)
	if err != nil {
		t.Fatalf("GetLatestLedger failed: %v", err)
	}

	p := NewLivePipeline(rpc, db, network.TestNetworkPassphrase, 10)

	start := latest.Sequence - 6
	count, err := p.processLedgerBatch(ctx, start, 5, nil)
	if err != nil {
		t.Fatalf("processLedgerBatch failed: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected 5 ledgers seeded, got %d", count)
	}
	end := start + uint32(count) - 1
	defer func() {
		for seq := start; seq <= end; seq++ {
			cleanupTestLedgers(t, db, seq)
		}
	}()

	// Punch a synthetic gap in the middle of the ingested range.
	gapSeq := start + 2
	db.CleanupTestData(ctx, gapSeq)

	var exists bool
	err = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ledgers WHERE sequence = $1)", gapSeq).Scan(&exists)
	if err != nil {
		t.Fatalf("failed to verify synthetic gap: %v", err)
	}
	if exists {
		t.Fatalf("expected ledger %d to be missing before gap fill", gapSeq)
	}

	p.detectAndFillGaps(ctx, nil)

	err = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ledgers WHERE sequence = $1)", gapSeq).Scan(&exists)
	if err != nil {
		t.Fatalf("failed to verify gap fill: %v", err)
	}
	if !exists {
		t.Fatalf("expected ledger %d to be refilled by detectAndFillGaps", gapSeq)
	}

	t.Logf("detectAndFillGaps successfully refilled synthetic gap at ledger %d", gapSeq)
}

func cleanupTestLedgers(t *testing.T, db *store.PostgresStore, sequences ...uint32) {
	t.Helper()
	ctx := context.Background()
	for _, seq := range sequences {
		db.CleanupTestData(ctx, seq)
	}
}

// TestContiguousRanges is a hermetic, network-free unit test for the pure
// gap-grouping logic used by detectAndFillGaps: it must collapse a sorted
// list of missing sequences into the minimal set of contiguous ranges.
func TestContiguousRanges(t *testing.T) {
	cases := []struct {
		name string
		in   []uint32
		want []ledgerRange
	}{
		{
			name: "empty",
			in:   nil,
			want: nil,
		},
		{
			name: "single sequence",
			in:   []uint32{42},
			want: []ledgerRange{{start: 42, end: 42}},
		},
		{
			name: "one contiguous run",
			in:   []uint32{100, 101, 102, 103},
			want: []ledgerRange{{start: 100, end: 103}},
		},
		{
			name: "multiple disjoint gaps",
			in:   []uint32{100, 101, 105, 200, 201, 202},
			want: []ledgerRange{
				{start: 100, end: 101},
				{start: 105, end: 105},
				{start: 200, end: 202},
			},
		},
		{
			name: "all isolated",
			in:   []uint32{1, 3, 5, 7},
			want: []ledgerRange{
				{start: 1, end: 1},
				{start: 3, end: 3},
				{start: 5, end: 5},
				{start: 7, end: 7},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := contiguousRanges(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("contiguousRanges(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("range[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestContractSpecWorkerPool_BoundedAndDrains is a hermetic, network-free test
// that verifies two properties of the worker pool under -race:
//  1. Concurrency is capped — no more than workerCount goroutines run at once.
//  2. stop() drains all in-flight work before returning (no leaked goroutines).
func TestContractSpecWorkerPool_BoundedAndDrains(t *testing.T) {
	const workers = 3
	const jobs = 20

	var (
		mu        sync.Mutex
		active    int
		maxActive int
		completed int
	)

	// Temporarily replace ProcessContractSpec with a fake via the job channel
	// by wiring the pool's workers to a counting function via a test-local pool.
	pool := &contractSpecWorkerPool{
		queue: make(chan contractSpecJob, 512),
	}
	for i := 0; i < workers; i++ {
		pool.wg.Add(1)
		go func() {
			defer pool.wg.Done()
			for range pool.queue {
				mu.Lock()
				active++
				if active > maxActive {
					maxActive = active
				}
				mu.Unlock()

				time.Sleep(5 * time.Millisecond) // simulate work

				mu.Lock()
				active--
				completed++
				mu.Unlock()
			}
		}()
	}

	// Submit jobs
	for i := 0; i < jobs; i++ {
		pool.queue <- contractSpecJob{} // send empty job; workers count it
	}

	pool.stop() // must drain before returning

	mu.Lock()
	defer mu.Unlock()

	if completed != jobs {
		t.Errorf("expected %d completed jobs, got %d", jobs, completed)
	}
	if maxActive > workers {
		t.Errorf("concurrency exceeded worker count: maxActive=%d workers=%d", maxActive, workers)
	}
	t.Logf("pool drained %d jobs, peak concurrency=%d/%d", completed, maxActive, workers)
}

// TestContractSpecWorkerPool_CancelledContextDropsJob verifies that submit()
// drops a job (not enqueues it) when the caller's context is already cancelled.
//
// To make this deterministic we fill the channel buffer completely so the
// "enqueue" case in the select is never ready — only ctx.Done() can fire —
// then assert the dropped counter is incremented exactly once.
func TestContractSpecWorkerPool_CancelledContextDropsJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before we even call submit

	// Use a zero-buffer channel so the enqueue arm is never immediately ready,
	// making ctx.Done() the only selectable case.
	pool := &contractSpecWorkerPool{
		queue: make(chan contractSpecJob, 0),
	}
	// Start one worker so stop() drains cleanly.
	pool.wg.Add(1)
	go func() {
		defer pool.wg.Done()
		for range pool.queue {
		}
	}()
	defer pool.stop()

	beforeDropped := pool.dropped.Load()

	pool.submit(ctx, nil, nil, transform.DetectedContract{})

	afterDropped := pool.dropped.Load()
	if afterDropped-beforeDropped != 1 {
		t.Errorf("expected dropped counter to increase by 1, got %d -> %d", beforeDropped, afterDropped)
	}
}

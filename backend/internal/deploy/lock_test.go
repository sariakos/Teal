package deploy

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "teal.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestLockAcquireAndDone(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	app, _ := st.Apps.Create(ctx, domain.App{Slug: "x", Name: "X"})
	lock := NewLock(st.DB)

	dep, err := lock.Acquire(ctx, AcquireParams{
		AppID: app.ID, Color: domain.ColorBlue, CommitSHA: "abc",
	})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if dep.Status != domain.DeploymentStatusPending {
		t.Errorf("status = %q", dep.Status)
	}

	// Second acquire while first is pending → ErrLocked.
	if _, err := lock.Acquire(ctx, AcquireParams{
		AppID: app.ID, Color: domain.ColorGreen,
	}); !errors.Is(err, ErrLocked) {
		t.Errorf("second Acquire: want ErrLocked, got %v", err)
	}

	// Mark started; still locked.
	if err := lock.Started(ctx, dep.ID); err != nil {
		t.Fatalf("Started: %v", err)
	}
	if _, err := lock.Acquire(ctx, AcquireParams{AppID: app.ID, Color: domain.ColorGreen}); !errors.Is(err, ErrLocked) {
		t.Errorf("after Started: want ErrLocked, got %v", err)
	}

	// Done releases.
	if err := lock.Done(ctx, dep.ID, domain.DeploymentStatusSucceeded, ""); err != nil {
		t.Fatalf("Done: %v", err)
	}
	if _, err := lock.Acquire(ctx, AcquireParams{AppID: app.ID, Color: domain.ColorGreen}); err != nil {
		t.Errorf("after Done: should acquire, got %v", err)
	}
}

func TestLockIsolatesAppsFromEachOther(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	a, _ := st.Apps.Create(ctx, domain.App{Slug: "a", Name: "A"})
	b, _ := st.Apps.Create(ctx, domain.App{Slug: "b", Name: "B"})
	lock := NewLock(st.DB)

	if _, err := lock.Acquire(ctx, AcquireParams{AppID: a.ID, Color: domain.ColorBlue}); err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Acquire(ctx, AcquireParams{AppID: b.ID, Color: domain.ColorBlue}); err != nil {
		t.Errorf("different apps must not block each other: %v", err)
	}
}

func TestLockConcurrentAcquireExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	app, _ := st.Apps.Create(ctx, domain.App{Slug: "x", Name: "X"})
	lock := NewLock(st.DB)

	const N = 10
	var wg sync.WaitGroup
	winners := make([]bool, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := lock.Acquire(ctx, AcquireParams{
				AppID: app.ID, Color: domain.ColorBlue,
			})
			winners[i] = err == nil
		}(i)
	}
	wg.Wait()

	count := 0
	for _, w := range winners {
		if w {
			count++
		}
	}
	if count != 1 {
		t.Errorf("exactly 1 winner expected, got %d", count)
	}
}

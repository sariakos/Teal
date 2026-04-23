package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sariakos/teal/backend/internal/domain"
)

func TestNotificationsCRUD(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.Users.Create(ctx, domain.User{Email: "u@x.com", PasswordHash: []byte("h"), Role: domain.UserRoleAdmin})
	if err != nil {
		t.Fatal(err)
	}

	// Insert one targeted + one broadcast.
	_, err = st.Notifications.Insert(ctx, domain.Notification{
		UserID: &user.ID, Level: domain.NotificationInfo, Kind: domain.NotificationKindDeploySucceeded,
		Title: "OK", AppSlug: "a",
	})
	if err != nil {
		t.Fatalf("insert targeted: %v", err)
	}
	_, err = st.Notifications.Insert(ctx, domain.Notification{
		Level: domain.NotificationError, Kind: domain.NotificationKindDeployFailed,
		Title: "Broadcast",
	})
	if err != nil {
		t.Fatalf("insert broadcast: %v", err)
	}

	// includeBroadcasts=true sees both.
	rows, err := st.Notifications.ListForUser(ctx, user.ID, true, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("list with broadcasts: %d", len(rows))
	}

	// includeBroadcasts=false sees only targeted.
	rows, _ = st.Notifications.ListForUser(ctx, user.ID, false, 0)
	if len(rows) != 1 {
		t.Errorf("list without broadcasts: %d", len(rows))
	}

	// Unread count.
	n, err := st.Notifications.CountUnreadForUser(ctx, user.ID, true)
	if err != nil || n != 2 {
		t.Errorf("unread = %d %v", n, err)
	}

	// Mark first row read.
	rows, _ = st.Notifications.ListForUser(ctx, user.ID, true, 0)
	target := rows[1].ID // oldest first → second-to-last in DESC order
	if err := st.Notifications.MarkRead(ctx, target, user.ID, true); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if err := st.Notifications.MarkRead(ctx, target, user.ID, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("second mark read: want ErrNotFound, got %v", err)
	}

	// Mark all → unread count 0.
	if err := st.Notifications.MarkAllReadForUser(ctx, user.ID, true); err != nil {
		t.Fatal(err)
	}
	n, _ = st.Notifications.CountUnreadForUser(ctx, user.ID, true)
	if n != 0 {
		t.Errorf("after mark all: %d", n)
	}

	// Prune everything in the past — wipes both rows.
	if _, err := st.Notifications.Prune(ctx, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.Notifications.ListForUser(ctx, user.ID, true, 0)
	if len(rows) != 0 {
		t.Errorf("after prune: %d", len(rows))
	}
}

func TestNotificationsCascadeOnUserDelete(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	user, _ := st.Users.Create(ctx, domain.User{Email: "u@x.com", PasswordHash: []byte("h"), Role: domain.UserRoleAdmin})
	_, _ = st.Notifications.Insert(ctx, domain.Notification{
		UserID: &user.ID, Level: domain.NotificationInfo, Kind: "x", Title: "t",
	})
	if err := st.Users.Delete(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.Notifications.ListForUser(ctx, user.ID, false, 0)
	if len(rows) != 0 {
		t.Errorf("notifications survived user delete: %d", len(rows))
	}
}

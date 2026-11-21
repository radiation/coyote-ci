package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func TestNotificationPlanMarkerFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT notification_plan_complete").WillReturnRows(sqlmock.NewRows([]string{"complete"}).AddRow(false))
	mock.ExpectExec("INSERT INTO notification_deliveries").WillReturnResult(sqlmock.NewResult(0, 1))
	failure := errors.New("marker update failed")
	mock.ExpectExec("UPDATE builds").WillReturnError(failure)
	mock.ExpectRollback()
	row := domain.NotificationDelivery{BuildID: "one", EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: "target", Status: domain.NotificationDeliveryStatusPending, MaxAttempts: 3}
	if won, planErr := NewNotificationDeliveryRepository(db).CommitNotificationPlan(context.Background(), row.BuildID, domain.BuildStatusFailed, []domain.NotificationDelivery{row}); won || !errors.Is(planErr, failure) {
		t.Fatalf("marker failure: %v %v", won, planErr)
	}
	if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
		t.Fatal(expectationErr)
	}
}

func TestNotificationPlanRestartPostgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	t.Cleanup(func() { closeNotificationIntegrationDB(t, db) })
	ctx := context.Background()
	ledger := NewNotificationDeliveryRepository(db)
	sender := &countingEmailSender{}
	drain := func(enabled bool) buildsvc.NotificationRecoveryIterationResult {
		notifier, err := buildsvc.NewBuildNotificationService(buildsvc.BuildNotificationConfig{Enabled: enabled, Recipients: "restart@example.com", Sender: sender, BuildRepo: NewBuildRepository(db), DeliveryRepo: ledger, PlanningRepo: ledger, SubscriptionRepo: NewNotificationSubscriptionRepository(db)})
		if err != nil {
			t.Fatal(err)
		}
		server, err := buildsvc.NewNotificationRecoveryDrain(buildsvc.NotificationRecoveryDrainConfig{Notifier: notifier, Interval: time.Second, BatchSize: 10})
		if err != nil {
			t.Fatal(err)
		}
		result, err := server.RunIteration(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	terminal := func(status domain.BuildStatus) string {
		id := createNotificationIntegrationBuild(t, db, ctx)
		if _, err := db.Exec(`UPDATE builds SET status=$2, finished_at='2000-01-01' WHERE id=$1`, id, status); err != nil {
			t.Fatal(err)
		}
		return id
	}
	suppressed := terminal(domain.BuildStatusSuccess)
	if result := drain(false); result.Sent != 0 {
		t.Fatal(result)
	}
	failed := terminal(domain.BuildStatusFailed)
	if result := drain(true); result.Sent != 1 || result.ClaimAcquired != 1 || result.RetryClaimed != 0 {
		t.Fatal(result)
	}
	if result := drain(true); result.Scanned != 0 {
		t.Fatal(result)
	}
	var complete bool
	var count int
	if err := db.QueryRow(`SELECT notification_plan_complete,(SELECT count(*) FROM notification_deliveries WHERE build_id=$1) FROM builds WHERE id=$1`, suppressed).Scan(&complete, &count); err != nil || !complete || count != 0 {
		t.Fatalf("disabled plan: %v %d %v", complete, count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM notification_deliveries WHERE build_id=$1 AND status='sent' AND attempts=1`, failed).Scan(&count); err != nil || count != 1 {
		t.Fatalf("recovered plan: %d %v", count, err)
	}
}

func TestNotificationPlanAtomicityPostgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	t.Cleanup(func() { closeNotificationIntegrationDB(t, db) })
	ctx := context.Background()
	id := createNotificationIntegrationBuild(t, db, ctx)
	ledger := NewNotificationDeliveryRepository(db)
	status := domain.BuildStatusFailed
	commit := func(keys ...string) (bool, error) {
		var rows []domain.NotificationDelivery
		for _, key := range keys {
			row := domain.NotificationDelivery{BuildID: id, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: key, Recipient: key, MaxAttempts: 3, Status: domain.NotificationDeliveryStatusPending}
			if key == "invalid-fk" {
				missing := uuid.NewString()
				row.NotificationTargetID = &missing
			}
			rows = append(rows, row)
		}
		return ledger.CommitNotificationPlan(ctx, id, status, rows)
	}
	owed := func() bool {
		ids, err := ledger.ListUnplannedBuildIDs(ctx, "", 1000)
		if err != nil {
			t.Fatal(err)
		}
		return slices.Contains(ids, id)
	}
	if won, err := commit(); err != nil || won || owed() {
		t.Fatalf("pending build planned: %v %v", won, err)
	}
	if _, err := db.Exec(`UPDATE builds SET status='failed' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if won, err := commit("first", "invalid-fk"); err == nil || won || !owed() {
		t.Fatalf("partial plan: %v %v", won, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM notification_deliveries WHERE build_id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("surviving partial rows: %d, %v", count, err)
	}
	type result struct {
		key string
		won bool
		err error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, key := range []string{"left", "right"} {
		go func() { <-start; won, err := commit(key, key+"2"); results <- result{key, won, err} }()
	}
	close(start)
	winner := ""
	for range 2 {
		received := <-results
		if received.err != nil {
			t.Fatal(received.err)
		}
		if received.won {
			if winner != "" {
				t.Fatal("two winners")
			}
			winner = received.key
		}
	}
	if winner == "" || owed() {
		t.Fatal("no completed winning plan")
	}
	if err := db.QueryRow(`SELECT count(*) FROM notification_deliveries WHERE build_id=$1 AND (destination_key NOT IN ($2,$3) OR status!='pending' OR attempts!=0)`, id, winner, winner+"2").Scan(&count); err != nil || count != 0 {
		t.Fatalf("mixed plan: %d %v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM notification_deliveries WHERE build_id=$1`, id).Scan(&count); err != nil || count != 2 {
		t.Fatalf("missing rows: %d %v", count, err)
	}
}

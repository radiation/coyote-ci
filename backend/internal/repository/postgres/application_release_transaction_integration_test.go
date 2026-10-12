package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestApplicationReleaseRepository_FinalizationAndPromotionTransactions_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	repo := NewApplicationReleaseRepository(db)

	t.Run("finalization moves latest once and is repeatable", func(t *testing.T) {
		release := createApplicationReleaseIntegrationFixture(t, db, "latest")
		reconciled, reconcileErr := repo.Reconcile(ctx, release.ID)
		if reconcileErr != nil {
			t.Fatalf("reconcile release: %v", reconcileErr)
		}
		if reconciled.Status != domain.ApplicationReleaseStatusComplete {
			t.Fatalf("release status=%q, want complete", reconciled.Status)
		}
		assertApplicationChannelState(t, db, release, "latest", 1)

		repeated, repeatedErr := repo.Reconcile(ctx, release.ID)
		if repeatedErr != nil {
			t.Fatalf("repeat reconcile release: %v", repeatedErr)
		}
		if repeated.Status != domain.ApplicationReleaseStatusComplete {
			t.Fatalf("repeated release status=%q, want complete", repeated.Status)
		}
		assertApplicationChannelState(t, db, release, "latest", 1)
	})

	t.Run("finalization rollback leaves no channel movement", func(t *testing.T) {
		release := createApplicationReleaseIntegrationFixture(t, db, "latest")
		functionName := "fail_application_release_event_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
		if _, createErr := db.ExecContext(ctx, fmt.Sprintf(`
			CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN RAISE EXCEPTION 'injected channel event failure'; END;
			$$
		`, functionName)); createErr != nil {
			t.Fatalf("create failure function: %v", createErr)
		}
		if _, createErr := db.ExecContext(ctx, fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON application_channel_events FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, functionName)); createErr != nil {
			t.Fatalf("create failure trigger: %v", createErr)
		}
		t.Cleanup(func() {
			_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON application_channel_events", functionName))
			_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
		})

		if _, reconcileErr := repo.Reconcile(ctx, release.ID); reconcileErr == nil {
			t.Fatal("expected injected finalization failure")
		}
		var status string
		if statusErr := db.QueryRowContext(ctx, `SELECT status FROM application_releases WHERE id=$1`, release.ID).Scan(&status); statusErr != nil {
			t.Fatalf("load release status: %v", statusErr)
		}
		if status != string(domain.ApplicationReleaseStatusPending) {
			t.Fatalf("release status=%q, want pending", status)
		}
		assertApplicationChannelState(t, db, release, "latest", 0)
	})

	t.Run("concurrent finalization and promotion produce one event", func(t *testing.T) {
		release := createApplicationReleaseIntegrationFixture(t, db, "latest")
		runConcurrentReleaseOperation(t, 2, func() error {
			_, reconcileErr := repo.Reconcile(context.Background(), release.ID)
			return reconcileErr
		})
		assertApplicationChannelState(t, db, release, "latest", 1)

		runConcurrentReleaseOperation(t, 3, func() error {
			_, promoteErr := repo.PromoteStable(context.Background(), release.ID, nil)
			return promoteErr
		})
		assertApplicationChannelState(t, db, release, "stable", 1)
	})
}

func createApplicationReleaseIntegrationFixture(t *testing.T, db *sql.DB, autoChannel string) domain.ApplicationRelease {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	projectID, jobID, buildID, releaseID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, projectErr := db.ExecContext(ctx, `INSERT INTO projects (id, name, slug, description, created_at, updated_at) VALUES ($1,$2,$3,NULL,$4,$4)`, projectID, "release project "+projectID, "release-project-"+uuid.NewString(), now); projectErr != nil {
		t.Fatalf("insert project: %v", projectErr)
	}
	if _, jobErr := db.ExecContext(ctx, `INSERT INTO jobs (id, project_id, name, priority, repository_url, push_enabled, trigger_mode, branch_allowlist, tag_allowlist, enabled, created_at, updated_at) VALUES ($1,$2,$3,5,'https://example.invalid/repo.git',FALSE,'branches','[]'::jsonb,'[]'::jsonb,TRUE,$4,$4)`, jobID, projectID, "release-job-"+jobID, now); jobErr != nil {
		t.Fatalf("insert job: %v", jobErr)
	}
	if _, buildErr := db.ExecContext(ctx, `INSERT INTO builds (id, build_number, project_id, job_id, priority, status, created_at, current_step_index, attempt_number, trigger_kind, image_source_kind) VALUES ($1,1,$2,$3,5,'success',$4,0,1,'manual','external')`, buildID, projectID, jobID, now); buildErr != nil {
		t.Fatalf("insert build: %v", buildErr)
	}
	if _, releaseErr := db.ExecContext(ctx, `INSERT INTO application_releases (id, build_id, project_id, job_id, application_version, status, auto_channel_name, created_at, updated_at) VALUES ($1,$2,$3,$4,'1.2.3','pending',$5,$6,$6)`, releaseID, buildID, projectID, jobID, autoChannel, now); releaseErr != nil {
		t.Fatalf("insert application release: %v", releaseErr)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM application_channel_events WHERE channel_id IN (SELECT id FROM application_channels WHERE project_id=$1)`, projectID)
		_, _ = db.ExecContext(ctx, `DELETE FROM application_channels WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(ctx, `DELETE FROM application_releases WHERE id=$1`, releaseID)
		_, _ = db.ExecContext(ctx, `DELETE FROM builds WHERE id=$1`, buildID)
		_, _ = db.ExecContext(ctx, `DELETE FROM jobs WHERE id=$1`, jobID)
		_, _ = db.ExecContext(ctx, `DELETE FROM projects WHERE id=$1`, projectID)
	})
	return domain.ApplicationRelease{ID: releaseID, ProjectID: projectID, JobID: &jobID}
}

func assertApplicationChannelState(t *testing.T, db *sql.DB, release domain.ApplicationRelease, channelName string, wantEvents int) {
	t.Helper()
	var currentReleaseID string
	var events int
	queryErr := db.QueryRowContext(context.Background(), `SELECT c.current_release_id, COUNT(e.id) FROM application_channels c LEFT JOIN application_channel_events e ON e.channel_id=c.id WHERE c.project_id=$1 AND c.job_id=$2 AND c.channel_name=$3 GROUP BY c.current_release_id`, release.ProjectID, *release.JobID, channelName).Scan(&currentReleaseID, &events)
	if wantEvents == 0 {
		if queryErr == nil {
			t.Fatalf("unexpected %s channel current=%s events=%d", channelName, currentReleaseID, events)
		}
		return
	}
	if queryErr != nil {
		t.Fatalf("load %s channel: %v", channelName, queryErr)
	}
	if currentReleaseID != release.ID || events != wantEvents {
		t.Fatalf("%s channel current=%s events=%d, want current=%s events=%d", channelName, currentReleaseID, events, release.ID, wantEvents)
	}
}

func runConcurrentReleaseOperation(t *testing.T, count int, operation func() error) {
	t.Helper()
	start := make(chan struct{})
	errors := make(chan error, count)
	var waitGroup sync.WaitGroup
	for index := 0; index < count; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			errors <- operation()
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errors)
	for operationErr := range errors {
		if operationErr != nil {
			t.Fatalf("concurrent release operation: %v", operationErr)
		}
	}
}

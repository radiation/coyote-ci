package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestArtifactBuildTransaction(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	t.Cleanup(func() { closeNotificationIntegrationDB(t, db) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	producer := createNotificationIntegrationBuild(t, db, context.Background())
	var project, upstream string
	if err := db.QueryRowContext(ctx, `SELECT project_id,job_id FROM builds WHERE id=$1`, producer).Scan(&project, &upstream); err != nil {
		t.Fatal(err)
	}
	consumer, artifact := uuid.NewString(), uuid.NewString()
	pkg := md5UUID(upstream + "::out")
	now := time.Now().UTC().Truncate(time.Microsecond)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO jobs (id,project_id,name,repository_url,enabled,created_at,updated_at) VALUES ($1,$2,'consumer','https://example.invalid',true,$3,$3)`, consumer, project, now)
	second := uuid.NewString()
	exec(`INSERT INTO jobs (id,project_id,name,repository_url,created_at,updated_at) VALUES ($1,$2,'second','https://example.invalid',$3,$3)`, second, project, now)
	t.Cleanup(func() {
		cleanup := func(query, id string) {
			t.Helper()
			if _, cleanupErr := db.Exec(query, id); cleanupErr != nil {
				t.Errorf("fixture cleanup %q: %v", query, cleanupErr)
			}
		}
		cleanup(`DELETE FROM builds WHERE job_id=$1`, consumer)
		cleanup(`DELETE FROM jobs WHERE id=$1`, consumer)
		cleanup(`DELETE FROM jobs WHERE id=$1`, second)
		cleanup(`DELETE FROM build_artifacts WHERE id=$1`, artifact)
		cleanup(`DELETE FROM artifact_packages WHERE id=$1`, pkg)
	})
	exec(`UPDATE jobs SET artifact_triggers=jsonb_build_array(jsonb_build_object('ProducerJobID',$2::text,'Path','out')) WHERE project_id=$1`, project, upstream)
	publisher := NewArtifactRepository(db)
	publication := domain.BuildArtifact{ID: artifact, BuildID: producer, LogicalPath: "out", StorageKey: "out", SizeBytes: 1, CreatedAt: now}
	constraint := "test_publication_" + fmt.Sprintf("%x", uuid.New())
	exec(`ALTER TABLE artifact_trigger_deliveries ADD CONSTRAINT ` + constraint + ` CHECK(consumer_job_id <> '` + consumer + `') NOT VALID`)
	_, rollbackErr := publisher.Create(ctx, publication)
	exec(`ALTER TABLE artifact_trigger_deliveries DROP CONSTRAINT ` + constraint)
	if rollbackErr == nil {
		t.Fatal("expected publication rollback")
	}
	var residue int
	if scanErr := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM artifact_packages WHERE id=$1)+(SELECT count(*) FROM build_artifacts WHERE build_id=$2)+(SELECT count(*) FROM artifact_trigger_deliveries WHERE producer_build_id=$2)`, pkg, producer).Scan(&residue); scanErr != nil || residue != 0 {
		t.Fatalf("publication not atomic: %d rows, %v", residue, scanErr)
	}
	results := make(chan error, 2)
	for range 2 {
		attempt := publication
		attempt.ID = uuid.NewString()
		go func() { _, publishErr := publisher.Create(ctx, attempt); results <- publishErr }()
	}
	firstErr, secondErr := <-results, <-results
	if secondErr == nil {
		firstErr, secondErr = secondErr, firstErr
	}
	if firstErr != nil || !errors.Is(secondErr, repository.ErrArtifactConflict) {
		t.Fatalf("concurrent publication: %v, %v", firstErr, secondErr)
	}
	rows, readErr := publisher.ListByBuildID(ctx, producer)
	if readErr != nil || len(rows) != 1 {
		t.Fatalf("committed artifacts: %+v %v", rows, readErr)
	}
	artifact = rows[0].ID
	restartedDB := openNotificationIntegrationDB(t)
	t.Cleanup(func() { closeNotificationIntegrationDB(t, restartedDB) })
	store := NewArtifactTriggerDeliveryRepository(restartedDB)
	obligations, listErr := store.ListByProducerBuildID(ctx, producer)
	if listErr != nil || len(obligations) != 2 {
		t.Fatalf("fan-out: %+v, %v", obligations, listErr)
	}
	exec(`UPDATE jobs SET enabled=false,artifact_triggers='[]' WHERE project_id=$1`, project)
	page := func(after string) []domain.ArtifactTriggerDelivery {
		t.Helper()
		items, pageErr := store.ListPendingAfter(ctx, after, 1)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		return items
	}
	if items := page(""); len(items) != 0 {
		t.Fatalf("early publication eligible for recovery: %+v", items)
	}
	completion, beginErr := db.BeginTx(ctx, nil)
	if beginErr != nil {
		t.Fatal(beginErr)
	}
	defer func() { _ = completion.Rollback() }()
	if _, updateErr := completion.ExecContext(ctx, `UPDATE builds SET status='success' WHERE id=$1`, producer); updateErr != nil {
		t.Fatal(updateErr)
	}
	if items := page(""); len(items) != 0 {
		t.Fatalf("observed uncommitted completion: %+v", items)
	}
	if commitErr := completion.Commit(); commitErr != nil {
		t.Fatal(commitErr)
	}
	for _, status := range []domain.BuildStatus{domain.BuildStatusSuccess, domain.BuildStatusFailed, domain.BuildStatusCanceled} {
		exec(`UPDATE builds SET status=$2 WHERE id=$1`, producer, status)
		first := page("")
		if len(first) != 1 || first[0].ID != obligations[0].ID && first[0].ID != obligations[1].ID {
			t.Fatalf("first page for %s: %+v", status, first)
		}
		next := page(first[0].ID)
		if len(next) != 1 || next[0].ID <= first[0].ID || len(page(next[0].ID)) != 0 {
			t.Fatalf("second page for %s: %+v", status, next)
		}
	}
	exec(`UPDATE artifact_trigger_deliveries SET atomic_creation=false WHERE producer_build_id=$1`, producer)
	if items := page(""); len(items) != 0 {
		t.Fatalf("legacy pending replayed: %+v", items)
	}
	delivery, err := store.GetByArtifactIDAndConsumerJobID(ctx, artifact, consumer)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE artifact_trigger_deliveries SET atomic_creation=false,status='failed' WHERE id=$1`, delivery.ID)
	if _, claimErr := store.ClaimFailedForRetry(ctx, delivery.ID, now); !errors.Is(claimErr, repository.ErrArtifactTriggerDeliveryRetryNotClaimable) {
		t.Fatalf("legacy delivery was claimable: %v", claimErr)
	}
	exec(`UPDATE artifact_trigger_deliveries SET atomic_creation=true WHERE id=$1`, delivery.ID)
	delivery, err = store.ClaimFailedForRetry(ctx, delivery.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	planForAttempt := func() repository.ArtifactBuildPlan {
		build := domain.Build{ID: uuid.NewString(), ProjectID: project, JobID: &consumer, CreatedAt: now, Trigger: domain.BuildTrigger{Kind: domain.BuildTriggerKindArtifact, ArtifactID: &artifact, ArtifactPath: &delivery.ArtifactPath, ProducerBuildID: &producer, ProducerProjectID: &project, ProducerJobID: &upstream}}
		plan := repository.ArtifactBuildPlan{Build: build, ConsumerUpdatedAt: now}
		for index, node := range []string{"compile", "package"} {
			step := domain.BuildStep{ID: uuid.NewString(), BuildID: build.ID, StepIndex: index, Name: node, NodeID: node, Command: "true", Status: domain.BuildStepStatusPending}
			job := domain.ExecutionJob{ID: uuid.NewString(), BuildID: build.ID, StepID: step.ID, StepIndex: index, Name: node, NodeID: node, Command: []string{"true"}, Status: domain.ExecutionJobStatusQueued, CreatedAt: now}
			if index == 1 {
				step.DependsOnNodes = []string{"compile"}
				job.DependsOnNodeIDs = step.DependsOnNodes
			}
			plan.Steps = append(plan.Steps, step)
			plan.Jobs = append(plan.Jobs, job)
		}
		plan.Outputs = []domain.ExecutionJobOutput{{ID: uuid.NewString(), JobID: plan.Jobs[1].ID, BuildID: build.ID, Name: "out", Kind: "artifact", DeclaredPath: "out", Status: domain.ExecutionJobOutputStatusDeclared, CreatedAt: now}}
		return plan
	}
	assertCounts := func(t *testing.T, builds, steps, jobs, outputs, next int) {
		t.Helper()
		var got [5]int
		query := `SELECT (SELECT count(*) FROM builds WHERE job_id=$1),(SELECT count(*) FROM build_steps s JOIN builds b ON b.id=s.build_id WHERE b.job_id=$1),(SELECT count(*) FROM build_jobs j JOIN builds b ON b.id=j.build_id WHERE b.job_id=$1),(SELECT count(*) FROM build_job_outputs o JOIN builds b ON b.id=o.build_id WHERE b.job_id=$1),next_build_number FROM jobs WHERE id=$1`
		if scanErr := db.QueryRowContext(ctx, query, consumer).Scan(&got[0], &got[1], &got[2], &got[3], &got[4]); scanErr != nil {
			t.Fatal(scanErr)
		}
		if got != [5]int{builds, steps, jobs, outputs, next} {
			t.Fatalf("unexpected graph counts and allocator: %v", got)
		}
	}
	for _, stage := range []string{"consumer", "step", "job", "output", "link"} {
		t.Run(stage, func(t *testing.T) {
			plan := planForAttempt()
			switch stage {
			case "consumer":
				plan.ConsumerUpdatedAt = now.Add(-time.Hour)
			case "step":
				plan.Steps[1].ID = plan.Steps[0].ID
			case "job":
				plan.Jobs[1].ID = plan.Jobs[0].ID
			case "output":
				plan.Outputs[0].JobID = uuid.NewString()
			case "link":
				constraint := "test_link_" + fmt.Sprintf("%x", uuid.New())
				exec(`ALTER TABLE artifact_trigger_deliveries ADD CONSTRAINT ` + constraint + ` CHECK(id <> '` + delivery.ID + `' OR status <> 'queued') NOT VALID`)
				defer exec(`ALTER TABLE artifact_trigger_deliveries DROP CONSTRAINT ` + constraint)
			}
			if _, createErr := store.CreateArtifactBuild(ctx, delivery.ID, delivery.UpdatedAt, plan); createErr == nil {
				t.Fatal("expected rollback")
			}
			assertCounts(t, 0, 0, 0, 0, 1)
			current, getErr := store.GetByID(ctx, delivery.ID)
			if getErr != nil || current.Status != domain.ArtifactTriggerDeliveryStatusPending || current.QueuedBuildID != nil {
				t.Fatalf("delivery changed: %+v %v", current, getErr)
			}
		})
	}
	first := planForAttempt()
	stale := delivery.UpdatedAt
	failed, failErr := store.FailPending(ctx, delivery.ID, stale, "failed preparation")
	if failErr != nil || failed.Status != domain.ArtifactTriggerDeliveryStatusFailed {
		t.Fatalf("expected failure: %+v %v", failed, failErr)
	}
	delivery, err = store.ClaimFailedForRetry(ctx, delivery.ID, stale)
	if err != nil || !delivery.UpdatedAt.After(stale) {
		t.Fatalf("expected a fresh claim: %+v %v", delivery, err)
	}
	if _, staleErr := store.CreateArtifactBuild(ctx, delivery.ID, stale, first); !errors.Is(staleErr, repository.ErrArtifactTriggerDeliveryRetryNotClaimable) {
		t.Fatalf("stale creation: %v", staleErr)
	}
	if current, staleErr := store.FailPending(ctx, delivery.ID, stale, "stale failure"); staleErr != nil || current.Status != domain.ArtifactTriggerDeliveryStatusPending {
		t.Fatalf("stale failure changed claim: %+v %v", current, staleErr)
	}
	locked := make(chan int, 1)
	release := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		_, createErr := store.createArtifactBuild(ctx, delivery.ID, delivery.UpdatedAt, first, func(tx *sql.Tx) error {
			var pid int
			if pidErr := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); pidErr != nil {
				return pidErr
			}
			locked <- pid
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			if commitErr := tx.Commit(); commitErr != nil {
				return commitErr
			}
			return errors.New("acknowledgment lost")
		})
		firstResult <- createErr
	}()
	var pid int
	select {
	case pid = <-locked:
	case early := <-firstResult:
		t.Fatalf("failed before commit: %v", early)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	otherDB := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, otherDB)
	other := NewArtifactTriggerDeliveryRepository(otherDB)
	secondResult := make(chan error, 1)
	go func() {
		existing, createErr := other.CreateArtifactBuild(ctx, delivery.ID, delivery.UpdatedAt, planForAttempt())
		if createErr == nil && existing.ID != first.Build.ID {
			createErr = fmt.Errorf("duplicate build %s", existing.ID)
		}
		secondResult <- createErr
	}()
	for {
		var blocked bool
		if scanErr := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&blocked); scanErr != nil {
			t.Fatal(scanErr)
		}
		if blocked {
			break
		}
	}
	assertCounts(t, 0, 0, 0, 0, 1)
	close(release)
	if commitErr := <-firstResult; !errors.Is(commitErr, repository.ErrArtifactBuildCommitUnknown) {
		t.Fatalf("expected unknown commit: %v", commitErr)
	}
	if secondErr := <-secondResult; secondErr != nil {
		t.Fatal(secondErr)
	}
	freshDB := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, freshDB)
	fresh := NewArtifactTriggerDeliveryRepository(freshDB)
	linked, getErr := fresh.CreateArtifactBuild(ctx, delivery.ID, delivery.UpdatedAt, planForAttempt())
	if getErr != nil || linked.ID != first.Build.ID {
		t.Fatalf("recover linked build: %+v %v", linked, getErr)
	}
	current, failErr := fresh.FailPending(ctx, delivery.ID, delivery.UpdatedAt, "late failure")
	if failErr != nil || current.QueuedBuildID == nil || *current.QueuedBuildID != linked.ID {
		t.Fatalf("late failure overwrote linkage: %+v %v", current, failErr)
	}
	assertCounts(t, 1, 2, 2, 1, 2)
	jobs, listErr := NewExecutionJobRepository(db).GetJobsByBuildID(ctx, linked.ID)
	if listErr != nil || len(jobs) != 2 || len(jobs[1].DependsOnNodeIDs) != 1 || jobs[1].DependsOnNodeIDs[0] != "compile" {
		t.Fatalf("missing job dependency: %+v %v", jobs, listErr)
	}
}

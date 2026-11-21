package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func TestBuildCompletionConcurrentSiblings_Postgres(t *testing.T) {
	for _, modes := range []struct {
		name   string
		atomic [2]bool
	}{
		{"atomic", [2]bool{true, true}},
		{"legacy", [2]bool{false, false}},
		{"mixed", [2]bool{true, false}},
	} {
		for _, statuses := range [][2]domain.BuildStepStatus{
			{domain.BuildStepStatusFailed, domain.BuildStepStatusFailed},
			{domain.BuildStepStatusSuccess, domain.BuildStepStatusFailed},
			{domain.BuildStepStatusSuccess, domain.BuildStepStatusSuccess},
		} {
			t.Run(fmt.Sprintf("%s/%s-%s", modes.name, statuses[0], statuses[1]), func(t *testing.T) {
				fixture := newBuildCompletionFixture(t, 2)
				gate, blocker := fixture.lockBuild(t)
				results := make(chan buildCompletionCall, 2)
				for index := range statuses {
					go func() {
						results <- fixture.complete(index, statuses[index], modes.atomic[index], fixture.tokens[index])
					}()
				}
				fixture.waitBuildWaiters(t, blocker, 2)
				if releaseErr := gate.Commit(); releaseErr != nil {
					t.Fatal(releaseErr)
				}
				successOwners := 0
				for range statuses {
					result := <-results
					if result.err != nil || result.outcome != repository.StepCompletionCompleted {
						t.Fatalf("completion outcome=%s err=%v", result.outcome, result.err)
					}
					if result.step.BuildBecameSuccessful {
						successOwners++
					}
				}
				wantBuild := domain.BuildStatusFailed
				wantOwners := 0
				if statuses[0] == domain.BuildStepStatusSuccess && statuses[1] == domain.BuildStepStatusSuccess {
					wantBuild = domain.BuildStatusSuccess
					wantOwners = 1
				}
				fixture.assertStates(t, wantBuild, statuses[:])
				if successOwners != wantOwners {
					t.Fatalf("success transition owners=%d, want %d", successOwners, wantOwners)
				}
				for index, atomic := range modes.atomic {
					if atomic {
						job, lookupErr := NewExecutionJobRepository(fixture.db).GetJobByID(fixture.ctx, fixture.jobs[index])
						if lookupErr != nil || string(job.Status) != string(statuses[index]) || job.ClaimToken != nil {
							t.Fatalf("job %d status=%s token=%v err=%v", index, job.Status, job.ClaimToken, lookupErr)
						}
					}
				}
			})
		}
	}
}

func TestBuildCompletionCancellationRace_Postgres(t *testing.T) {
	for _, atomic := range []bool{true, false} {
		for _, status := range []domain.BuildStepStatus{domain.BuildStepStatusSuccess, domain.BuildStepStatusFailed} {
			for _, cancelFirst := range []bool{true, false} {
				for _, siblings := range []int{1, 2} {
					t.Run(fmt.Sprintf("atomic=%t/%s/cancel-first=%t/steps=%d", atomic, status, cancelFirst, siblings), func(t *testing.T) {
						fixture := newBuildCompletionFixture(t, siblings)
						gate, blocker := fixture.lockBuild(t)
						completed := make(chan buildCompletionCall, 1)
						canceled := make(chan error, 1)
						complete := func() { completed <- fixture.complete(0, status, atomic, fixture.tokens[0]) }
						cancel := func() {
							_, _, cancelErr := NewBuildRepository(fixture.db).CancelBuild(fixture.ctx, fixture.buildID, "operator canceled", time.Now().UTC())
							canceled <- cancelErr
						}
						first, second := complete, cancel
						if cancelFirst {
							first, second = cancel, complete
						}
						go first()
						fixture.waitBuildWaiters(t, blocker, 1)
						go second()
						fixture.waitBuildWaiters(t, blocker, 2)
						if releaseErr := gate.Commit(); releaseErr != nil {
							t.Fatal(releaseErr)
						}
						result, cancelErr := <-completed, <-canceled
						wantOutcome := repository.StepCompletionCompleted
						wantBuild := domain.BuildStatusCanceled
						wantSteps := make([]domain.BuildStepStatus, siblings)
						for index := range wantSteps {
							wantSteps[index] = domain.BuildStepStatusCanceled
						}
						if cancelFirst {
							wantOutcome = repository.StepCompletionDuplicateTerminal
						} else {
							wantSteps[0] = status
							if siblings == 1 {
								wantBuild = domain.BuildStatus(status)
								if !errors.Is(cancelErr, repository.ErrInvalidBuildStatusTransition) {
									t.Fatalf("cancel terminal build: %v", cancelErr)
								}
								cancelErr = nil
							}
						}
						if result.err != nil || result.outcome != wantOutcome || cancelErr != nil {
							t.Fatalf("completion=%s err=%v cancellation=%v", result.outcome, result.err, cancelErr)
						}
						fixture.assertStates(t, wantBuild, wantSteps)
					})
				}
			}
		}
	}
}

func TestBuildCompletionClaimsAndDuplicates_Postgres(t *testing.T) {
	for _, atomic := range []bool{true, false} {
		for _, status := range []domain.BuildStepStatus{domain.BuildStepStatusSuccess, domain.BuildStepStatusFailed} {
			for _, claimCase := range []string{"wrong-token", "expired", "duplicate"} {
				t.Run(fmt.Sprintf("atomic=%t/%s/%s", atomic, status, claimCase), func(t *testing.T) {
					fixture := newBuildCompletionFixture(t, 1)
					if claimCase == "expired" {
						fixture.exec(t, `UPDATE build_jobs SET claim_expires_at = NOW() - INTERVAL '1 minute' WHERE build_id = $1`, fixture.buildID)
						fixture.exec(t, `UPDATE build_steps SET lease_expires_at = NOW() - INTERVAL '1 minute' WHERE build_id = $1`, fixture.buildID)
					}
					if claimCase == "duplicate" {
						first := fixture.complete(0, status, atomic, fixture.tokens[0])
						if first.err != nil || first.outcome != repository.StepCompletionCompleted {
							t.Fatalf("initial completion: %+v", first)
						}
					}
					before := fixture.snapshot(t)
					token := fixture.tokens[0]
					if claimCase != "expired" {
						token = "obsolete-token"
					}
					result := fixture.complete(0, status, atomic, token)
					wantOutcome := repository.StepCompletionStaleClaim
					if claimCase == "duplicate" {
						wantOutcome = repository.StepCompletionDuplicateTerminal
					} else if claimCase == "expired" && !atomic {
						wantOutcome = repository.StepCompletionCompleted
					}
					if result.err != nil || result.outcome != wantOutcome || result.step.BuildBecameSuccessful && wantOutcome != repository.StepCompletionCompleted {
						t.Fatalf("completion=%+v, want %s", result, wantOutcome)
					}
					if wantOutcome != repository.StepCompletionCompleted && fixture.snapshot(t) != before {
						t.Fatal("stale or duplicate completion mutated persisted state")
					}
				})
			}
		}
	}
}

func TestBuildCompletionConcurrentDuplicate_Postgres(t *testing.T) {
	for _, atomic := range []bool{true, false} {
		for _, status := range []domain.BuildStepStatus{domain.BuildStepStatusSuccess, domain.BuildStepStatusFailed} {
			t.Run(fmt.Sprintf("atomic=%t/%s", atomic, status), func(t *testing.T) {
				fixture := newBuildCompletionFixture(t, 1)
				gate, blocker := fixture.lockBuild(t)
				results := make(chan buildCompletionCall, 2)
				for range 2 {
					go func() { results <- fixture.complete(0, status, atomic, fixture.tokens[0]) }()
				}
				fixture.waitBuildWaiters(t, blocker, 2)
				if releaseErr := gate.Commit(); releaseErr != nil {
					t.Fatal(releaseErr)
				}
				outcomes := map[repository.StepCompletionOutcome]int{}
				for range 2 {
					result := <-results
					if result.err != nil {
						t.Fatal(result.err)
					}
					outcomes[result.outcome]++
				}
				if outcomes[repository.StepCompletionCompleted] != 1 || outcomes[repository.StepCompletionDuplicateTerminal] != 1 {
					t.Fatalf("concurrent completion outcomes: %v", outcomes)
				}
				fixture.assertStates(t, domain.BuildStatus(status), []domain.BuildStepStatus{status})
			})
		}
	}
}

func TestBuildCompletionClaimChangesWhileWaiting_Postgres(t *testing.T) {
	for _, atomic := range []bool{true, false} {
		for _, status := range []domain.BuildStepStatus{domain.BuildStepStatusSuccess, domain.BuildStepStatusFailed} {
			t.Run(fmt.Sprintf("atomic=%t/%s", atomic, status), func(t *testing.T) {
				fixture := newBuildCompletionFixture(t, 1)
				gate, blocker := fixture.lockBuild(t)
				results := make(chan buildCompletionCall, 1)
				go func() { results <- fixture.complete(0, status, atomic, fixture.tokens[0]) }()
				fixture.waitBuildWaiters(t, blocker, 1)
				if atomic {
					fixture.exec(t, `UPDATE build_jobs SET claim_expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, fixture.jobs[0])
				} else {
					fixture.exec(t, `UPDATE build_steps SET claim_token = 'replacement' WHERE build_id = $1`, fixture.buildID)
				}
				before := fixture.snapshot(t)
				if releaseErr := gate.Commit(); releaseErr != nil {
					t.Fatal(releaseErr)
				}
				result := <-results
				if result.err != nil || result.outcome != repository.StepCompletionStaleClaim || fixture.snapshot(t) != before {
					t.Fatalf("completion did not revalidate claim after lock wait: %+v", result)
				}
			})
		}
	}
}

func TestBuildCompletionPreservesDAGProgress_Postgres(t *testing.T) {
	for _, atomic := range []bool{true, false} {
		for _, blocked := range []bool{true, false} {
			t.Run(fmt.Sprintf("atomic=%t/blocked=%t", atomic, blocked), func(t *testing.T) {
				fixture := newBuildCompletionFixture(t, 3)
				dependencies := "[]"
				if blocked {
					dependencies = `["node-0", "node-1"]`
				}
				fixture.exec(t, `UPDATE build_steps SET status = 'pending', depends_on_node_ids = $2::jsonb WHERE build_id = $1 AND step_index = 2`, fixture.buildID, dependencies)
				fixture.exec(t, `UPDATE build_jobs SET status = 'queued', depends_on_node_ids = $2::jsonb WHERE id = $1`, fixture.jobs[2], dependencies)
				first := fixture.complete(0, domain.BuildStepStatusFailed, atomic, fixture.tokens[0])
				if first.err != nil || first.outcome != repository.StepCompletionCompleted {
					t.Fatalf("first completion: %+v", first)
				}
				fixture.assertStates(t, domain.BuildStatusRunning, []domain.BuildStepStatus{domain.BuildStepStatusFailed, domain.BuildStepStatusRunning, domain.BuildStepStatusPending})
				second := fixture.complete(1, domain.BuildStepStatusSuccess, atomic, fixture.tokens[1])
				if second.err != nil || second.outcome != repository.StepCompletionCompleted {
					t.Fatalf("sibling completion: %+v", second)
				}
				wantBuild := domain.BuildStatusRunning
				if blocked {
					wantBuild = domain.BuildStatusFailed
				}
				fixture.assertStates(t, wantBuild, []domain.BuildStepStatus{domain.BuildStepStatusFailed, domain.BuildStepStatusSuccess, domain.BuildStepStatusPending})
			})
		}
	}
}

func TestBuildCompletionLockErrors_Postgres(t *testing.T) {
	for _, mode := range []string{"legacy", "success", "failure"} {
		for _, missing := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/missing=%t", mode, missing), func(t *testing.T) {
				fixture := newBuildCompletionFixture(t, 1)
				status := domain.BuildStepStatusSuccess
				if mode == "failure" {
					status = domain.BuildStepStatusFailed
				}
				before := fixture.snapshot(t)
				caller := *fixture
				var result buildCompletionCall
				wantErr := repository.ErrBuildNotFound
				if missing {
					caller.buildID = uuid.NewString()
					result = caller.complete(0, status, mode != "legacy", caller.tokens[0])
				} else {
					_, blocker := fixture.lockBuild(t)
					ctx, cancel := context.WithCancel(fixture.ctx)
					defer cancel()
					caller.ctx = ctx
					results := make(chan buildCompletionCall, 1)
					go func() { results <- caller.complete(0, status, mode != "legacy", caller.tokens[0]) }()
					fixture.waitBuildWaiters(t, blocker, 1)
					cancel()
					result = <-results
					wantErr = context.Canceled
				}
				if !errors.Is(result.err, wantErr) || fixture.snapshot(t) != before {
					t.Fatalf("lock failure result=%+v, want %v with no mutation", result, wantErr)
				}
			})
		}
	}
}

func TestBuildCompletionCommitRollback_Postgres(t *testing.T) {
	for _, atomic := range []bool{true, false} {
		for _, status := range []domain.BuildStepStatus{domain.BuildStepStatusSuccess, domain.BuildStepStatusFailed, domain.BuildStepStatusCanceled} {
			t.Run(fmt.Sprintf("atomic=%t/%s", atomic, status), func(t *testing.T) {
				fixture := newBuildCompletionFixture(t, 1)
				allowCommit := fixture.injectCommitFailure(t)
				before := fixture.snapshot(t)
				var mutationErr error
				if status == domain.BuildStepStatusCanceled {
					_, _, mutationErr = NewBuildRepository(fixture.db).CancelBuild(fixture.ctx, fixture.buildID, "cancel", time.Now().UTC())
				} else {
					mutationErr = fixture.complete(0, status, atomic, fixture.tokens[0]).err
				}
				if mutationErr == nil || !strings.Contains(mutationErr.Error(), "injected completion commit failure") {
					t.Fatalf("expected deferred commit failure, got %v", mutationErr)
				}
				if fixture.snapshot(t) != before {
					t.Fatal("commit failure did not roll back all step/job/build mutations and claims")
				}
				allowCommit()
				result := fixture.complete(0, domain.BuildStepStatusSuccess, atomic, fixture.tokens[0])
				if result.err != nil || result.outcome != repository.StepCompletionCompleted {
					t.Fatalf("completion after rollback: %+v", result)
				}
			})
		}
	}
}

func TestBuildSynchronousCompletion_Postgres(t *testing.T) {
	for _, status := range []runner.RunStepStatus{runner.RunStepStatusSuccess, runner.RunStepStatusFailed} {
		for _, failCommit := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/rollback=%t", status, failCommit), func(t *testing.T) {
				fixture := newBuildCompletionFixture(t, 1)
				jobs := NewExecutionJobRepository(fixture.db)
				service := buildsvc.NewBuildServiceFromConfig(NewBuildRepository(fixture.db), nil, nil, buildsvc.BuildServiceConfig{ExecutionJobRepo: jobs})
				request := runner.RunStepRequest{BuildID: fixture.buildID, JobID: fixture.jobs[0], StepIndex: 0, StepName: "sibling", ClaimToken: fixture.tokens[0]}
				now := time.Now().UTC()
				result := runner.RunStepResult{Status: status, StartedAt: now, FinishedAt: now}
				if status == runner.RunStepStatusFailed {
					result.ExitCode = 1
				}
				if failCommit {
					allowCommit := fixture.injectCommitFailure(t)
					before := fixture.snapshot(t)
					report, completeErr := service.HandleStepResult(fixture.ctx, request, result)
					if completeErr == nil || !strings.Contains(completeErr.Error(), "injected completion commit failure") || report.CompletionOutcome != repository.StepCompletionInvalidTransition || report.SideEffectErr != nil {
						t.Fatalf("expected lifecycle persistence error, report=%+v err=%v", report, completeErr)
					}
					if fixture.snapshot(t) != before {
						t.Fatal("service completion partially persisted after commit failure")
					}
					allowCommit()
				}
				report, completeErr := service.HandleStepResult(fixture.ctx, request, result)
				if completeErr != nil || report.CompletionOutcome != repository.StepCompletionCompleted || report.BuildBecameSuccessful != (status == runner.RunStepStatusSuccess) {
					t.Fatalf("completion=%+v err=%v", report, completeErr)
				}
				fixture.assertStates(t, domain.BuildStatus(status), []domain.BuildStepStatus{domain.BuildStepStatus(status)})
				job, jobErr := jobs.GetJobByID(fixture.ctx, request.JobID)
				if jobErr != nil || job.Status != domain.ExecutionJobStatus(status) || job.ClaimToken != nil {
					t.Fatalf("job completion=%+v err=%v", job, jobErr)
				}
				before := fixture.snapshot(t)
				request.ClaimToken = "obsolete-token"
				duplicate, duplicateErr := service.HandleStepResult(fixture.ctx, request, result)
				if duplicateErr != nil || duplicate.CompletionOutcome != repository.StepCompletionDuplicateTerminal || duplicate.BuildBecameSuccessful || fixture.snapshot(t) != before {
					t.Fatalf("duplicate=%+v err=%v", duplicate, duplicateErr)
				}
			})
		}
	}
}

func (fixture *buildCompletionFixture) injectCommitFailure(t *testing.T) func() {
	t.Helper()
	trigger := "completion_failure_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	fixture.exec(t, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected completion commit failure'; END $$`, trigger))
	t.Cleanup(func() {
		_, _ = fixture.db.ExecContext(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON builds; DROP FUNCTION %s()`, trigger, trigger))
	})
	fixture.exec(t, fmt.Sprintf(`CREATE CONSTRAINT TRIGGER %s AFTER UPDATE ON builds
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
		WHEN (NEW.id = '%s'::uuid AND NEW.status IN ('success', 'failed', 'canceled'))
		EXECUTE FUNCTION %s()`, trigger, fixture.buildID, trigger))
	return func() { fixture.exec(t, fmt.Sprintf(`DROP TRIGGER %s ON builds`, trigger)) }
}

type buildCompletionCall struct {
	step    repository.CompleteStepResult
	outcome repository.StepCompletionOutcome
	err     error
}

type buildCompletionFixture struct {
	db      *sql.DB
	ctx     context.Context
	buildID string
	jobs    []string
	tokens  []string
}

func newBuildCompletionFixture(t *testing.T, count int) *buildCompletionFixture {
	t.Helper()
	db := openNotificationIntegrationDB(t)
	t.Cleanup(func() { closeNotificationIntegrationDB(t, db) })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	fixture := &buildCompletionFixture{db: db, ctx: ctx, buildID: createNotificationIntegrationBuild(t, db, context.Background())}
	fixture.exec(t, `UPDATE builds SET status = 'running', started_at = NOW() WHERE id = $1`, fixture.buildID)
	for index := range count {
		stepID, jobID, token := uuid.NewString(), uuid.NewString(), uuid.NewString()
		fixture.exec(t, `INSERT INTO build_steps
			(id, build_id, step_index, node_id, name, command, status, claim_token, claimed_at, lease_expires_at, started_at)
			VALUES ($1, $2, $3, $4, 'sibling', 'true', 'running', $5, NOW(), NOW() + INTERVAL '1 hour', NOW())`,
			stepID, fixture.buildID, index, fmt.Sprintf("node-%d", index), token)
		fixture.exec(t, `INSERT INTO build_jobs
			(id, build_id, step_id, step_index, node_id, name, image, working_dir, command_json, source_commit_sha,
			 resolved_spec_json, status, claim_token, claimed_by, claim_expires_at, created_at, started_at)
			VALUES ($1, $2, $3, $4, $5, 'sibling', 'alpine', '.', '["true"]', 'test', '{}', 'running', $6, 'worker',
			 NOW() + INTERVAL '1 hour', NOW(), NOW())`, jobID, fixture.buildID, stepID, index, fmt.Sprintf("node-%d", index), token)
		fixture.jobs = append(fixture.jobs, jobID)
		fixture.tokens = append(fixture.tokens, token)
	}
	return fixture
}

func (fixture *buildCompletionFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, execErr := fixture.db.ExecContext(fixture.ctx, query, args...); execErr != nil {
		t.Fatal(execErr)
	}
}

func (fixture *buildCompletionFixture) complete(index int, status domain.BuildStepStatus, atomic bool, token string) buildCompletionCall {
	now := time.Now().UTC()
	exitCode := 0
	if status == domain.BuildStepStatusFailed {
		exitCode = 1
	}
	request := repository.CompleteStepRequest{BuildID: fixture.buildID, StepIndex: index, RequireClaim: true, ClaimToken: token,
		Update: repository.StepUpdate{Status: status, FinishedAt: &now, ExitCode: &exitCode}}
	var result buildCompletionCall
	if !atomic {
		result.step, result.err = NewBuildRepository(fixture.db).CompleteStep(fixture.ctx, request)
		result.outcome = result.step.Outcome
	} else if status == domain.BuildStepStatusSuccess {
		result.step, _, result.outcome, result.err = NewExecutionJobRepository(fixture.db).CompleteSuccessfulStepAndJob(fixture.ctx,
			repository.CompleteSuccessfulStepAndJobRequest{JobID: fixture.jobs[index], ClaimToken: token, FinishedAt: now, ExitCode: exitCode, StepRequest: request})
	} else {
		result.step, _, result.outcome, result.err = NewExecutionJobRepository(fixture.db).CompleteFailedStepAndJob(fixture.ctx,
			repository.CompleteFailedStepAndJobRequest{JobID: fixture.jobs[index], ClaimToken: token, FinishedAt: now, ExitCode: &exitCode,
				ErrorMessage: "step failed", FailureKind: domain.ExecutionFailureKindExecution, StepRequest: request})
	}
	return result
}

func (fixture *buildCompletionFixture) lockBuild(t *testing.T) (*sql.Tx, int) {
	t.Helper()
	tx, beginErr := fixture.db.BeginTx(fixture.ctx, nil)
	if beginErr != nil {
		t.Fatal(beginErr)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	var blocker int
	if lockErr := tx.QueryRowContext(fixture.ctx, `SELECT pg_backend_pid() FROM builds WHERE id = $1 FOR NO KEY UPDATE`, fixture.buildID).Scan(&blocker); lockErr != nil {
		t.Fatal(lockErr)
	}
	return tx, blocker
}

// Observe PostgreSQL's lock wait graph, not elapsed time, before releasing a gate.
func (fixture *buildCompletionFixture) waitBuildWaiters(t *testing.T, blocker, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(fixture.ctx, 5*time.Second)
	defer cancel()
	for {
		var count int
		queryErr := fixture.db.QueryRowContext(ctx, `WITH RECURSIVE waiters(pid) AS (
			SELECT $1::int
			UNION
			SELECT activity.pid FROM pg_stat_activity activity
			JOIN waiters ON waiters.pid = ANY(pg_blocking_pids(activity.pid))
			WHERE activity.datname = current_database()
		) SELECT COUNT(*) FROM waiters JOIN pg_stat_activity USING (pid)
		WHERE pid <> $1 AND wait_event_type = 'Lock' AND query ~ '^[[:space:]]*SELECT'`, blocker).Scan(&count)
		if queryErr != nil {
			t.Fatalf("waiting for %d build-lock waiters: %v", want, queryErr)
		}
		if count == want {
			return
		}
	}
}

func (fixture *buildCompletionFixture) assertStates(t *testing.T, buildStatus domain.BuildStatus, statuses []domain.BuildStepStatus) {
	t.Helper()
	buildRepo := NewBuildRepository(fixture.db)
	build, buildErr := buildRepo.GetByID(fixture.ctx, fixture.buildID)
	if buildErr != nil || build.Status != buildStatus {
		t.Fatalf("build status=%s, want %s, err=%v", build.Status, buildStatus, buildErr)
	}
	steps, stepsErr := buildRepo.GetStepsByBuildID(fixture.ctx, fixture.buildID)
	if stepsErr != nil || len(steps) != len(statuses) {
		t.Fatalf("steps=%d, want %d, err=%v", len(steps), len(statuses), stepsErr)
	}
	for index, step := range steps {
		if step.Status != statuses[index] || domain.IsTerminalStepStatus(step.Status) && step.ClaimToken != nil {
			t.Fatalf("step %d status=%s token=%v, want %s", index, step.Status, step.ClaimToken, statuses[index])
		}
	}
}

func (fixture *buildCompletionFixture) snapshot(t *testing.T) string {
	t.Helper()
	var snapshot string
	snapshotErr := fixture.db.QueryRowContext(fixture.ctx, `SELECT jsonb_build_object(
		'build', to_jsonb(builds),
		'steps', (SELECT jsonb_agg(to_jsonb(bs) ORDER BY step_index) FROM build_steps bs WHERE build_id = $1),
		'jobs', (SELECT jsonb_agg(to_jsonb(bj) ORDER BY step_index) FROM build_jobs bj WHERE build_id = $1)
	) FROM builds WHERE id = $1`, fixture.buildID).Scan(&snapshot)
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	return snapshot
}

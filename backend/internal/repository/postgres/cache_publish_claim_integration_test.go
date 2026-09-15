package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCacheEntryRepository_AcquirePublishClaimConcurrentClaimsOnce_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	jobID := "cache-job-" + uuid.NewString()
	preset := "go-module"
	cacheKey := "go-module:" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM cache_publish_claims WHERE job_id = $1 AND preset = $2 AND cache_key = $3`, jobID, preset, cacheKey)
	})

	repo := NewCacheEntryRepository(db)
	now := time.Now().UTC()
	const publishers = 8
	start := make(chan struct{})
	results := make(chan bool, publishers)
	errors := make(chan error, publishers)
	var waitGroup sync.WaitGroup
	for index := 0; index < publishers; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			_, acquired, claimErr := repo.TryAcquirePublishClaim(context.Background(), jobID, preset, cacheKey, uuid.NewString(), now, time.Minute)
			if claimErr != nil {
				errors <- claimErr
				return
			}
			results <- acquired
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errors)
	close(results)
	for claimErr := range errors {
		if claimErr != nil {
			t.Fatalf("acquire publish claim: %v", claimErr)
		}
	}
	acquired := 0
	for result := range results {
		if result {
			acquired++
		}
	}
	if acquired != 1 {
		t.Fatalf("acquired claims=%d, want 1", acquired)
	}
}

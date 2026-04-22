package build

import "context"

type ManagedImageRefreshInput struct {
	JobID         string
	ProjectID     string
	RepositoryURL string
	Ref           string
	BaseBranch    string
	PipelinePath  string
}

type ManagedImageRefreshResult struct {
	ManagedImageID        string
	ManagedImageVersionID string
	DependencyFingerprint string
	PinnedImageRef        string
	Updated               bool
	BranchName            string
	CommitSHA             string
}

type ManagedImageRefresher interface {
	RefreshManagedPipelineImage(ctx context.Context, req ManagedImageRefreshInput) (ManagedImageRefreshResult, error)
}

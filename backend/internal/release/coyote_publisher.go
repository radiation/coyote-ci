package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

func (p *FilePublisher) PromoteCoyoteLatestIfNewer(ctx context.Context, releaseVersion, manifestDigest string) (bool, error) {
	releaseLock, err := p.acquireLock(ctx, filepath.Join(p.root, ".channels.lock"))
	if err != nil {
		return false, err
	}
	defer func() { _ = os.Remove(releaseLock) }()
	if !semVerPattern.MatchString(releaseVersion) || !digestPattern.MatchString(manifestDigest) {
		return false, errors.New("channel promotion requires an exact release and manifest digest")
	}
	source, err := NewFileSource(p.root)
	if err != nil {
		return false, err
	}
	index, err := source.FetchChannelIndex(ctx)
	if errors.Is(err, ErrNotFound) {
		index = ChannelIndex{SchemaVersion: SchemaVersion, Channels: map[string]ChannelEntry{}}
	} else if err != nil {
		return false, err
	}
	if err := index.Validate(); err != nil {
		return false, err
	}
	if current, found := index.Channels["latest"]; found {
		currentNumber, currentOK := coyoteBuildNumber(current.Release)
		candidateNumber, candidateOK := coyoteBuildNumber(releaseVersion)
		if !currentOK || !candidateOK {
			return false, errors.New("automatic Coyote latest requires 0.0.<build_number> releases")
		}
		if candidateNumber < currentNumber {
			return false, nil
		}
	}
	index.Channels["latest"] = ChannelEntry{Release: releaseVersion, ManifestDigest: manifestDigest}
	return true, p.PublishChannels(index)
}

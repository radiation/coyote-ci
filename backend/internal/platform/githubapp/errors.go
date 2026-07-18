package githubapp

import "errors"

var ErrPrivateKeyMissing = errors.New("github app private key is required")
var ErrPrivateKeyMalformed = errors.New("github app private key is malformed")
var ErrPrivateKeyNotRSA = errors.New("github app private key must be an rsa private key")
var ErrAuthentication = errors.New("github app authentication failed")
var ErrInstallationUnavailable = errors.New("github app installation is unavailable")
var ErrRateLimited = errors.New("github app request was rate limited")
var ErrProviderUnavailable = errors.New("github app provider is unavailable")
var ErrMalformedResponse = errors.New("github app provider response was malformed")

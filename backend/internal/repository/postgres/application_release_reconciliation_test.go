package postgres

import "testing"

func TestImagePublicationMatches(t *testing.T) {
	definition := `{"target_image_reference":"acme/api"}`
	validReference := "us-central1-docker.pkg.dev/example/apps/acme/api:1.4.2"
	validDigest := "us-central1-docker.pkg.dev/example/apps/acme/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name      string
		target    string
		reference string
		digest    string
		want      bool
	}{
		{"valid artifact registry publication", "acme/api", validReference, validDigest, true},
		{"logical target mismatch", "acme/web", validReference, validDigest, false},
		{"version mismatch", "acme/api", "us-central1-docker.pkg.dev/example/apps/acme/api:1.4.3", validDigest, false},
		{"digest repository mismatch", "acme/api", validReference, "us-central1-docker.pkg.dev/example/apps/acme/web@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"missing published reference", "acme/api", "", validDigest, false},
		{"missing digest reference", "acme/api", validReference, "", false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := imagePublicationMatches(definition, "1.4.2", testCase.target, testCase.reference, testCase.digest); got != testCase.want {
				t.Fatalf("imagePublicationMatches() = %t, want %t", got, testCase.want)
			}
		})
	}
}

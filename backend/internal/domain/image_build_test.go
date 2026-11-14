package domain

import "testing"

func TestValidateImagePlatforms(t *testing.T) {
	for _, test := range []struct {
		name      string
		platforms []string
		valid     bool
	}{
		{"legacy", nil, true},
		{"amd64", []string{"linux/amd64"}, true},
		{"both", []string{"linux/amd64", "linux/arm64"}, true},
		{"duplicate", []string{"linux/arm64", "linux/arm64"}, false},
		{"unsupported", []string{"windows/amd64"}, false},
		{"empty", []string{""}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateImagePlatforms(test.platforms); (err == nil) != test.valid {
				t.Fatalf("platforms=%v error=%v", test.platforms, err)
			}
		})
	}
}

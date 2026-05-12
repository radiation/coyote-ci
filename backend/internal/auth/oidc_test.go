package auth

import "testing"

func TestIdentityFromOIDCClaims(t *testing.T) {
	verified := true
	unverified := false

	tests := []struct {
		name        string
		claims      oidcClaims
		expectErr   error
		expectEmail string
		expectName  *string
	}{
		{
			name:      "requires email",
			claims:    oidcClaims{Name: "Missing Email"},
			expectErr: ErrOIDCEmailRequired,
		},
		{
			name:      "rejects unverified email when claim is present",
			claims:    oidcClaims{Email: "user@example.com", EmailVerified: &unverified},
			expectErr: ErrOIDCEmailNotVerified,
		},
		{
			name:        "accepts verified email",
			claims:      oidcClaims{Email: "user@example.com", EmailVerified: &verified, Name: "Verified User"},
			expectEmail: "user@example.com",
			expectName:  stringPointer("Verified User"),
		},
		{
			name:        "accepts missing email_verified claim",
			claims:      oidcClaims{Email: "user@example.com", PreferredUsername: "coyote-user"},
			expectEmail: "user@example.com",
			expectName:  stringPointer("coyote-user"),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			identity, err := identityFromOIDCClaims(tc.claims)
			if tc.expectErr != nil {
				if err != tc.expectErr {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if identity.Email != tc.expectEmail {
				t.Fatalf("expected email %q, got %q", tc.expectEmail, identity.Email)
			}
			if !equalStringPointers(identity.DisplayName, tc.expectName) {
				t.Fatalf("expected display name %v, got %v", tc.expectName, identity.DisplayName)
			}
		})
	}
}

func stringPointer(value string) *string {
	return &value
}

func equalStringPointers(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

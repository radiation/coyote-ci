package domain

import (
	"errors"
	"reflect"
	"testing"
)

func TestAPITokenScopesReturnsCopyInDisplayOrder(t *testing.T) {
	got := APITokenScopes()
	want := []APITokenScope{
		APITokenScopeArtifactRead,
		APITokenScopeBuildLogs,
		APITokenScopeBuildRead,
		APITokenScopeBuildRun,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected scopes %v, got %v", want, got)
	}

	got[0] = APITokenScopeBuildRun
	again := APITokenScopes()
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("expected fresh scope slice %v, got %v", want, again)
	}
}

func TestNormalizeAPITokenScopes(t *testing.T) {
	t.Run("empty input returns empty slice", func(t *testing.T) {
		scopes, err := NormalizeAPITokenScopes(nil)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if len(scopes) != 0 {
			t.Fatalf("expected empty scopes, got %v", scopes)
		}
	})

	t.Run("trims deduplicates and sorts", func(t *testing.T) {
		scopes, err := NormalizeAPITokenScopes([]string{" build:run ", "build:read", "", "artifact:read", "build:read"})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		want := []APITokenScope{APITokenScopeArtifactRead, APITokenScopeBuildRead, APITokenScopeBuildRun}
		if !reflect.DeepEqual(scopes, want) {
			t.Fatalf("expected scopes %v, got %v", want, scopes)
		}
	})

	t.Run("unknown scope returns sentinel error", func(t *testing.T) {
		_, err := NormalizeAPITokenScopes([]string{"build:read", "invalid:scope"})
		if !errors.Is(err, ErrUnknownAPITokenScope) {
			t.Fatalf("expected unknown scope error, got %v", err)
		}
	})
}

func TestCloneAPITokenScopes(t *testing.T) {
	if cloned := CloneAPITokenScopes(nil); len(cloned) != 0 {
		t.Fatalf("expected empty clone for nil input, got %v", cloned)
	}

	original := []APITokenScope{APITokenScopeBuildRead, APITokenScopeBuildLogs}
	cloned := CloneAPITokenScopes(original)
	if !reflect.DeepEqual(cloned, original) {
		t.Fatalf("expected clone %v, got %v", original, cloned)
	}
	cloned[0] = APITokenScopeBuildRun
	if original[0] != APITokenScopeBuildRead {
		t.Fatalf("expected original slice to remain unchanged, got %v", original)
	}
}

func TestHasAPITokenScope(t *testing.T) {
	scopes := []APITokenScope{APITokenScopeBuildRead, APITokenScopeArtifactRead}
	if !HasAPITokenScope(scopes, APITokenScopeArtifactRead) {
		t.Fatal("expected artifact:read to be present")
	}
	if HasAPITokenScope(scopes, APITokenScopeBuildRun) {
		t.Fatal("expected build:run to be absent")
	}
}

package postgres

import (
	"database/sql"
	"testing"
)

func TestNewBuildStore(t *testing.T) {
	store := NewBuildStore(&sql.DB{})
	if store == nil {
		t.Fatal("expected store, got nil")
	}
}

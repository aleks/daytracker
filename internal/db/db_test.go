package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_DefaultPath(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	t.Setenv("DAYTRACKER_DB_PATH", dbPath)

	db, err := Open()
	require.NoError(t, err)
	require.NotNil(t, db)

	m := db.Migrator()
	assert.True(t, m.HasTable(&Day{}), "days table should exist")
	assert.True(t, m.HasTable(&Task{}), "tasks table should exist")
	assert.True(t, m.HasTable(&ActivityItem{}), "activity_items table should exist")
	assert.True(t, m.HasTable(&ConnectorState{}), "connector_states table should exist")
}

func TestOpen_UsesEnvVar(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "specific.db")
	t.Setenv("DAYTRACKER_DB_PATH", dbPath)

	db, err := Open()
	require.NoError(t, err)
	require.NotNil(t, db)

	_, statErr := os.Stat(dbPath)
	assert.NoError(t, statErr, "database file should have been created at the env var path")
}

func TestOpen_InvalidPath(t *testing.T) {
	t.Setenv("DAYTRACKER_DB_PATH", "/nonexistent/dir/db.sqlite")

	db, err := Open()
	assert.Error(t, err)
	assert.Nil(t, db)
}

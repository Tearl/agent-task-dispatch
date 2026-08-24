package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestClientMultiRoleMigrationBackfillsBothClientRoles(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000021_client_multi_role.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{"'publisher'", "'agent_provider'", "ON CONFLICT DO NOTHING"} {
		if !strings.Contains(sql, required) {
			t.Errorf("client multi-role migration missing %q", required)
		}
	}
}

func TestClientMultiRoleRollbackDoesNotRevokeRoleGrants(t *testing.T) {
	contents, err := os.ReadFile("migrations/000021_client_multi_role.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(contents))
	for _, forbidden := range []string{"delete from", "truncate", "drop table"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("client multi-role rollback revokes role grants with %q", forbidden)
		}
	}
}

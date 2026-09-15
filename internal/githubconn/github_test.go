package githubconn

import (
	"context"
	"testing"
)

func TestRepoMustBeOwnerSlashName(t *testing.T) {
	if _, err := ListOpenIssues(context.Background(), "token", "no-slash"); err == nil {
		t.Fatal("expected an error for a repo without owner/")
	}
}

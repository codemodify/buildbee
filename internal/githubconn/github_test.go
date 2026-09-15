package githubconn

import (
	"context"
	"errors"
	"testing"
)

func TestFakePR(t *testing.T) {
	res, err := OpenDraftPR(context.Background(), Options{Fake: true, TaskID: "aaaaaaaa-bbbb", Repo: "acme/buildbee"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Fake || res.URL == "" {
		t.Fatalf("%#v", res)
	}
}

func TestMissingConfigIsAnError(t *testing.T) {
	if _, err := OpenDraftPR(context.Background(), Options{TaskID: "task1"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v, want ErrNotConfigured", err)
	}
}

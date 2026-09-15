package githubconn

import "testing"

func TestFakePR(t *testing.T) {
	res, err := OpenDraftPR(nil, Options{Fake: true, TaskID: "aaaaaaaa-bbbb", Repo: "acme/buildbee"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Fake || res.URL == "" {
		t.Fatalf("%#v", res)
	}
}

func TestMissingTokenRecordsFake(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPO", "")
	res, err := OpenDraftPR(nil, Options{TaskID: "task1"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Fake {
		t.Fatal("expected fake")
	}
}

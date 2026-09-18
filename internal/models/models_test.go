package models

import "testing"

func TestRunStatusTransitions(t *testing.T) {
	allowed := map[RunStatus][]RunStatus{
		RunPending: {RunRunning, RunFailed, RunCanceled},
		RunRunning: {RunSucceeded, RunFailed, RunCanceled},
	}
	all := []RunStatus{RunPending, RunRunning, RunSucceeded, RunFailed, RunCanceled}
	for _, from := range all {
		for _, to := range all {
			want := false
			for _, ok := range allowed[from] {
				if ok == to {
					want = true
				}
			}
			if got := from.CanBecome(to); got != want {
				t.Errorf("%s -> %s: got %v want %v", from, to, got, want)
			}
		}
	}
	for _, s := range []RunStatus{RunSucceeded, RunFailed, RunCanceled} {
		if !s.Terminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
}

func TestParseStatuses(t *testing.T) {
	if _, ok := ParseTaskStatus("Done "); ok {
		t.Fatal("task status must be canonical")
	}
	if s, ok := ParseTaskStatus("in_progress"); !ok || s != TaskInProgress {
		t.Fatal("in_progress")
	}
	if _, ok := ParseRunStatus("finished"); ok {
		t.Fatal("unknown run status accepted")
	}
	for in, want := range map[string]PipelineStatus{
		"success": PipelineSuccess, "timed_out": PipelineFailure, "queued": PipelinePending,
	} {
		if got, ok := ParsePipelineStatus(in); !ok || got != want {
			t.Fatalf("%s: %v %v", in, got, ok)
		}
	}
	if _, ok := ParsePipelineStatus("banana"); ok {
		t.Fatal("unknown pipeline status accepted")
	}
}

func TestMentionedBots(t *testing.T) {
	members := []Member{
		{ID: "h", Kind: KindHuman, DisplayName: "Scout"},
		{ID: "s", Kind: KindBot, DisplayName: "Scout", Role: RoleScout},
		{ID: "b", Kind: KindBot, DisplayName: "Builder", Role: RoleBuilder},
	}
	got := MentionedBots("hey @scout and @BUILDER, also @scout again", members)
	if len(got) != 2 || got[0].ID != "s" || got[1].ID != "b" {
		t.Fatalf("got %+v", got)
	}
	if MentionedBots("no mentions", members) != nil {
		t.Fatal("expected nil")
	}
	if got := MentionedBots("please take this @Builder.", members); len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("trailing punctuation: %+v", got)
	}
}

// A Bot is mentioned by the name it was configured with, dashes and all.
func TestMentionedBotsUseTheNameAsSet(t *testing.T) {
	members := []Member{{ID: "g", Kind: KindBot, DisplayName: "dev-grok-nc-laptop", Role: RoleBuilder}}
	for _, body := range []string{
		"@dev-grok-nc-laptop tell me about your harness",
		"@DEV-Grok-NC-Laptop tell me about your harness",
		"ask @dev-grok-nc-laptop.",
	} {
		if got := MentionedBots(body, members); len(got) != 1 || got[0].ID != "g" {
			t.Fatalf("%q reached %+v", body, got)
		}
	}
	for _, body := range []string{
		"@dev-grok tell me about your harness",       // half a name
		"@dev_grok_nc_laptop about your harness",     // not how it was set
		"@dev-grok-nc-laptop-two whose bot is this?", // a longer name
	} {
		if got := MentionedBots(body, members); got != nil {
			t.Fatalf("%q must not reach it: %+v", body, got)
		}
	}
}

func TestMentionedPeople(t *testing.T) {
	members := []Member{
		{ID: "a", Kind: KindHuman, DisplayName: "Ada Lovelace"},
		{ID: "g", Kind: KindHuman, DisplayName: "Grace"},
		{ID: "s", Kind: KindBot, DisplayName: "Scout", Role: RoleScout},
	}
	got := MentionedPeople("@Ada Lovelace and @Grace, not @Scout", members)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "g" {
		t.Fatalf("got %+v", got)
	}
	// A name with spaces is written the way it reads.
	if got := MentionedPeople("@ada lovelace!", members); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("name as set: %+v", got)
	}
	if got := MentionedPeople("@Ada_Lovelace", members); got != nil {
		t.Fatalf("underscores are not that name: %+v", got)
	}
}

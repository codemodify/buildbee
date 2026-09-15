package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	newTestMux(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body: %#v", body)
	}
}

func TestV1Index(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/", nil)
	newTestMux(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["api"] != "v1" {
		t.Fatalf("body: %#v", body)
	}
}

func TestUnknownPath(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/nope", nil)
	newTestMux(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCrossOriginWritesRefused(t *testing.T) {
	h := newTestMux(t)
	body := strings.NewReader(`{"name":"x"}`)
	req := httptest.NewRequest(http.MethodPost, "http://buildbee.lan/v1/projects", body)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST: got %d want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "http://buildbee.lan/v1/projects", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Origin", "http://buildbee.lan")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("same-origin POST: got %d want 201: %s", rec.Code, rec.Body.String())
	}
}

func TestRunEventsStream(t *testing.T) {
	h := newTestMux(t)
	proj := decode[map[string]any](t, doJSON(t, h, http.MethodPost, "/v1/projects", map[string]string{"name": "Stream Project"}))
	task := decode[map[string]any](t, doJSON(t, h, http.MethodPost, "/v1/projects/"+proj["id"].(string)+"/tasks?handoff=none", map[string]string{"title": "Stream"}))
	run := decode[map[string]any](t, doJSON(t, h, http.MethodPost, "/v1/tasks/"+task["id"].(string)+"/runs", nil))
	rid := run["id"].(string)

	tok := doJSON(t, h, http.MethodPost, "/v1/runs/"+rid+"/events", map[string]any{
		"kind": "token", "payload": map[string]any{"text": "hello"},
	})
	if tok.Code != http.StatusCreated {
		t.Fatalf("token: %d %s", tok.Code, tok.Body.String())
	}
	tool := doJSON(t, h, http.MethodPost, "/v1/runs/"+rid+"/events", map[string]any{
		"kind": "tool_call", "payload": map[string]any{"name": "read_task"},
	})
	if tool.Code != http.StatusCreated {
		t.Fatalf("tool: %d %s", tool.Code, tool.Body.String())
	}
	bad := doJSON(t, h, http.MethodPost, "/v1/runs/"+rid+"/events", map[string]any{"kind": "nope"})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad kind: %d", bad.Code)
	}

	all := doJSON(t, h, http.MethodGet, "/v1/runs/"+rid+"/events", nil)
	var listed struct {
		Items []map[string]any `json:"items"`
	}
	listed = decode[struct {
		Items []map[string]any `json:"items"`
	}](t, all)
	if len(listed.Items) < 3 { // pending status + token + tool
		t.Fatalf("events: %#v", listed.Items)
	}
	after := doJSON(t, h, http.MethodGet, "/v1/runs/"+rid+"/events?after=1", nil)
	listed = decode[struct {
		Items []map[string]any `json:"items"`
	}](t, after)
	for _, ev := range listed.Items {
		if int(ev["seq"].(float64)) <= 1 {
			t.Fatalf("after=1 returned seq %+v", ev)
		}
	}
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v\n%s", rec.Result().Status, err, rec.Body.String())
	}
	return out
}

func TestVerticalSlice(t *testing.T) {
	h := newTestMux(t)

	created := doJSON(t, h, http.MethodPost, "/v1/projects", map[string]string{"name": "Project"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", created.Code, created.Body.String())
	}
	type seededMember struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
		Role string `json:"role"`
	}
	var proj struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Members  []seededMember
		Channels []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"channels"`
	}
	proj = decode[struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Members  []seededMember
		Channels []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"channels"`
	}](t, created)
	if proj.ID == "" || len(proj.Members) < 5 || len(proj.Channels) < 1 {
		t.Fatalf("seed: %#v", proj)
	}
	var human, bot string
	roles := map[string]string{}
	for _, m := range proj.Members {
		if m.Kind == "human" {
			human = m.ID
		}
		if m.Kind == "bot" {
			roles[m.Role] = m.ID
			if bot == "" {
				bot = m.ID
			}
		}
	}
	if roles["scout"] == "" || roles["builder"] == "" || roles["sentry"] == "" || roles["pulse"] == "" {
		t.Fatalf("expected Scout/Builder/Sentry/Pulse, got %#v", roles)
	}
	channelID := proj.Channels[0].ID

	got := doJSON(t, h, http.MethodGet, "/v1/projects/"+proj.ID, nil)
	if got.Code != http.StatusOK {
		t.Fatalf("get project: %d", got.Code)
	}

	msg := doJSON(t, h, http.MethodPost, "/v1/channels/"+channelID+"/messages", map[string]string{"body": "hello project", "member_id": human})
	if msg.Code != http.StatusCreated {
		t.Fatalf("message: %d %s", msg.Code, msg.Body.String())
	}

	taskRec := doJSON(t, h, http.MethodPost, "/v1/projects/"+proj.ID+"/tasks", map[string]string{"title": "Ship slice"})
	if taskRec.Code != http.StatusCreated {
		t.Fatalf("task: %d %s", taskRec.Code, taskRec.Body.String())
	}
	task := decode[map[string]any](t, taskRec)
	taskID := task["id"].(string)

	ho := doJSON(t, h, http.MethodPost, "/v1/tasks/"+taskID+"/handoffs", map[string]string{
		"from_member_id": human, "to_member_id": bot, "note": "please take this",
	})
	if ho.Code != http.StatusCreated {
		t.Fatalf("handoff: %d %s", ho.Code, ho.Body.String())
	}
	handoff := decode[map[string]any](t, ho)
	done := doJSON(t, h, http.MethodPost, "/v1/handoffs/"+handoff["id"].(string)+"/complete", map[string]string{})
	if done.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", done.Code, done.Body.String())
	}

	dec := doJSON(t, h, http.MethodPost, "/v1/projects/"+proj.ID+"/decisions", map[string]any{
		"prompt": "Ship today?", "options": []string{"yes", "no"}, "recommendation": "yes",
	})
	if dec.Code != http.StatusCreated {
		t.Fatalf("decision: %d %s", dec.Code, dec.Body.String())
	}
	decision := decode[map[string]any](t, dec)
	ans := doJSON(t, h, http.MethodPost, "/v1/decisions/"+decision["id"].(string)+"/answer", map[string]string{"answer": "yes"})
	if ans.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", ans.Code, ans.Body.String())
	}

	runRec := doJSON(t, h, http.MethodPost, "/v1/tasks/"+taskID+"/runs", nil)
	if runRec.Code != http.StatusCreated {
		t.Fatalf("run: %d %s", runRec.Code, runRec.Body.String())
	}
	run := decode[map[string]any](t, runRec)
	patch := doJSON(t, h, http.MethodPatch, "/v1/runs/"+run["id"].(string), map[string]string{"status": "succeeded", "detail": "fake success"})
	if patch.Code != http.StatusOK {
		t.Fatalf("run patch: %d %s", patch.Code, patch.Body.String())
	}

	feed := doJSON(t, h, http.MethodGet, "/v1/projects/"+proj.ID+"/activity?type=decision", nil)
	if feed.Code != http.StatusOK {
		t.Fatalf("activity: %d %s", feed.Code, feed.Body.String())
	}
	var items struct {
		Items []map[string]any `json:"items"`
	}
	items = decode[struct {
		Items []map[string]any `json:"items"`
	}](t, feed)
	if len(items.Items) == 0 {
		t.Fatal("expected decision Activity")
	}

	list := doJSON(t, h, http.MethodGet, "/v1/projects/"+proj.ID+"/tasks", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("task list: %d", list.Code)
	}

	art := doJSON(t, h, http.MethodPost, "/v1/tasks/"+taskID+"/artifacts", map[string]string{
		"kind": "log", "name": "sandbox.log", "body": "ok\n", "run_id": run["id"].(string),
	})
	if art.Code != http.StatusCreated {
		t.Fatalf("artifact: %d %s", art.Code, art.Body.String())
	}
	pr := doJSON(t, h, http.MethodPost, "/v1/tasks/"+taskID+"/pr", map[string]any{"fake": true, "run_id": run["id"]})
	if pr.Code != http.StatusCreated {
		t.Fatalf("pr: %d %s", pr.Code, pr.Body.String())
	}
	hook := doJSON(t, h, http.MethodPost, "/v1/pipelines/webhook", map[string]any{
		"task_id": taskID, "name": "ci", "status": "success", "external_url": "https://example.test/ci",
	})
	if hook.Code != http.StatusCreated {
		t.Fatalf("webhook: %d %s", hook.Code, hook.Body.String())
	}
	detail := doJSON(t, h, http.MethodGet, "/v1/tasks/"+taskID+"/detail", nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", detail.Code, detail.Body.String())
	}
	var td struct {
		Artifacts []any `json:"artifacts"`
		Pipelines []any `json:"pipelines"`
		Runs      []any `json:"runs"`
	}
	td = decode[struct {
		Artifacts []any `json:"artifacts"`
		Pipelines []any `json:"pipelines"`
		Runs      []any `json:"runs"`
	}](t, detail)
	if len(td.Artifacts) < 2 || len(td.Pipelines) < 1 || len(td.Runs) < 1 {
		t.Fatalf("detail incomplete: %#v", td)
	}

	sync := doJSON(t, h, http.MethodPost, "/v1/projects/"+proj.ID+"/issues/sync", map[string]any{"fake": true})
	if sync.Code != http.StatusOK {
		t.Fatalf("issues sync: %d %s", sync.Code, sync.Body.String())
	}
	issueHook := doJSON(t, h, http.MethodPost, "/v1/issues/webhook?project_id="+proj.ID, map[string]any{
		"action": "opened",
		"issue":  map[string]any{"number": 9, "title": "From webhook", "html_url": "https://github.com/example/buildbee/issues/9"},
	})
	if issueHook.Code != http.StatusOK {
		t.Fatalf("issues webhook: %d %s", issueHook.Code, issueHook.Body.String())
	}

	rts := doJSON(t, h, http.MethodGet, "/v1/projects/"+proj.ID+"/routines", nil)
	if rts.Code != http.StatusOK {
		t.Fatalf("routines: %d %s", rts.Code, rts.Body.String())
	}
	var rlist struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	rlist = decode[struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}](t, rts)
	if len(rlist.Items) < 1 {
		t.Fatal("expected seeded morning-digest Routine")
	}
	fired := doJSON(t, h, http.MethodPost, "/v1/routines/"+rlist.Items[0].ID+"/run", map[string]string{})
	if fired.Code != http.StatusOK {
		t.Fatalf("routine run: %d %s", fired.Code, fired.Body.String())
	}
}

func TestChannelBotMention(t *testing.T) {
	h := newTestMux(t)
	created := doJSON(t, h, http.MethodPost, "/v1/projects", map[string]string{"name": "Mentions"})
	var proj struct {
		ID      string `json:"id"`
		Members []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Role string `json:"role"`
		} `json:"members"`
		Channels []struct {
			ID string `json:"id"`
		} `json:"channels"`
	}
	proj = decode[struct {
		ID      string `json:"id"`
		Members []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Role string `json:"role"`
		} `json:"members"`
		Channels []struct {
			ID string `json:"id"`
		} `json:"channels"`
	}](t, created)
	var human, scout string
	for _, m := range proj.Members {
		if m.Kind == "human" {
			human = m.ID
		}
		if m.Role == "scout" {
			scout = m.ID
		}
	}
	msg := doJSON(t, h, http.MethodPost, "/v1/channels/"+proj.Channels[0].ID+"/messages", map[string]string{
		"body": "@Scout please triage this", "member_id": human,
	})
	if msg.Code != http.StatusCreated {
		t.Fatalf("mention: %d %s", msg.Code, msg.Body.String())
	}
	body := decode[map[string]any](t, msg)
	ments, _ := body["mentions"].([]any)
	if len(ments) < 1 {
		t.Fatalf("expected mention: %#v", body)
	}
	tasks, _ := body["tasks"].([]any)
	if len(tasks) < 1 {
		t.Fatalf("expected mention Task: %#v", body)
	}
	feed := doJSON(t, h, http.MethodGet, "/v1/projects/"+proj.ID+"/activity?type=mention", nil)
	items := decode[struct {
		Items []any `json:"items"`
	}](t, feed)
	if len(items.Items) < 1 {
		t.Fatal("expected mention Activity")
	}
	_ = scout
}

func TestWebhookSignatureRequired(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "s3cret")
	h := newTestMux(t)
	rec := doJSON(t, h, http.MethodPost, "/v1/issues/webhook?project_id=x", map[string]any{
		"action": "opened",
		"issue":  map[string]any{"number": 1, "title": "no sig", "html_url": "https://example.test/1"},
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
}

func TestDecisionMemoryReuse(t *testing.T) {
	h := newTestMux(t)
	created := doJSON(t, h, http.MethodPost, "/v1/projects", map[string]string{"name": "Memory Project"})
	proj := decode[map[string]any](t, created)
	pid := proj["id"].(string)

	first := doJSON(t, h, http.MethodPost, "/v1/projects/"+pid+"/decisions", map[string]any{
		"prompt": "Ship today?", "options": []string{"yes", "no"}, "recommendation": "yes",
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", first.Code, first.Body.String())
	}
	d1 := decode[map[string]any](t, first)
	ans := doJSON(t, h, http.MethodPost, "/v1/decisions/"+d1["id"].(string)+"/answer", map[string]string{"answer": "yes"})
	if ans.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", ans.Code, ans.Body.String())
	}

	again := doJSON(t, h, http.MethodPost, "/v1/projects/"+pid+"/decisions", map[string]any{
		"prompt": "  SHIP TODAY?!  ", "options": []string{"yes", "no"}, "recommendation": "yes",
	})
	if again.Code != http.StatusCreated {
		t.Fatalf("reuse create: %d %s", again.Code, again.Body.String())
	}
	d2 := decode[map[string]any](t, again)
	if d2["reused"] != true {
		t.Fatalf("expected reused Decision: %#v", d2)
	}
	if d2["answer"] != "yes" {
		t.Fatalf("expected auto-applied answer, got %#v", d2)
	}

	inbox := doJSON(t, h, http.MethodGet, "/v1/projects/"+pid+"/decisions?inbox=1", nil)
	open := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, inbox)
	if len(open.Items) != 0 {
		t.Fatalf("inbox should skip reused/answered: %#v", open.Items)
	}

	mems := doJSON(t, h, http.MethodGet, "/v1/projects/"+pid+"/decisions/memories", nil)
	listed := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, mems)
	if len(listed.Items) < 1 || listed.Items[0]["answer"] != "yes" {
		t.Fatalf("memories: %#v", listed.Items)
	}

	feed := doJSON(t, h, http.MethodGet, "/v1/projects/"+pid+"/activity?type=memory", nil)
	acts := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, feed)
	if len(acts.Items) < 1 {
		t.Fatal("expected memory reuse Activity")
	}
}

func TestNotificationsInbox(t *testing.T) {
	h := newTestMux(t)
	created := doJSON(t, h, http.MethodPost, "/v1/projects", map[string]string{"name": "Notify Project"})
	var proj struct {
		ID      string `json:"id"`
		Members []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Role string `json:"role"`
		} `json:"members"`
		Channels []struct {
			ID string `json:"id"`
		} `json:"channels"`
	}
	proj = decode[struct {
		ID      string `json:"id"`
		Members []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Role string `json:"role"`
		} `json:"members"`
		Channels []struct {
			ID string `json:"id"`
		} `json:"channels"`
	}](t, created)
	var human, scout string
	for _, m := range proj.Members {
		if m.Kind == "human" {
			human = m.ID
		}
		if m.Role == "scout" {
			scout = m.ID
		}
	}

	dec := doJSON(t, h, http.MethodPost, "/v1/projects/"+proj.ID+"/decisions", map[string]any{
		"prompt": "Need a Decision?", "options": []string{"yes", "no"}, "recommendation": "yes",
	})
	if dec.Code != http.StatusCreated {
		t.Fatalf("decision: %d %s", dec.Code, dec.Body.String())
	}

	taskRec := doJSON(t, h, http.MethodPost, "/v1/projects/"+proj.ID+"/tasks?handoff=none", map[string]string{"title": "Owned Task"})
	task := decode[map[string]any](t, taskRec)
	taskID := task["id"].(string)
	doJSON(t, h, http.MethodPost, "/v1/tasks/"+taskID+"/handoffs", map[string]string{
		"from_member_id": scout, "to_member_id": human, "note": "back to you",
	})

	msg := doJSON(t, h, http.MethodPost, "/v1/channels/"+proj.Channels[0].ID+"/messages", map[string]string{
		"body": "@Scout please look", "member_id": human,
	})
	if msg.Code != http.StatusCreated {
		t.Fatalf("mention: %d %s", msg.Code, msg.Body.String())
	}

	hook := doJSON(t, h, http.MethodPost, "/v1/pipelines/webhook", map[string]any{
		"task_id": taskID, "name": "ci", "status": "failure",
	})
	if hook.Code != http.StatusCreated {
		t.Fatalf("pipeline: %d %s", hook.Code, hook.Body.String())
	}

	inbox := doJSON(t, h, http.MethodGet, "/v1/notifications?member_id="+human, nil)
	if inbox.Code != http.StatusOK {
		t.Fatalf("list: %d %s", inbox.Code, inbox.Body.String())
	}
	listed := decode[struct {
		Items  []map[string]any `json:"items"`
		Unread int              `json:"unread"`
	}](t, inbox)
	if listed.Unread < 3 || len(listed.Items) < 3 {
		t.Fatalf("expected decision+handoff+mention+pipeline notifications: %#v", listed)
	}
	kinds := map[string]int{}
	for _, n := range listed.Items {
		kinds[n["kind"].(string)]++
	}
	for _, want := range []string{"decision", "handoff", "mention", "pipeline"} {
		if kinds[want] < 1 {
			t.Fatalf("missing kind %s in %#v", want, kinds)
		}
	}

	firstID := listed.Items[0]["id"].(string)
	read := doJSON(t, h, http.MethodPost, "/v1/notifications/"+firstID+"/read", map[string]string{})
	if read.Code != http.StatusOK {
		t.Fatalf("read: %d %s", read.Code, read.Body.String())
	}
	one := decode[map[string]any](t, read)
	if one["read_at"] == nil {
		t.Fatalf("expected read_at: %#v", one)
	}

	all := doJSON(t, h, http.MethodPost, "/v1/notifications/read-all?member_id="+human, map[string]string{})
	if all.Code != http.StatusOK {
		t.Fatalf("read-all: %d %s", all.Code, all.Body.String())
	}
	after := doJSON(t, h, http.MethodGet, "/v1/notifications?member_id="+human+"&unread=1", nil)
	empty := decode[struct {
		Items  []any `json:"items"`
		Unread int   `json:"unread"`
	}](t, after)
	if empty.Unread != 0 || len(empty.Items) != 0 {
		t.Fatalf("expected empty unread inbox: %#v", empty)
	}
}

func TestMuxKeepsAPIWhenWebMissing(t *testing.T) {
	h := newTestMux(t)
	health := doJSON(t, h, http.MethodGet, "/healthz", nil)
	if health.Code != http.StatusOK {
		t.Fatalf("healthz: %d", health.Code)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("root without UI: %d %s", rec.Code, rec.Body.String())
	}
}

func TestBotRolesHandoffAndAutorun(t *testing.T) {
	h := newTestMux(t)
	created := doJSON(t, h, http.MethodPost, "/v1/projects", map[string]any{"name": "Roles", "auto_run": true})
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var proj struct {
		ID      string `json:"id"`
		AutoRun bool   `json:"auto_run"`
		Members []struct {
			ID           string `json:"id"`
			Kind         string `json:"kind"`
			Role         string `json:"role"`
			Instructions string `json:"instructions"`
			DisplayName  string `json:"display_name"`
		} `json:"members"`
	}
	proj = decode[struct {
		ID      string `json:"id"`
		AutoRun bool   `json:"auto_run"`
		Members []struct {
			ID           string `json:"id"`
			Kind         string `json:"kind"`
			Role         string `json:"role"`
			Instructions string `json:"instructions"`
			DisplayName  string `json:"display_name"`
		} `json:"members"`
	}](t, created)
	if !proj.AutoRun {
		t.Fatal("expected auto_run")
	}
	var human, scout, builder string
	for _, m := range proj.Members {
		switch m.Role {
		case "owner":
			human = m.ID
		case "scout":
			scout = m.ID
			if m.Instructions == "" || m.DisplayName != "Scout" {
				t.Fatalf("scout seed: %#v", m)
			}
		case "builder":
			builder = m.ID
		}
	}
	if human == "" || scout == "" || builder == "" {
		t.Fatalf("missing roles: %#v", proj.Members)
	}

	taskRec := doJSON(t, h, http.MethodPost, "/v1/projects/"+proj.ID+"/tasks?handoff=scout", map[string]string{"title": "Scope unclear?"})
	if taskRec.Code != http.StatusCreated {
		t.Fatalf("task: %d %s", taskRec.Code, taskRec.Body.String())
	}
	task := decode[map[string]any](t, taskRec)
	if task["handoff"] == nil {
		t.Fatal("expected auto-Handoff to Scout")
	}
	taskID := task["id"].(string)
	ho := task["handoff"].(map[string]any)
	done := doJSON(t, h, http.MethodPost, "/v1/handoffs/"+ho["id"].(string)+"/complete", map[string]string{})
	if done.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", done.Code, done.Body.String())
	}
	doneBody := decode[map[string]any](t, done)
	if doneBody["decision"] == nil {
		t.Fatal("expected Scout Decision stub for ambiguous Task")
	}

	builderHO := doJSON(t, h, http.MethodPost, "/v1/tasks/"+taskID+"/handoffs?autorun=1", map[string]string{
		"from_member_id": human, "to_role": "builder", "note": "implement",
	})
	if builderHO.Code != http.StatusCreated {
		t.Fatalf("builder handoff: %d %s", builderHO.Code, builderHO.Body.String())
	}
	body := decode[map[string]any](t, builderHO)
	if body["to_member_id"] != builder {
		t.Fatalf("to_role builder: %#v", body)
	}
	if body["run"] == nil {
		t.Fatal("expected autorun Run for Builder Handoff")
	}
}

func TestDecisionAssigneeNotify(t *testing.T) {
	h := newTestMux(t)
	created := doJSON(t, h, http.MethodPost, "/v1/projects", map[string]string{"name": "Assign"})
	var proj struct {
		ID      string `json:"id"`
		Members []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Role string `json:"role"`
		} `json:"members"`
	}
	proj = decode[struct {
		ID      string `json:"id"`
		Members []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Role string `json:"role"`
		} `json:"members"`
	}](t, created)
	var owner string
	for _, m := range proj.Members {
		if m.Role == "owner" {
			owner = m.ID
		}
	}
	bob := decode[map[string]any](t, doJSON(t, h, http.MethodPost, "/v1/projects/"+proj.ID+"/members", map[string]string{
		"display_name": "Bob", "kind": "human", "role": "member",
	}))
	bobID := bob["id"].(string)

	dec := doJSON(t, h, http.MethodPost, "/v1/projects/"+proj.ID+"/decisions", map[string]any{
		"prompt": "Only Bob?", "options": []string{"yes"}, "assignee_id": bobID,
	})
	if dec.Code != http.StatusCreated {
		t.Fatalf("decision: %d %s", dec.Code, dec.Body.String())
	}
	d := decode[map[string]any](t, dec)
	if d["assignee_id"] != bobID {
		t.Fatalf("assignee: %#v", d)
	}

	ownerInbox := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, doJSON(t, h, http.MethodGet, "/v1/notifications?member_id="+owner, nil))
	for _, n := range ownerInbox.Items {
		if n["kind"] == "decision" {
			t.Fatalf("owner should not get assigned Decision: %#v", ownerInbox.Items)
		}
	}
	bobInbox := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, doJSON(t, h, http.MethodGet, "/v1/notifications?member_id="+bobID, nil))
	found := false
	for _, n := range bobInbox.Items {
		if n["kind"] == "decision" {
			found = true
		}
	}
	if !found {
		t.Fatalf("bob should get Decision: %#v", bobInbox.Items)
	}

	mine := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, doJSON(t, h, http.MethodGet, "/v1/projects/"+proj.ID+"/decisions?inbox=1&mine=1&member_id="+bobID, nil))
	if len(mine.Items) != 1 {
		t.Fatalf("bob inbox: %#v", mine.Items)
	}
	other := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, doJSON(t, h, http.MethodGet, "/v1/projects/"+proj.ID+"/decisions?inbox=1&mine=1&member_id="+owner, nil))
	if len(other.Items) != 0 {
		t.Fatalf("owner mine inbox should skip Bob's Decision: %#v", other.Items)
	}
}

func TestNotificationPreferences(t *testing.T) {
	h := newTestMux(t)
	created := doJSON(t, h, http.MethodPost, "/v1/projects", map[string]string{"name": "Prefs"})
	var proj struct {
		ID      string `json:"id"`
		Members []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"members"`
		Channels []struct {
			ID string `json:"id"`
		} `json:"channels"`
	}
	proj = decode[struct {
		ID      string `json:"id"`
		Members []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"members"`
		Channels []struct {
			ID string `json:"id"`
		} `json:"channels"`
	}](t, created)
	var human string
	for _, m := range proj.Members {
		if m.Kind == "human" {
			human = m.ID
		}
	}
	got := doJSON(t, h, http.MethodGet, "/v1/me/preferences?member_id="+human, nil)
	if got.Code != http.StatusOK {
		t.Fatalf("get prefs: %d %s", got.Code, got.Body.String())
	}
	patch := doJSON(t, h, http.MethodPatch, "/v1/me/preferences?member_id="+human, map[string]any{
		"mute_mentions": true, "mute_routines": true,
	})
	if patch.Code != http.StatusOK {
		t.Fatalf("patch prefs: %d %s", patch.Code, patch.Body.String())
	}
	p := decode[map[string]any](t, patch)
	if p["mute_mentions"] != true || p["mute_routines"] != true {
		t.Fatalf("prefs: %#v", p)
	}

	msg := doJSON(t, h, http.MethodPost, "/v1/channels/"+proj.Channels[0].ID+"/messages", map[string]string{
		"body": "@Scout please", "member_id": human,
	})
	if msg.Code != http.StatusCreated {
		t.Fatalf("mention: %d %s", msg.Code, msg.Body.String())
	}
	inbox := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, doJSON(t, h, http.MethodGet, "/v1/notifications?member_id="+human, nil))
	for _, n := range inbox.Items {
		if n["kind"] == "mention" {
			t.Fatalf("muted mention leaked: %#v", inbox.Items)
		}
	}

	rts := decode[struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}](t, doJSON(t, h, http.MethodGet, "/v1/projects/"+proj.ID+"/routines", nil))
	if len(rts.Items) < 1 {
		t.Fatal("expected routine")
	}
	doJSON(t, h, http.MethodPost, "/v1/routines/"+rts.Items[0].ID+"/run", map[string]string{})
	after := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, doJSON(t, h, http.MethodGet, "/v1/notifications?member_id="+human, nil))
	for _, n := range after.Items {
		if n["kind"] == "routine" {
			t.Fatalf("muted routine leaked: %#v", after.Items)
		}
	}
}

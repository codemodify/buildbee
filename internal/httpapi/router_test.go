package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/config"
	"github.com/gorilla/websocket"
)

func TestHealthzReportsDatabase(t *testing.T) {
	s := newStack(t, Options{})
	if rec := s.call(http.MethodGet, "/healthz", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("healthz: %d", rec.Code)
	}
	s.pool.Close()
	if rec := s.call(http.MethodGet, "/healthz", "", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("healthz without database: %d", rec.Code)
	}
}

func TestIdentityByCookieHeaderOrNobody(t *testing.T) {
	s := newStack(t, Options{})
	if me := ok[obj](t, s.call(http.MethodGet, "/v1/me", "", nil), http.StatusOK); me["person"] != nil {
		t.Fatalf("anonymous: %v", me)
	}
	rec := s.call(http.MethodPost, "/v1/me", "", obj{"name": "Ada"})
	me := ok[struct {
		Person struct{ ID, Name string } `json:"person"`
	}](t, rec, http.StatusOK)
	cookie := rec.Result().Cookies()[0]
	if cookie.Name != personCookie || cookie.Value != me.Person.ID || !cookie.HttpOnly {
		t.Fatalf("cookie: %+v", cookie)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.AddCookie(cookie)
	got := httptest.NewRecorder()
	s.h.ServeHTTP(got, req)
	if !strings.Contains(got.Body.String(), `"name":"Ada"`) {
		t.Fatalf("cookie identity: %s", got.Body.String())
	}
	if byHeader := ok[obj](t, s.call(http.MethodGet, "/v1/me", "ada", nil), http.StatusOK); byHeader["person"].(obj)["id"] != me.Person.ID {
		t.Fatalf("header identity resolves to the same Person: %v", byHeader)
	}

	stale := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	stale.AddCookie(&http.Cookie{Name: personCookie, Value: "00000000-0000-0000-0000-000000000000"})
	cleared := httptest.NewRecorder()
	s.h.ServeHTTP(cleared, stale)
	if c := cleared.Result().Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Fatalf("a cookie for an unknown Person is cleared: %+v", c)
	}
}

func TestAnonymousWritesThatNeedAPersonAre401(t *testing.T) {
	s := newStack(t, Options{})
	p := s.project("Ada", "P")
	rec := s.call(http.MethodPost, "/v1/channels/"+p.Channels[0].ID+"/messages", "", obj{"body": "hi"})
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "POST /v1/me") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}

func TestErrorStatuses(t *testing.T) {
	s := newStack(t, Options{MaxBodyBytes: 1 << 10})
	p := s.project("Ada", "Errors")
	task := ok[obj](t, s.call(http.MethodPost, "/v1/projects/"+p.ID+"/tasks", "Ada", obj{"title": "Is this ready?"}), http.StatusCreated)
	hid := task["handoff"].(obj)["id"].(string)
	for _, tc := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"unknown project", "GET", "/v1/projects/00000000-0000-0000-0000-000000000000", "", 404},
		{"malformed id", "GET", "/v1/projects/not-a-uuid", "", 400},
		{"bad json", "POST", "/v1/projects", `{"name":`, 400},
		{"missing name", "POST", "/v1/projects", `{}`, 400},
		{"bad status", "PATCH", "/v1/tasks/" + task["id"].(string), `{"status":"Done "}`, 400},
		{"too large", "POST", "/v1/projects", `{"name":"` + strings.Repeat("x", 2<<10) + `"}`, 413},
		{"bad cursor", "GET", "/v1/projects/" + p.ID + "/activity?before=-1", "", 400},
	} {
		if rec := s.call(tc.method, tc.path, "Ada", tc.body); rec.Code != tc.want {
			t.Errorf("%s: got %d want %d: %s", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}
	ok[obj](t, s.call(http.MethodPost, "/v1/handoffs/"+hid+"/complete", "Ada", nil), http.StatusOK)
	if rec := s.call(http.MethodPost, "/v1/handoffs/"+hid+"/complete", "Ada", nil); rec.Code != http.StatusConflict {
		t.Fatalf("second completion: %d", rec.Code)
	}
}

func TestMessagesPageByCursor(t *testing.T) {
	s := newStack(t, Options{})
	p := s.project("Ada", "Chat")
	fresh := ok[obj](t, s.call(http.MethodPost, "/v1/projects/"+p.ID+"/channels", "Ada", obj{"name": "chat"}), http.StatusCreated)
	ch := "/v1/channels/" + fresh["id"].(string) + "/messages"
	for _, b := range []string{"one", "two", "three"} {
		ok[obj](t, s.call(http.MethodPost, ch, "Ada", obj{"body": b}), http.StatusCreated)
	}
	type pageJSON struct {
		Items []struct {
			Seq  int64  `json:"seq"`
			Body string `json:"body"`
		} `json:"items"`
		HasMore bool `json:"has_more"`
	}
	latest := ok[pageJSON](t, s.call(http.MethodGet, ch+"?limit=2", "", nil), http.StatusOK)
	if !latest.HasMore || latest.Items[0].Body != "two" || latest.Items[1].Body != "three" {
		t.Fatalf("latest: %+v", latest)
	}
	older := ok[pageJSON](t, s.call(http.MethodGet, ch+"?limit=2&before="+strconv.FormatInt(latest.Items[0].Seq, 10), "", nil), http.StatusOK)
	if older.HasMore || len(older.Items) != 1 || older.Items[0].Body != "one" {
		t.Fatalf("older: %+v", older)
	}
}

func TestCrossOriginWritesRefused(t *testing.T) {
	s := newStack(t, Options{})
	req := httptest.NewRequest(http.MethodPost, "http://buildbee.lan/v1/projects", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set(asHeader, "Ada")
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "http://buildbee.lan/v1/projects", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Origin", "http://buildbee.lan")
	req.Header.Set(asHeader, "Ada")
	rec = httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("same-origin POST: %d %s", rec.Code, rec.Body.String())
	}
}

func TestNULAndMultibyteTextArePostgresSafe(t *testing.T) {
	s := newStack(t, Options{})
	p := s.project("Ada", "Text")
	msg := ok[obj](t, s.call(http.MethodPost, "/v1/channels/"+p.Channels[0].ID+"/messages", "Ada",
		`{"body":"@Scout `+strings.Repeat("\u65e5\u672c\u8a9e", 50)+` a\u0000b"}`), http.StatusCreated)
	if tasks := msg["tasks"].([]any); len(tasks) != 1 {
		t.Fatalf("mention with long CJK text must create its Task: %v", msg)
	}
	if !strings.Contains(msg["body"].(string), "a\u2400b") {
		t.Fatalf("NUL stored as U+2400: %q", msg["body"])
	}
}

func TestWebhooks(t *testing.T) {
	s := newStack(t, Options{})
	p := s.project("Ada", "Hooks")
	task := ok[obj](t, s.call(http.MethodPost, "/v1/projects/"+p.ID+"/tasks?handoff=none", "Ada", obj{"title": "x"}), http.StatusCreated)
	tid := task["id"].(string)
	pl := ok[obj](t, s.call(http.MethodPost, "/v1/pipelines/webhook", "", obj{"task_id": tid,
		"check_run": obj{"name": "test", "conclusion": "failure", "html_url": "https://ci/1"}}), http.StatusCreated)
	if pl["status"] != "failure" || pl["name"] != "test" || pl["external_url"] != "https://ci/1" {
		t.Fatalf("check_run mapping: %v", pl)
	}
	issue := ok[obj](t, s.call(http.MethodPost, "/v1/issues/webhook?project_id="+p.ID, "",
		obj{"action": "opened", "issue": obj{"number": 3, "title": "Crash", "html_url": "https://gh/3"}}), http.StatusOK)
	if issue["issue_number"].(float64) != 3 {
		t.Fatalf("issue: %v", issue)
	}
	if rec := s.call(http.MethodPost, "/v1/issues/webhook?project_id="+p.ID, "",
		obj{"action": "opened", "issue": obj{"number": 0, "title": "zero"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("issue number 0: %d", rec.Code)
	}

	signed := newStack(t, Options{GitHub: config.GitHub{WebhookSecret: "s3cret"}})
	if rec := signed.call(http.MethodPost, "/v1/issues/webhook?project_id=x", "", obj{"issue": obj{"number": 1}}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned webhook with a secret configured: %d", rec.Code)
	}
}

func TestRunSliceOverHTTP(t *testing.T) {
	s := newStack(t, Options{})
	p := s.project("Ada", "Slice")
	task := ok[obj](t, s.call(http.MethodPost, "/v1/projects/"+p.ID+"/tasks", "Ada",
		obj{"title": "Add caching", "body": "Cache GET /v1/projects", "handoff_role": "none"}), http.StatusCreated)
	tid := task["id"].(string)
	h := ok[obj](t, s.call(http.MethodPost, "/v1/tasks/"+tid+"/handoffs?autorun=1", "Ada", obj{"to_role": "builder", "note": "go"}), http.StatusCreated)
	run := h["run"].(obj)
	rid := run["id"].(string)
	if run["status"] != "pending" || run["bot_member_id"] != p.role("builder") {
		t.Fatalf("autorun: %v", run)
	}

	worker := func(method, path string, body any) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(mustJSON(t, body)))
		req.Header.Set(workerHeader, "w1")
		rec := httptest.NewRecorder()
		s.h.ServeHTTP(rec, req)
		return rec
	}
	if rec := worker(http.MethodPost, "/v1/runs/"+rid+"/events", obj{"kind": "token", "payload": obj{}}); rec.Code != http.StatusConflict {
		t.Fatalf("writing to an unclaimed Run: %d", rec.Code)
	}
	if rec := worker(http.MethodPost, "/v1/worker/claim", obj{"agents": []string{"fake"}}); rec.Code != http.StatusNoContent {
		t.Fatalf("a Run for any agent is not for the fake agent: %d %s", rec.Code, rec.Body)
	}
	claim := ok[obj](t, worker(http.MethodPost, "/v1/worker/claim", obj{"agents": []string{"claude"}, "wait_seconds": 1}), http.StatusOK)
	if c := claim["run"].(obj); c["id"] != rid || c["status"] != "running" || c["worker"] != "w1" ||
		!strings.Contains(c["prompt"].(string), "Add caching") || claim["bot"].(obj)["id"] != p.role("builder") {
		t.Fatalf("claim: %v", claim)
	}
	hb := ok[obj](t, worker(http.MethodPost, "/v1/runs/"+rid+"/heartbeat", nil), http.StatusOK)
	if hb["status"] != "running" {
		t.Fatalf("heartbeat: %v", hb)
	}
	if rec := s.call(http.MethodPost, "/v1/runs/"+rid+"/heartbeat", "Ada", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("people do not heartbeat: %d", rec.Code)
	}
	ok[obj](t, worker(http.MethodPost, "/v1/runs/"+rid+"/events", obj{"kind": "token", "payload": obj{"text": "working"}}), http.StatusCreated)
	ok[obj](t, worker(http.MethodPatch, "/v1/runs/"+rid, obj{"status": "succeeded", "detail": "exit 0"}), http.StatusOK)
	art := ok[obj](t, worker(http.MethodPost, "/v1/tasks/"+tid+"/artifacts", obj{"kind": "log", "name": "acp.log", "body": "full log", "run_id": rid}), http.StatusCreated)
	if rec := worker(http.MethodPost, "/v1/runs/"+rid+"/events", obj{"kind": "log", "payload": obj{}}); rec.Code != http.StatusConflict {
		t.Fatalf("event after finish: %d", rec.Code)
	}

	detail := ok[struct {
		Body      string `json:"body"`
		Handoffs  []obj  `json:"handoffs"`
		Runs      []obj  `json:"runs"`
		Artifacts []obj  `json:"artifacts"`
	}](t, s.call(http.MethodGet, "/v1/tasks/"+tid+"/detail", "", nil), http.StatusOK)
	if detail.Body != "Cache GET /v1/projects" || len(detail.Handoffs) != 1 || detail.Runs[0]["status"] != "succeeded" {
		t.Fatalf("detail: %+v", detail)
	}
	if a := detail.Artifacts[0]; a["body"] != nil || a["size"].(float64) != 8 {
		t.Fatalf("artifact listing carries size, not body: %v", a)
	}
	full := ok[obj](t, s.call(http.MethodGet, "/v1/artifacts/"+art["id"].(string), "", nil), http.StatusOK)
	if full["body"] != "full log" {
		t.Fatalf("artifact body: %v", full)
	}
	events := ok[struct{ Items []obj }](t, s.call(http.MethodGet, "/v1/runs/"+rid+"/events", "", nil), http.StatusOK)
	if len(events.Items) != 4 { // queued, claimed, token, succeeded
		t.Fatalf("events: %+v", events.Items)
	}
}

func TestWebSocketStreamsThroughTheStack(t *testing.T) {
	s := newStack(t, Options{})
	srv := httptest.NewServer(s.h)
	t.Cleanup(srv.Close)
	p := s.project("Ada", "Live")
	ch := ok[obj](t, s.call(http.MethodPost, "/v1/projects/"+p.ID+"/channels", "Ada", obj{"name": "live"}), http.StatusCreated)["id"].(string)
	ok[obj](t, s.call(http.MethodPost, "/v1/channels/"+ch+"/messages", "Ada", obj{"body": "before"}), http.StatusCreated)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(obj{"op": "subscribe", "topic": "channel:" + ch, "after": 0}); err != nil {
		t.Fatal(err)
	}
	type frame struct {
		Topic  string `json:"topic"`
		Cursor int64  `json:"cursor"`
		Data   struct {
			Body string `json:"body"`
		} `json:"data"`
	}
	next := func() frame {
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var f frame
		if err := conn.ReadJSON(&f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	if f := next(); f.Data.Body != "before" {
		t.Fatalf("replay: %+v", f)
	}
	ok[obj](t, s.call(http.MethodPost, "/v1/channels/"+ch+"/messages", "Ada", obj{"body": "live"}), http.StatusCreated)
	if f := next(); f.Data.Body != "live" || f.Topic != "channel:"+ch {
		t.Fatalf("live: %+v", f)
	}

	// Pages on another site cannot open streams through a user's browser.
	hdr := http.Header{"Origin": {"https://evil.example"}}
	if _, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/ws", hdr); err == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin WebSocket must be refused, got %v", err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMembersListsTheServer(t *testing.T) {
	s := newStack(t, Options{})
	s.project("Ada", "Chat")
	type rosterJSON struct {
		People []struct {
			Name     string `json:"name"`
			Projects []struct {
				ProjectName string `json:"project_name"`
			} `json:"projects"`
		} `json:"people"`
		Bots []struct {
			DisplayName string `json:"display_name"`
			ProjectName string `json:"project_name"`
		} `json:"bots"`
		Events []struct {
			Name   string `json:"name"`
			Action string `json:"action"`
		} `json:"events"`
	}
	r := ok[rosterJSON](t, s.call(http.MethodGet, "/v1/members", "", nil), http.StatusOK)
	if len(r.People) != 1 || r.People[0].Name != "Ada" || len(r.People[0].Projects) != 1 || r.People[0].Projects[0].ProjectName != "Chat" {
		t.Fatalf("people: %+v", r.People)
	}
	if len(r.Bots) == 0 || r.Bots[0].ProjectName != "Chat" {
		t.Fatalf("bots: %+v", r.Bots)
	}
	if len(r.Events) != 1 || r.Events[0].Name != "Ada" || r.Events[0].Action != "created" {
		t.Fatalf("events: %+v", r.Events)
	}
}

func TestUsageWithNoRunsIsEmptyNotNull(t *testing.T) {
	s := newStack(t, Options{})
	s.project("Ada", "Chat")
	rec := s.call(http.MethodGet, "/v1/usage", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"by_agent":[]`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestFilesDownloadSafely(t *testing.T) {
	s := newStack(t, Options{})
	p := s.project("Ada", "Files")
	ch := p.Channels[0].ID
	upload := func(name, body string) string {
		req := httptest.NewRequest(http.MethodPost, "/v1/channels/"+ch+"/files?name="+name, strings.NewReader(body))
		req.Header.Set(asHeader, "Ada")
		rec := httptest.NewRecorder()
		s.h.ServeHTTP(rec, req)
		return ok[obj](t, rec, http.StatusCreated)["id"].(string)
	}
	page := upload("evil.html", "<html><script>alert(1)</script></html>")
	img := upload("dot.gif", "GIF89a\x01\x00\x01\x00\x00\x00\x00;")
	ok[obj](t, s.call(http.MethodPost, "/v1/channels/"+ch+"/messages", "Ada", obj{"body": "look", "file_ids": []string{page, img}}), http.StatusCreated)

	rec := s.call(http.MethodGet, "/v1/files/"+page, "Bob", nil)
	h := rec.Header()
	if rec.Code != 200 || h.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment;") ||
		h.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("html is a download: %d %v", rec.Code, h)
	}
	rec = s.call(http.MethodGet, "/v1/files/"+img, "Bob", nil)
	if rec.Header().Get("Content-Type") != "image/gif" || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "inline;") {
		t.Fatalf("an image shows inline: %v", rec.Header())
	}
	again := httptest.NewRequest(http.MethodGet, "/v1/files/"+img, nil)
	again.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	rec = httptest.NewRecorder()
	s.h.ServeHTTP(rec, again)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("cached: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/channels/"+ch+"/files?name=x", strings.NewReader("x"))
	req.ContentLength = -1
	req.Header.Set(asHeader, "Ada")
	rec = httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusLengthRequired {
		t.Fatalf("no length: %d", rec.Code)
	}
}

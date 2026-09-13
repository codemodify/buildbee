package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	NewMux().ServeHTTP(rec, req)

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
	NewMux().ServeHTTP(rec, req)
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
	NewMux().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusNotFound)
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
	h := NewMux()

	created := doJSON(t, h, http.MethodPost, "/v1/projects", map[string]string{"name": "Hive"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", created.Code, created.Body.String())
	}
	var proj struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Members []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"members"`
		Channels []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"channels"`
	}
	proj = decode[struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Members []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"members"`
		Channels []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"channels"`
	}](t, created)
	if proj.ID == "" || len(proj.Members) < 2 || len(proj.Channels) < 1 {
		t.Fatalf("seed: %#v", proj)
	}
	var human, bot string
	for _, m := range proj.Members {
		if m.Kind == "human" {
			human = m.ID
		}
		if m.Kind == "bot" {
			bot = m.ID
		}
	}
	channelID := proj.Channels[0].ID

	got := doJSON(t, h, http.MethodGet, "/v1/projects/"+proj.ID, nil)
	if got.Code != http.StatusOK {
		t.Fatalf("get project: %d", got.Code)
	}

	msg := doJSON(t, h, http.MethodPost, "/v1/channels/"+channelID+"/messages", map[string]string{"body": "hello hive", "member_id": human})
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
}

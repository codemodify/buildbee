package runtime

import "testing"

func TestStartRunStub(t *testing.T) {
	s := NewSupervisor()
	status, err := s.StartRun("run-placeholder")
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusPending {
		t.Fatalf("status: got %q want %q", status, StatusPending)
	}
}

package provider

import (
	"context"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestProviderExecutesRegisteredCapabilityAndCommitsResult(t *testing.T) {
	store := DiskStore{Dir: t.TempDir()}
	service, err := New(store, HashExecutor{}, IdentityExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte("scientific-work-unit")
	request := Request{
		JobID: types.JobID(types.HashBytes("job", []byte("provider"))),
		WorkloadHash: types.HashBytes("workload", []byte("sha256")),
		InputRoot: InputRoot(input), Capability: "sha256", Input: input,
	}
	wire, err := request.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	responseWire, err := service.Handle(context.Background(), wire)
	if err != nil {
		t.Fatal(err)
	}
	response, err := ParseResponse(responseWire)
	if err != nil {
		t.Fatal(err)
	}
	if response.JobID != request.JobID || !response.Stored || ResultRoot(response.Output) != response.ResultRoot {
		t.Fatalf("unexpected provider response: %+v", response)
	}
	stored, err := store.Get(response.ResultRoot)
	if err != nil || ResultRoot(stored) != response.ResultRoot {
		t.Fatal("content-addressed result was not persisted")
	}
}

func TestProviderRejectsUnknownExecutorAndTamperedInput(t *testing.T) {
	service, err := New(DiskStore{Dir: t.TempDir()}, HashExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	base := Request{JobID: types.JobID(types.HashBytes("job", []byte("reject"))), WorkloadHash: types.HashBytes("workload", []byte("reject")), InputRoot: InputRoot([]byte("good")), Capability: "unknown", Input: []byte("good")}
	if _, err := service.Execute(context.Background(), base); err != ErrExecutor {
		t.Fatalf("expected unknown executor rejection, got %v", err)
	}
	base.Capability = "sha256"
	base.Input = []byte("tampered")
	if _, err := service.Execute(context.Background(), base); err != ErrInputRoot {
		t.Fatalf("expected input-root rejection, got %v", err)
	}
}

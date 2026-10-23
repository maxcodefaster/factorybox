//go:build linux && factorybox_worker_probe

package devx_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"time"

	"factorybox/internal/lifecycle"
	providerfixture "factorybox/tests/provider-fixture"
)

func (f *fixture) canceledExternalProvider(ctx context.Context, view lifecycle.View) func() {
	f.t.Helper()
	lease, err := providerfixture.Acquire(ctx, "/output", "continue", providerfixture.Target{
		RunOwner: os.Getenv("FACTORYBOX_DEVX_RUN"), Sandbox: view.Record.ID, Owner: view.Record.Owner,
		Runtime: view.Runtime.ID, PID: view.Runtime.PID, Created: view.Runtime.Created,
	})
	if err != nil {
		f.t.Fatal("attach exact canceled-agent inference provider", err)
	}
	return func() {
		closed, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := lease.Close(closed); err != nil {
			f.t.Fatal("canceled-agent provider did not exit before capture", err)
		}
	}
}

func (f *fixture) continueCanceledExternalAgent(view lifecycle.View, saved canceledAgent) canceledContinuation {
	f.t.Helper()
	output := f.canceledAgentOutput(view, "continue")
	var observed canceledContinuation
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&observed); err != nil {
		f.t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(saved.ToolError.Message))
	if saved.ToolCallID == "" || observed.ToolCallID != saved.ToolCallID ||
		observed.SessionID != saved.SessionID || observed.CallSHA256 != saved.CallSHA256 ||
		len(observed.Token) != 32 || observed.Ack != "FACTORYBOX_CANCEL_ACK_"+observed.Token ||
		!observed.Completed || !observed.ToolErrorConsumed || observed.Launches != 1 ||
		observed.ContentSHA256 != "" || observed.ErrorSHA256 != hex.EncodeToString(digest[:]) ||
		!strings.HasPrefix(observed.UserMessageID, "msg_") || !strings.HasPrefix(observed.AssistantMessageID, "msg_") ||
		!strings.HasPrefix(observed.IdleMessageID, "msg_") {
		f.t.Fatal("missing real external prompt/error acknowledgement after cancellation", observed)
	}
	if actual := f.canceledAgent(view, "verify"); !reflect.DeepEqual(actual, saved) {
		f.t.Fatal("external prompt changed the original cancellation", actual, saved)
	}
	return observed
}

package platform

import (
	"os"
	"testing"
)

func TestCurrentExecutionWorker(t *testing.T) {
	if os.Getenv("FACTORYBOX_EXECUTION_NATIVE") != "1" {
		t.Skip("requires the declared current native execution worker")
	}
	info, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	if err := info.CheckExecution(); err != nil {
		t.Fatal(err)
	}
}

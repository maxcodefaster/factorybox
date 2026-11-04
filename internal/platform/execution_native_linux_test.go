package platform

import (
	"os"
	"testing"
)

func TestCurrentExecutionWorker(t *testing.T) {
	if os.Getenv("FACTORYBOX_EXECUTION_NATIVE") != "1" {
		t.Skip("requires the declared current native execution worker")
	}
	report, err := CheckHost()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("qualification=%s kernel=%s capabilities=%+v", report.Qualification, report.Platform.Kernel, report.Capabilities)
}

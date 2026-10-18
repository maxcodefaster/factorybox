package platform

import (
	"os"
	"testing"
)

func TestCurrentReferenceWorker(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE") != "1" {
		t.Skip("requires the declared native Linux worker")
	}
	info, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	if err := info.CheckReference(); err != nil {
		t.Fatal(err)
	}
}

package sysboxFsGrpc

import (
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	pb "github.com/nestybox/sysbox-ipc/sysboxFsGrpc/sysboxFsProtobuf"
)

func TestExperimentForkSourceSurvivesWireEncoding(t *testing.T) {
	data := &ContainerData{Id: "copy", ForkSourceId: "source", Ctime: time.Unix(123, 0)}
	message, err := containerDataToPbData(data)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var decoded pb.ContainerData
	if err := proto.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	restored, err := pbDatatoContainerData(&decoded)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Id != data.Id || restored.ForkSourceId != data.ForkSourceId || !restored.Ctime.Equal(data.Ctime) {
		t.Fatal("fork source or identity/creation time lost in the client/server protocol")
	}
}

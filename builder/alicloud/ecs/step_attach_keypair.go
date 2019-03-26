package ecs

import (
	"context"
	"fmt"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/errors"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

type stepAttachKeyPair struct {
}

func (s *stepAttachKeyPair) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packer.Ui)
	client := state.Get("client").(*ecs.Client)
	config := state.Get("config").(*Config)
	instance := state.Get("instance").(ecs.Instance)
	timeoutPoint := time.Now().Add(120 * time.Second)
	keyPairName := config.Comm.SSHKeyPairName
	if keyPairName == "" {
		return multistep.ActionContinue
	}
	for {
		attachKeyPairReq := ecs.CreateAttachKeyPairRequest()

		attachKeyPairReq.RegionId = config.AlicloudRegion
		attachKeyPairReq.KeyPairName = keyPairName
		attachKeyPairReq.InstanceIds = "[\"" + instance.InstanceId + "\"]"
		_, err := client.AttachKeyPair(attachKeyPairReq)
		if err != nil {
			e, _ := err.(errors.Error)
			if (!(e.ErrorCode() == "MissingParameter" || e.ErrorCode() == "DependencyViolation.WindowsInstance" ||
				e.ErrorCode() == "InvalidKeyPairName.NotFound" || e.ErrorCode() == "InvalidRegionId.NotFound")) &&
				time.Now().Before(timeoutPoint) {
				time.Sleep(5 * time.Second)
				continue
			}
			err := fmt.Errorf("Error attaching keypair %s to instance %s : %s",
				keyPairName, instance.InstanceId, err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		break
	}

	ui.Message(fmt.Sprintf("Attach keypair %s to instance: %s", keyPairName, instance.InstanceId))

	return multistep.ActionContinue
}

func (s *stepAttachKeyPair) Cleanup(state multistep.StateBag) {
	client := state.Get("client").(*ecs.Client)
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)
	instance := state.Get("instance").(ecs.Instance)
	keyPairName := config.Comm.SSHKeyPairName
	if keyPairName == "" {
		return
	}
	detachKeyPairReq := ecs.CreateDetachKeyPairRequest()

	detachKeyPairReq.RegionId = config.AlicloudRegion
	detachKeyPairReq.KeyPairName = keyPairName
	detachKeyPairReq.InstanceIds = "[\"" + instance.InstanceId + "\"]"
	_, err := client.DetachKeyPair(detachKeyPairReq)
	if err != nil {
		err := fmt.Errorf("Error Detaching keypair %s to instance %s : %s", keyPairName,
			instance.InstanceId, err)
		state.Put("error", err)
		ui.Error(err.Error())
		return
	}

	ui.Message(fmt.Sprintf("Detach keypair %s from instance: %s", keyPairName, instance.InstanceId))

}

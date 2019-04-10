package ecs

import (
	"context"
	"fmt"
	"strconv"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

type stepStopAlicloudInstance struct {
	ForceStop   bool
	DisableStop bool
}

func (s *stepStopAlicloudInstance) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	client := state.Get("client").(*ecs.Client)
	instance := state.Get("instance").(ecs.Instance)
	ui := state.Get("ui").(packer.Ui)

	if !s.DisableStop {
		ui.Say(fmt.Sprintf("Stopping instance: %s", instance.InstanceId))
		stopInstanceReq := ecs.CreateStopInstanceRequest()

		stopInstanceReq.InstanceId = instance.InstanceId
		stopInstanceReq.ForceStop = requests.Boolean(strconv.FormatBool(s.ForceStop))
		if _, err := client.StopInstance(stopInstanceReq); err != nil {
			err := fmt.Errorf("Error stopping alicloud instance: %s", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
	}

	ui.Say(fmt.Sprintf("Waiting instance stopped: %s", instance.InstanceId))

	waitForParam := AlicloudAccessConfig{AlicloudRegion: instance.RegionId, WaitForInstanceId: instance.InstanceId, WaitForStatus: "Stopped"}
	if err := WaitForExpected(waitForParam.DescribeInstances, waitForParam.EvaluatorInstance, ALICLOUD_DEFAULT_TIMEOUT); err != nil {
		err := fmt.Errorf("Error waiting for alicloud instance to stop: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *stepStopAlicloudInstance) Cleanup(multistep.StateBag) {
	// No cleanup...
}

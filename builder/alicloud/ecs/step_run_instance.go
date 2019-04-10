package ecs

import (
	"context"
	"fmt"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

type stepRunAlicloudInstance struct {
}

func (s *stepRunAlicloudInstance) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)
	instance := state.Get("instance").(ecs.Instance)
	startInstanceReq := ecs.CreateStartInstanceRequest()

	startInstanceReq.InstanceId = instance.InstanceId
	if _, err := client.StartInstance(startInstanceReq); err != nil {
		err := fmt.Errorf("Error starting instance: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	ui.Say(fmt.Sprintf("Starting instance: %s", instance.InstanceId))

	waitForParam := AlicloudAccessConfig{AlicloudRegion: instance.RegionId, WaitForInstanceId: instance.InstanceId, WaitForStatus: "Running"}
	if err := WaitForExpected(waitForParam.DescribeInstances, waitForParam.EvaluatorInstance, ALICLOUD_DEFAULT_TIMEOUT); err != nil {
		err := fmt.Errorf("Timeout waiting for instance to start: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *stepRunAlicloudInstance) Cleanup(state multistep.StateBag) {
	_, cancelled := state.GetOk(multistep.StateCancelled)
	_, halted := state.GetOk(multistep.StateHalted)
	if cancelled || halted {
		ui := state.Get("ui").(packer.Ui)
		client := state.Get("client").(*ecs.Client)
		instance := state.Get("instance").(ecs.Instance)
		describeInstancesReq := ecs.CreateDescribeInstancesRequest()

		describeInstancesReq.InstanceIds = "[\"" + instance.InstanceId + "\"]"
		instances, _ := client.DescribeInstances(describeInstancesReq)
		instanceAttribute := instances.Instances.Instance[0]
		if instanceAttribute.Status == "Starting" || instanceAttribute.Status == "Running" {
			stopInstanceReq := ecs.CreateStopInstanceRequest()

			stopInstanceReq.InstanceId = instance.InstanceId
			stopInstanceReq.ForceStop = "true"
			if _, err := client.StopInstance(stopInstanceReq); err != nil {
				ui.Say(fmt.Sprintf("Error stopping instance %s, it may still be around %s", instance.InstanceId, err))
				return
			}
			waitForParam := AlicloudAccessConfig{AlicloudRegion: instance.RegionId, WaitForInstanceId: instance.InstanceId, WaitForStatus: "Stopped"}
			if err := WaitForExpected(waitForParam.DescribeInstances, waitForParam.EvaluatorInstance, ALICLOUD_DEFAULT_TIMEOUT); err != nil {
				ui.Say(fmt.Sprintf("Error stopping instance %s, it may still be around %s", instance.InstanceId, err))
			}
		}
	}
}

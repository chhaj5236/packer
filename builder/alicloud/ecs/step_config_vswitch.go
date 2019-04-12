package ecs

import (
	"context"
	errorsNew "errors"
	"fmt"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/errors"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

type stepConfigAlicloudVSwitch struct {
	VSwitchId   string
	ZoneId      string
	isCreate    bool
	CidrBlock   string
	VSwitchName string
}

func (s *stepConfigAlicloudVSwitch) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)
	vpcId := state.Get("vpcid").(string)
	config := state.Get("config").(*Config)

	if len(s.VSwitchId) != 0 {
		describeVSwitchesReq := ecs.CreateDescribeVSwitchesRequest()

		describeVSwitchesReq.VpcId = vpcId
		describeVSwitchesReq.VSwitchId = s.VSwitchId
		describeVSwitchesReq.ZoneId = s.ZoneId
		vswitchs, err := client.DescribeVSwitches(describeVSwitchesReq)
		if err != nil {
			ui.Say(fmt.Sprintf("Failed querying vswitch: %s", err))
			state.Put("error", err)
			return multistep.ActionHalt
		}
		vswitch := vswitchs.VSwitches.VSwitch
		if len(vswitch) > 0 {
			state.Put("vswitchid", vswitch[0].VSwitchId)
			s.isCreate = false
			return multistep.ActionContinue
		}
		s.isCreate = false
		message := fmt.Sprintf("The specified vswitch {%s} doesn't exist.", s.VSwitchId)
		state.Put("error", errorsNew.New(message))
		ui.Say(message)
		return multistep.ActionHalt

	}
	if s.ZoneId == "" {

		describeZonesReq := ecs.CreateDescribeZonesRequest()

		describeZonesReq.RegionId = config.AlicloudRegion
		dzones, err := client.DescribeZones(describeZonesReq)
		if err != nil {
			ui.Say(fmt.Sprintf("Query for available zones failed: %s", err))
			state.Put("error", err)
			return multistep.ActionHalt
		}
		var instanceTypes []string
		zones := dzones.Zones.Zone
		for _, zone := range zones {
			isVSwitchSupported := false
			for _, resourceType := range zone.AvailableResourceCreation.ResourceTypes {
				if resourceType == "VSwitch" {
					isVSwitchSupported = true
				}
			}
			if isVSwitchSupported {
				for _, instanceType := range zone.AvailableInstanceTypes.InstanceTypes {
					if instanceType == config.InstanceType {
						s.ZoneId = zone.ZoneId
						break
					}
					instanceTypes = append(instanceTypes, instanceType)
				}
			}
		}

		if s.ZoneId == "" {
			if len(instanceTypes) > 0 {
				ui.Say(fmt.Sprintf("The instance type %s isn't available in this region."+
					"\n You can either change the instance to one of following: %v \n"+
					"or choose another region.", config.InstanceType, instanceTypes))

				state.Put("error", fmt.Errorf("The instance type %s isn't available in this region."+
					"\n You can either change the instance to one of following: %v \n"+
					"or choose another region.", config.InstanceType, instanceTypes))
				return multistep.ActionHalt
			} else {
				ui.Say(fmt.Sprintf("The instance type %s isn't available in this region."+
					"\n You can change to other regions.", config.InstanceType))

				state.Put("error", fmt.Errorf("The instance type %s isn't available in this region."+
					"\n You can change to other regions.", config.InstanceType))
				return multistep.ActionHalt
			}
		}
	}
	if config.CidrBlock == "" {
		s.CidrBlock = "172.16.0.0/24" //use the default CirdBlock
	}
	ui.Say("Creating vswitch...")
	createVSwitchReq := ecs.CreateCreateVSwitchRequest()

	createVSwitchReq.CidrBlock = s.CidrBlock
	createVSwitchReq.ZoneId = s.ZoneId
	createVSwitchReq.VpcId = vpcId
	createVSwitchReq.VSwitchName = s.VSwitchName
	vswitchRes, err := client.CreateVSwitch(createVSwitchReq)
	if err != nil {
		state.Put("error", err)
		ui.Say(fmt.Sprintf("Create vswitch failed %v", err))
		return multistep.ActionHalt
	}

	waitForParam := AlicloudAccessConfig{AlicloudRegion: config.AlicloudRegion, WaitForVpcId: vpcId, WaitForVSwitchId: vswitchRes.VSwitchId, WaitForStatus: "Available"}
	if err := WaitForExpected(waitForParam.DescribeVSwitches, waitForParam.EvaluatorVSwitches, ALICLOUD_DEFAULT_TIMEOUT); err != nil {
		state.Put("error", err)
		ui.Error(fmt.Sprintf("Timeout waiting for vswitch to become available: %v", err))
		return multistep.ActionHalt
	}

	state.Put("vswitchid", vswitchRes.VSwitchId)
	s.isCreate = true
	s.VSwitchId = vswitchRes.VSwitchId
	return multistep.ActionContinue
}

func (s *stepConfigAlicloudVSwitch) Cleanup(state multistep.StateBag) {
	if !s.isCreate {
		return
	}

	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)
	cleanUpMessage(state, "vSwitch")
	timeoutPoint := time.Now().Add(10 * time.Second)
	for {
		deleteVSwitchReq := ecs.CreateDeleteVSwitchRequest()

		deleteVSwitchReq.VSwitchId = s.VSwitchId
		if _, err := client.DeleteVSwitch(deleteVSwitchReq); err != nil {
			e := err.(errors.Error)
			if (e.ErrorCode() == "IncorrectVSwitchStatus" || e.ErrorCode() == "DependencyViolation" ||
				e.ErrorCode() == "DependencyViolation.HaVip" ||
				e.ErrorCode() == "IncorrectRouteEntryStatus") && time.Now().Before(timeoutPoint) {
				time.Sleep(1 * time.Second)
				continue
			}
			ui.Error(fmt.Sprintf("Error deleting vswitch, it may still be around: %s", err))
			return
		}
		break
	}
}

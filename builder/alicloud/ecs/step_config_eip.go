package ecs

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

type stepConfigAlicloudEIP struct {
	AssociatePublicIpAddress bool
	RegionId                 string
	InternetChargeType       string
	InternetMaxBandwidthOut  int
	allocatedId              string
	SSHPrivateIp             bool
}

func (s *stepConfigAlicloudEIP) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)
	instance := state.Get("instance").(ecs.Instance)

	if s.SSHPrivateIp {
		ipaddress := instance.VpcAttributes.PrivateIpAddress.IpAddress
		if len(ipaddress) == 0 {
			ui.Say("Failed to get private ip of instance")
			return multistep.ActionHalt
		}
		state.Put("ipaddress", ipaddress[0])
		return multistep.ActionContinue
	}

	ui.Say("Allocating eip")
	allocateEipAddressReq := ecs.CreateAllocateEipAddressRequest()

	allocateEipAddressReq.RegionId = instance.RegionId
	allocateEipAddressReq.InternetChargeType = s.InternetChargeType
	allocateEipAddressReq.Bandwidth = strconv.Itoa(s.InternetMaxBandwidthOut)
	response, err := client.AllocateEipAddress(allocateEipAddressReq)
	if err != nil {
		state.Put("error", err)
		ui.Say(fmt.Sprintf("Error allocating eip: %s", err))
		return multistep.ActionHalt
	}
	s.allocatedId = response.AllocationId

	if err := WaitForEip(instance.RegionId, response.AllocationId, "Available", ALICLOUD_DEFAULT_SHORT_TIMEOUT); err != nil {
		state.Put("error", err)
		ui.Say(fmt.Sprintf("Error allocating eip: %s", err))
		return multistep.ActionHalt
	}

	associateEipAddressReq := ecs.CreateAssociateEipAddressRequest()

	associateEipAddressReq.AllocationId = response.AllocationId
	associateEipAddressReq.InstanceId = instance.InstanceId
	if _, err := client.AssociateEipAddress(associateEipAddressReq); err != nil {
		state.Put("error", err)
		ui.Say(fmt.Sprintf("Error binding eip: %s", err))
		return multistep.ActionHalt
	}

	if err := WaitForEip(instance.RegionId, response.AllocationId, "InUse", ALICLOUD_DEFAULT_SHORT_TIMEOUT); err != nil {
		state.Put("error", err)
		ui.Say(fmt.Sprintf("Error associating eip: %s", err))
		return multistep.ActionHalt
	}
	ui.Say(fmt.Sprintf("Allocated eip %s", response.EipAddress))
	state.Put("ipaddress", response.EipAddress)
	return multistep.ActionContinue
}

func (s *stepConfigAlicloudEIP) Cleanup(state multistep.StateBag) {
	if len(s.allocatedId) == 0 {
		return
	}

	client := state.Get("client").(*ecs.Client)
	instance := state.Get("instance").(ecs.Instance)
	ui := state.Get("ui").(packer.Ui)

	message(state, "EIP")

	unassociateEipAddressReq := ecs.CreateUnassociateEipAddressRequest()

	unassociateEipAddressReq.AllocationId = s.allocatedId
	unassociateEipAddressReq.InstanceId = instance.InstanceId
	if _, err := client.UnassociateEipAddress(unassociateEipAddressReq); err != nil {
		ui.Say(fmt.Sprintf("Failed to unassociate eip."))
	}

	if err := WaitForEip(instance.RegionId, s.allocatedId, "Available", ALICLOUD_DEFAULT_SHORT_TIMEOUT); err != nil {
		ui.Say(fmt.Sprintf("Timeout while unassociating eip."))
	}

	releaseEipAddressReq := ecs.CreateReleaseEipAddressRequest()

	releaseEipAddressReq.AllocationId = s.allocatedId
	if _, err := client.ReleaseEipAddress(releaseEipAddressReq); err != nil {
		ui.Say(fmt.Sprintf("Failed to release eip."))
	}

}

func WaitForEip(regionId string, allocatedId string, status string, timeout int) error {
	var b Builder
	b.config.AlicloudRegion = regionId
	if err := b.config.Config(); err != nil {
		return err
	}
	client, err := b.config.Client()
	if err != nil {
		return err
	}

	if timeout <= 0 {
		timeout = 60
	}
	for {
		describeEipAddressesReq := ecs.CreateDescribeEipAddressesRequest()

		describeEipAddressesReq.RegionId = regionId
		describeEipAddressesReq.AllocationId = allocatedId
		resp, err := client.DescribeEipAddresses(describeEipAddressesReq)
		if err != nil {
			return err
		}
		eipAddress := resp.EipAddresses.EipAddress
		if len(eipAddress) == 0 {
			return fmt.Errorf("Not found eip")
		}
		if eipAddress[0].Status == status {
			break
		}
		timeout = timeout - 5
		if timeout <= 0 {
			return fmt.Errorf("Timeout")
		}
		time.Sleep(5 * time.Second)
	}
	return nil
}

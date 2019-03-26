package ecs

import (
	"context"
	"fmt"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

type stepConfigAlicloudPublicIP struct {
	publicIPAddress string
	RegionId        string
	SSHPrivateIp    bool
}

func (s *stepConfigAlicloudPublicIP) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)
	instance := state.Get("instance").(ecs.Instance)

	if s.SSHPrivateIp {
		ipaddress := instance.InnerIpAddress.IpAddress
		if len(ipaddress) == 0 {
			ui.Say("Failed to get private ip of instance")
			return multistep.ActionHalt
		}
		state.Put("ipaddress", ipaddress[0])
		return multistep.ActionContinue
	}

	allocatePublicIpAddressReq := ecs.CreateAllocatePublicIpAddressRequest()

	allocatePublicIpAddressReq.InstanceId = instance.InstanceId
	ipaddress, err := client.AllocatePublicIpAddress(allocatePublicIpAddressReq)
	if err != nil {
		state.Put("error", err)
		ui.Say(fmt.Sprintf("Error allocating public ip: %s", err))
		return multistep.ActionHalt
	}
	s.publicIPAddress = ipaddress.IpAddress
	ui.Say(fmt.Sprintf("Allocated public ip address %s.", ipaddress.IpAddress))
	state.Put("ipaddress", ipaddress.IpAddress)
	return multistep.ActionContinue
}

func (s *stepConfigAlicloudPublicIP) Cleanup(state multistep.StateBag) {

}

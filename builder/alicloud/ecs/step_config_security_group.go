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

type stepConfigAlicloudSecurityGroup struct {
	SecurityGroupId   string
	SecurityGroupName string
	Description       string
	VpcId             string
	RegionId          string
	isCreate          bool
}

func (s *stepConfigAlicloudSecurityGroup) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)
	networkType := state.Get("networktype").(InstanceNetWork)

	var securityGroupItems []ecs.SecurityGroup
	var err error
	if len(s.SecurityGroupId) != 0 {
		if networkType == VpcNet {
			vpcId := state.Get("vpcid").(string)
			describeSecurityGroupsReq := ecs.CreateDescribeSecurityGroupsRequest()

			describeSecurityGroupsReq.VpcId = vpcId
			describeSecurityGroupsReq.RegionId = s.RegionId
			securityGroups, _ := client.DescribeSecurityGroups(describeSecurityGroupsReq)
			securityGroupItems = securityGroups.SecurityGroups.SecurityGroup
		} else {
			describeSecurityGroupsReq := ecs.CreateDescribeSecurityGroupsRequest()

			describeSecurityGroupsReq.RegionId = s.RegionId
			securityGroups, _ := client.DescribeSecurityGroups(describeSecurityGroupsReq)
			securityGroupItems = securityGroups.SecurityGroups.SecurityGroup
		}

		if err != nil {
			ui.Say(fmt.Sprintf("Failed querying security group: %s", err))
			state.Put("error", err)
			return multistep.ActionHalt
		}
		for _, securityGroupItem := range securityGroupItems {
			if securityGroupItem.SecurityGroupId == s.SecurityGroupId {
				state.Put("securitygroupid", s.SecurityGroupId)
				s.isCreate = false
				return multistep.ActionContinue
			}
		}
		s.isCreate = false
		message := fmt.Sprintf("The specified security group {%s} doesn't exist.", s.SecurityGroupId)
		state.Put("error", message)
		ui.Say(message)
		return multistep.ActionHalt

	}
	var securityGroupId string
	ui.Say("Creating security groups...")
	if networkType == VpcNet {
		vpcId := state.Get("vpcid").(string)
		createSecurityGroupReq := ecs.CreateCreateSecurityGroupRequest()

		createSecurityGroupReq.RegionId = s.RegionId
		createSecurityGroupReq.SecurityGroupName = s.SecurityGroupName
		createSecurityGroupReq.VpcId = vpcId
		securityGroup, _ := client.CreateSecurityGroup(createSecurityGroupReq)
		securityGroupId = securityGroup.SecurityGroupId
	} else {
		createSecurityGroupReq := ecs.CreateCreateSecurityGroupRequest()

		createSecurityGroupReq.RegionId = s.RegionId
		createSecurityGroupReq.SecurityGroupName = s.SecurityGroupName
		securityGroup, _ := client.CreateSecurityGroup(createSecurityGroupReq)
		securityGroupId = securityGroup.SecurityGroupId
	}
	if err != nil {
		state.Put("error", err)
		ui.Say(fmt.Sprintf("Failed creating security group %s.", err))
		return multistep.ActionHalt
	}
	state.Put("securitygroupid", securityGroupId)
	s.isCreate = true
	s.SecurityGroupId = securityGroupId

	authorizeSecurityGroupEgressReq := ecs.CreateAuthorizeSecurityGroupEgressRequest()

	authorizeSecurityGroupEgressReq.SecurityGroupId = securityGroupId
	authorizeSecurityGroupEgressReq.RegionId = s.RegionId
	authorizeSecurityGroupEgressReq.IpProtocol = IpProtocol
	authorizeSecurityGroupEgressReq.PortRange = PortRange
	authorizeSecurityGroupEgressReq.NicType = NicType
	authorizeSecurityGroupEgressReq.DestCidrIp = CidrIp
	if _, err := client.AuthorizeSecurityGroupEgress(authorizeSecurityGroupEgressReq); err != nil {
		state.Put("error", err)
		ui.Say(fmt.Sprintf("Failed authorizing security group: %s", err))
		return multistep.ActionHalt
	}

	authorizeSecurityGroupReq := ecs.CreateAuthorizeSecurityGroupRequest()

	authorizeSecurityGroupReq.SecurityGroupId = securityGroupId
	authorizeSecurityGroupReq.RegionId = s.RegionId
	authorizeSecurityGroupReq.IpProtocol = IpProtocol
	authorizeSecurityGroupReq.PortRange = PortRange
	authorizeSecurityGroupReq.NicType = NicType
	authorizeSecurityGroupReq.SourceCidrIp = CidrIp
	if _, err := client.AuthorizeSecurityGroup(authorizeSecurityGroupReq); err != nil {
		state.Put("error", err)
		ui.Say(fmt.Sprintf("Failed authorizing security group: %s", err))
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *stepConfigAlicloudSecurityGroup) Cleanup(state multistep.StateBag) {
	if !s.isCreate {
		return
	}

	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)

	message(state, "security group")
	timeoutPoint := time.Now().Add(120 * time.Second)
	for {
		deleteSecurityGroupReq := ecs.CreateDeleteSecurityGroupRequest()

		deleteSecurityGroupReq.RegionId = s.RegionId
		deleteSecurityGroupReq.SecurityGroupId = s.SecurityGroupId
		if _, err := client.DeleteSecurityGroup(deleteSecurityGroupReq); err != nil {
			e := err.(errors.Error)
			if e.ErrorCode() == "DependencyViolation" && time.Now().Before(timeoutPoint) {
				time.Sleep(5 * time.Second)
				continue
			}
			ui.Error(fmt.Sprintf("Failed to delete security group, it may still be around: %s", err))
			return
		}
		break
	}
}

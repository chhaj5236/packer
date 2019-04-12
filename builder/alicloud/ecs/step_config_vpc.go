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

type stepConfigAlicloudVPC struct {
	VpcId     string
	CidrBlock string //192.168.0.0/16 or 172.16.0.0/16 (default)
	VpcName   string
	isCreate  bool
}

func (s *stepConfigAlicloudVPC) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)

	if len(s.VpcId) != 0 {
		describeVpcsReq := ecs.CreateDescribeVpcsRequest()

		describeVpcsReq.VpcId = s.VpcId
		describeVpcsReq.RegionId = config.AlicloudRegion
		vpcs, err := client.DescribeVpcs(describeVpcsReq)
		if err != nil {
			return halt(state, err, "Failed querying vpcs")
		}
		vpc := vpcs.Vpcs.Vpc
		if len(vpc) > 0 {
			state.Put("vpcid", vpc[0].VpcId)
			s.isCreate = false
			return multistep.ActionContinue
		}

		message := fmt.Sprintf("The specified vpc {%s} doesn't exist.", s.VpcId)
		return halt(state, errorsNew.New(message), "")
	}
	ui.Say("Creating vpc...")
	createVpcReq := ecs.CreateCreateVpcRequest()

	createVpcReq.RegionId = config.AlicloudRegion
	createVpcReq.CidrBlock = s.CidrBlock
	createVpcReq.VpcName = s.VpcName
	vpc, err := client.CreateVpc(createVpcReq)
	if err != nil {
		state.Put("error", err)
		ui.Say(fmt.Sprintf("Failed creating vpc: %s", err))
		return multistep.ActionHalt
	}

	waitForParam := AlicloudAccessConfig{AlicloudRegion: config.AlicloudRegion, WaitForVpcId: vpc.VpcId, WaitForStatus: "Available"}
	if err := WaitForExpected(waitForParam.DescribeVpcs, waitForParam.EvaluatorVpcs, ALICLOUD_DEFAULT_SHORT_TIMEOUT); err != nil {
		state.Put("error", err)
		ui.Say(fmt.Sprintf("Failed waiting for vpc to become available: %s", err))
		return multistep.ActionHalt
	}

	state.Put("vpcid", vpc.VpcId)
	s.isCreate = true
	s.VpcId = vpc.VpcId
	return multistep.ActionContinue
}

func (s *stepConfigAlicloudVPC) Cleanup(state multistep.StateBag) {
	if !s.isCreate {
		return
	}

	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)

	cleanUpMessage(state, "VPC")
	timeoutPoint := time.Now().Add(60 * time.Second)
	for {
		deleteVpcReq := ecs.CreateDeleteVpcRequest()

		deleteVpcReq.VpcId = s.VpcId
		if _, err := client.DeleteVpc(deleteVpcReq); err != nil {
			e := err.(errors.Error)
			if (e.ErrorCode() == "DependencyViolation.Instance" ||
				e.ErrorCode() == "DependencyViolation.RouteEntry" ||
				e.ErrorCode() == "DependencyViolation.VSwitch" ||
				e.ErrorCode() == "DependencyViolation.SecurityGroup" ||
				e.ErrorCode() == "Forbbiden") && time.Now().Before(timeoutPoint) {
				time.Sleep(1 * time.Second)
				continue
			}
			ui.Error(fmt.Sprintf("Error deleting vpc, it may still be around: %s", err))
			return
		}
		break
	}
}

package ecs

import (
	"context"
	"fmt"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

type stepPreValidate struct {
	AlicloudDestImageName string
	ForceDelete           bool
}

func (s *stepPreValidate) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packer.Ui)
	if s.ForceDelete {
		ui.Say("Force delete flag found, skipping prevalidating image name.")
		return multistep.ActionContinue
	}

	client := state.Get("client").(*ecs.Client)
	config := state.Get("config").(*Config)
	ui.Say("Prevalidating image name...")
	describeImagesReq := ecs.CreateDescribeImagesRequest()

	describeImagesReq.RegionId = config.AlicloudRegion
	describeImagesReq.ImageName = s.AlicloudDestImageName
	imagesResponse, err := client.DescribeImages(describeImagesReq)

	if err != nil {
		err := fmt.Errorf("Error querying alicloud image: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	images := imagesResponse.Images.Image
	if len(images) > 0 {
		err := fmt.Errorf("Error: Image Name: '%s' is used by an existing alicloud image: %s", images[0].ImageName, images[0].ImageId)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *stepPreValidate) Cleanup(multistep.StateBag) {}

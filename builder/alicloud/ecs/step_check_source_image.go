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

type stepCheckAlicloudSourceImage struct {
	SourceECSImageId string
}

func (s *stepCheckAlicloudSourceImage) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	client := state.Get("client").(*ecs.Client)
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)
	pageSize := 50
	describeImagesReq := ecs.CreateDescribeImagesRequest()

	describeImagesReq.RegionId = config.AlicloudRegion
	describeImagesReq.ImageId = config.AlicloudSourceImage
	describeImagesReq.PageSize = requests.Integer(strconv.Itoa(pageSize))
	imageRes, err := client.DescribeImages(describeImagesReq)
	if err != nil {
		err := fmt.Errorf("Error querying alicloud image: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	images := imageRes.Images.Image

	// Describe markerplace image
	describeImagesReq.ImageOwnerAlias = "marketplace"
	imageMarkets, err := client.DescribeImages(describeImagesReq)
	if err != nil {
		err := fmt.Errorf("Error querying alicloud marketplace image: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	marketImages := imageMarkets.Images.Image
	if len(marketImages) > 0 {
		images = append(images, marketImages...)
	}

	if len(images) == 0 {
		err := fmt.Errorf("No alicloud image was found matching filters: %v", config.AlicloudSourceImage)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	ui.Message(fmt.Sprintf("Found image ID: %s", images[0].ImageId))

	state.Put("source_image", &images[0])
	return multistep.ActionContinue
}

func (s *stepCheckAlicloudSourceImage) Cleanup(multistep.StateBag) {}

package ecs

import (
	"context"
	"fmt"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

type stepRegionCopyAlicloudImage struct {
	AlicloudImageDestinationRegions []string
	AlicloudImageDestinationNames   []string
	RegionId                        string
}

func (s *stepRegionCopyAlicloudImage) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	if len(s.AlicloudImageDestinationRegions) == 0 {
		return multistep.ActionContinue
	}
	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)
	imageId := state.Get("alicloudimage").(string)
	alicloudImages := state.Get("alicloudimages").(map[string]string)
	region := s.RegionId

	numberOfName := len(s.AlicloudImageDestinationNames)
	for index, destinationRegion := range s.AlicloudImageDestinationRegions {
		if destinationRegion == s.RegionId {
			continue
		}
		ecsImageName := ""
		if numberOfName > 0 && index < numberOfName {
			ecsImageName = s.AlicloudImageDestinationNames[index]
		}
		copyImageReq := ecs.CreateCopyImageRequest()

		copyImageReq.RegionId = region
		copyImageReq.ImageId = imageId
		copyImageReq.DestinationRegionId = destinationRegion
		copyImageReq.DestinationImageName = ecsImageName
		image, err := client.CopyImage(copyImageReq)
		if err != nil {
			state.Put("error", err)
			ui.Say(fmt.Sprintf("Error copying images: %s", err))
			return multistep.ActionHalt
		}
		alicloudImages[destinationRegion] = image.ImageId
	}
	return multistep.ActionContinue
}

func (s *stepRegionCopyAlicloudImage) Cleanup(state multistep.StateBag) {
	_, cancelled := state.GetOk(multistep.StateCancelled)
	_, halted := state.GetOk(multistep.StateHalted)
	if cancelled || halted {
		ui := state.Get("ui").(packer.Ui)
		client := state.Get("client").(*ecs.Client)
		alicloudImages := state.Get("alicloudimages").(map[string]string)
		ui.Say(fmt.Sprintf("Stopping copy image because cancellation or error..."))
		for copiedRegionId, copiedImageId := range alicloudImages {
			if copiedRegionId == s.RegionId {
				continue
			}
			cancelCopyImageReq := ecs.CreateCancelCopyImageRequest()

			cancelCopyImageReq.RegionId = copiedRegionId
			cancelCopyImageReq.ImageId = copiedImageId
			if _, err := client.CancelCopyImage(cancelCopyImageReq); err != nil {
				ui.Say(fmt.Sprintf("Error cancelling copy image: %v", err))
			}
		}
	}
}

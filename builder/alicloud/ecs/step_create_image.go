package ecs

import (
	"context"
	"fmt"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

type stepCreateAlicloudImage struct {
	AlicloudImageIgnoreDataDisks bool
	WaitSnapshotReadyTimeout     int
	image                        *ecs.Image
}

func (s *stepCreateAlicloudImage) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)

	// Create the alicloud image
	ui.Say(fmt.Sprintf("Creating image: %s", config.AlicloudImageName))
	var imageId string
	var err error

	if s.AlicloudImageIgnoreDataDisks {
		snapshotId := state.Get("alicloudsnapshot").(string)
		createImageReq := ecs.CreateCreateImageRequest()

		createImageReq.RegionId = config.AlicloudRegion
		createImageReq.SnapshotId = snapshotId
		createImageReq.ImageName = config.AlicloudImageName
		createImageReq.ImageVersion = config.AlicloudImageVersion
		createImageReq.Description = config.AlicloudImageDescription
		image, _ := client.CreateImage(createImageReq)
		imageId = image.ImageId
	} else {
		instance := state.Get("instance").(ecs.Instance)
		createImageReq := ecs.CreateCreateImageRequest()

		createImageReq.RegionId = config.AlicloudRegion
		createImageReq.InstanceId = instance.InstanceId
		createImageReq.ImageName = config.AlicloudImageName
		createImageReq.ImageVersion = config.AlicloudImageVersion
		createImageReq.Description = config.AlicloudImageDescription
		image, _ := client.CreateImage(createImageReq)
		imageId = image.ImageId
	}

	if err != nil {
		return halt(state, err, "Error creating image")
	}

	waitForParam := AlicloudAccessConfig{AlicloudRegion: config.AlicloudRegion, WaitForImageId: imageId}
	if err := WaitForExpected(waitForParam.DescribeImages, waitForParam.EvaluatorImages, s.WaitSnapshotReadyTimeout); err != nil {
		return halt(state, err, "Timeout waiting for image to be created")
	}

	describeImagesReq := ecs.CreateDescribeImagesRequest()

	describeImagesReq.ImageId = imageId
	describeImagesReq.RegionId = config.AlicloudRegion
	images, err := client.DescribeImages(describeImagesReq)
	if err != nil {
		return halt(state, err, "Error querying created imaged")
	}
	image := images.Images.Image
	if len(image) == 0 {
		return halt(state, err, "Unable to find created image")
	}

	s.image = &image[0]

	var snapshotIds = []string{}
	for _, device := range image[0].DiskDeviceMappings.DiskDeviceMapping {
		snapshotIds = append(snapshotIds, device.SnapshotId)
	}

	state.Put("alicloudimage", imageId)
	state.Put("alicloudsnapshots", snapshotIds)

	alicloudImages := make(map[string]string)
	alicloudImages[config.AlicloudRegion] = image[0].ImageId
	state.Put("alicloudimages", alicloudImages)

	return multistep.ActionContinue
}

func (s *stepCreateAlicloudImage) Cleanup(state multistep.StateBag) {
	if s.image == nil {
		return
	}
	_, cancelled := state.GetOk(multistep.StateCancelled)
	_, halted := state.GetOk(multistep.StateHalted)
	if !cancelled && !halted {
		return
	}

	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)
	config := state.Get("config").(*Config)

	ui.Say("Deleting the image because of cancellation or error...")
	deleteImageReq := ecs.CreateDeleteImageRequest()

	deleteImageReq.RegionId = config.AlicloudRegion
	deleteImageReq.ImageId = s.image.ImageId
	if _, err := client.DeleteImage(deleteImageReq); err != nil {
		ui.Error(fmt.Sprintf("Error deleting image, it may still be around: %s", err))
		return
	}
}

package ecs

import (
	"context"
	"fmt"
	"time"

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

	createImageRequest := ecs.CreateCreateImageRequest()
	createImageRequest.RegionId = config.AlicloudRegion
	createImageRequest.ImageName = config.AlicloudImageName
	createImageRequest.ImageVersion = config.AlicloudImageVersion
	createImageRequest.Description = config.AlicloudImageDescription

	if s.AlicloudImageIgnoreDataDisks {
		snapshotId := state.Get("alicloudsnapshot").(string)
		createImageRequest.SnapshotId = snapshotId
	} else {
		instance := state.Get("instance").(ecs.Instance)
		createImageRequest.InstanceId = instance.InstanceId
	}

	imageResponse, err := client.CreateImage(createImageRequest)
	if err != nil {
		return halt(state, err, "Error creating image")
	}

	imageId := imageResponse.ImageId
	if err := WaitForImageReady(config.AlicloudRegion, imageId, s.WaitSnapshotReadyTimeout); err != nil {
		return halt(state, err, "Timeout waiting for image to be created")
	}

	describeImagesResponse := ecs.CreateDescribeImagesRequest()
	describeImagesResponse.ImageId = imageId
	describeImagesResponse.RegionId = config.AlicloudRegion
	images, err := client.DescribeImages(describeImagesResponse)
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

func WaitForImageReady(regionId string, imageId string, timeout int) error {
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
		describeImagesReq := ecs.CreateDescribeImagesRequest()

		describeImagesReq.ImageId = imageId
		describeImagesReq.RegionId = regionId
		describeImagesReq.Status = "Creating"
		resp, err := client.DescribeImages(describeImagesReq)
		if err != nil {
			return err
		}
		image := resp.Images.Image
		if image == nil || len(image) == 0 {
			describeImagesReq.Status = "Available"
			images, err := client.DescribeImages(describeImagesReq)
			if err == nil && len(images.Images.Image) == 1 {
				break
			} else {
				return fmt.Errorf("not found images: %s", err)
			}
		}
		if image[0].Progress == "100%" {
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

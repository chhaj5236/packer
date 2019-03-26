package ecs

import (
	"context"
	"fmt"
	"log"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

type stepDeleteAlicloudImageSnapshots struct {
	AlicloudImageForceDelete          bool
	AlicloudImageForceDeleteSnapshots bool
	AlicloudImageName                 string
	AlicloudImageDestinationRegions   []string
	AlicloudImageDestinationNames     []string
}

func (s *stepDeleteAlicloudImageSnapshots) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)

	// Check for force delete
	if s.AlicloudImageForceDelete {
		err := s.deleteImageAndSnapshots(state, s.AlicloudImageName, config.AlicloudRegion)
		if err != nil {
			return halt(state, err, "")
		}

		numberOfName := len(s.AlicloudImageDestinationNames)
		if numberOfName == 0 {
			return multistep.ActionContinue
		}

		for index, destinationRegion := range s.AlicloudImageDestinationRegions {
			if destinationRegion == config.AlicloudRegion {
				continue
			}

			if index < numberOfName {
				err = s.deleteImageAndSnapshots(state, s.AlicloudImageDestinationNames[index], destinationRegion)
				if err != nil {
					return halt(state, err, "")
				}
			} else {
				break
			}
		}
	}

	return multistep.ActionContinue
}

func (s *stepDeleteAlicloudImageSnapshots) deleteImageAndSnapshots(state multistep.StateBag, imageName string, region string) error {
	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)

	describeImagesReq := ecs.CreateDescribeImagesRequest()

	describeImagesReq.RegionId = region
	describeImagesReq.ImageName = imageName
	imageRes, _ := client.DescribeImages(describeImagesReq)
	images := imageRes.Images.Image
	if len(images) < 1 {
		return nil
	}

	ui.Say(fmt.Sprintf("Deleting duplicated image and snapshot in %s: %s", region, imageName))

	for _, image := range images {
		if image.ImageOwnerAlias != string("self") {
			log.Printf("You can not delete non-customized images: %s ", image.ImageId)
			continue
		}

		deleteImageReq := ecs.CreateDeleteImageRequest()

		deleteImageReq.RegionId = region
		deleteImageReq.ImageId = image.ImageId
		if _, err := client.DeleteImage(deleteImageReq); err != nil {
			err := fmt.Errorf("Failed to delete image: %s", err)
			return err
		}

		if s.AlicloudImageForceDeleteSnapshots {
			for _, diskDevice := range image.DiskDeviceMappings.DiskDeviceMapping {
				request := ecs.CreateDeleteSnapshotRequest()

				request.SnapshotId = diskDevice.SnapshotId
				if _, err := client.DeleteSnapshot(request); err != nil {
					err := fmt.Errorf("Deleting ECS snapshot failed: %s", err)
					return err
				}
			}
		}
	}

	return nil
}

func (s *stepDeleteAlicloudImageSnapshots) Cleanup(state multistep.StateBag) {
}

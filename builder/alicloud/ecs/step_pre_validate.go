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

	var errs *packer.MultiError

	if err := s.validateRegions(state); err != nil {
		errs = packer.MultiErrorAppend(errs, err)
	}

	if err := s.validateDestImageName(state); err != nil {
		errs = packer.MultiErrorAppend(errs, err)
	}

	if errs != nil && len(errs.Errors) > 0 {
		state.Put("error", errs)
		ui.Error(errs.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *stepPreValidate) validateRegions(state multistep.StateBag) error{
	ui := state.Get("ui").(packer.Ui)
	config := state.Get("config").(*Config)

	if config.AlicloudSkipValidation {
		ui.Say("Skip region validation flag found, skipping prevalidating source region and copied regions.")
		return nil
	}

	ui.Say("Prevalidating source region and copied regions...")

	var errs *packer.MultiError
	if err := config.ValidateRegion(config.AlicloudRegion); err != nil {
		errs = packer.MultiErrorAppend(errs, err)
	}
	for _, region := range config.AlicloudImageDestinationRegions {
		if err := config.ValidateRegion(region); err != nil {
			errs = packer.MultiErrorAppend(errs, err)
		}
	}

	if errs != nil && len(errs.Errors) > 0 {
		return errs
	}

	return nil
}

func (s *stepPreValidate) validateDestImageName(state multistep.StateBag) error{
	ui := state.Get("ui").(packer.Ui)
	client := state.Get("client").(*ecs.Client)
	config := state.Get("config").(*Config)

	if s.ForceDelete {
		ui.Say("Force delete flag found, skipping prevalidating image name.")
		return nil
	}

	ui.Say("Prevalidating image name...")

	describeImagesReq := ecs.CreateDescribeImagesRequest()
	describeImagesReq.RegionId = config.AlicloudRegion
	describeImagesReq.ImageName = s.AlicloudDestImageName

	imagesResponse, err := client.DescribeImages(describeImagesReq)
	if err != nil {
		return fmt.Errorf("Error querying alicloud image: %s", err)
	}

	images := imagesResponse.Images.Image
	if len(images) > 0 {
		return fmt.Errorf("Error: Image Name: '%s' is used by an existing alicloud image: %s", images[0].ImageName, images[0].ImageId)
	}

	return nil
}

func (s *stepPreValidate) Cleanup(multistep.StateBag) {}

package ecs

import (
	"context"
	"fmt"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

type stepMountAlicloudDisk struct {
}

func (s *stepMountAlicloudDisk) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	client := state.Get("client").(*ecs.Client)
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)
	instance := state.Get("instance").(ecs.Instance)
	alicloudDiskDevices := config.ECSImagesDiskMappings
	if len(config.ECSImagesDiskMappings) == 0 {
		return multistep.ActionContinue
	}
	ui.Say("Mounting disks.")
	describeDisksReq := ecs.CreateDescribeDisksRequest()

	describeDisksReq.RegionId = instance.RegionId
	describeDisksReq.InstanceId = instance.InstanceId
	diskres, err := client.DescribeDisks(describeDisksReq)
	if err != nil {
		err := fmt.Errorf("Error querying disks: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	disks := diskres.Disks.Disk
	for _, disk := range disks {
		if disk.Status == "Available" {
			attachDiskReq := ecs.CreateAttachDiskRequest()

			attachDiskReq.DiskId = disk.DiskId
			attachDiskReq.InstanceId = instance.InstanceId
			attachDiskReq.Device = getDevice(&disk, alicloudDiskDevices)
			if _, err := client.AttachDisk(attachDiskReq); err != nil {
				err := fmt.Errorf("Error mounting disks: %s", err)
				state.Put("error", err)
				ui.Error(err.Error())
				return multistep.ActionHalt
			}
		}
	}
	for _, disk := range disks {
		waitForParam := AlicloudAccessConfig{AlicloudRegion: instance.RegionId, WaitForDiskId: disk.DiskId, WaitForStatus: "In_use"}
		if err := WaitForExpected(waitForParam.DescribeDisks, waitForParam.EvaluatorDisks, ALICLOUD_DEFAULT_SHORT_TIMEOUT); err != nil {
			err := fmt.Errorf("Timeout waiting for mount: %s", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
	}
	ui.Say("Finished mounting disks.")
	return multistep.ActionContinue
}

func (s *stepMountAlicloudDisk) Cleanup(state multistep.StateBag) {

}

func getDevice(disk *ecs.Disk, diskDevices []AlicloudDiskDevice) string {
	if disk.Device != "" {
		return disk.Device
	}
	for _, alicloudDiskDevice := range diskDevices {
		if alicloudDiskDevice.DiskName == disk.DiskName || alicloudDiskDevice.SnapshotId == disk.SourceSnapshotId {
			return alicloudDiskDevice.Device
		}
	}
	return ""
}

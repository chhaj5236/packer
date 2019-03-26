package ecs

import (
	"context"
	"fmt"
	"time"

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
		if err := WaitForDisk(instance.RegionId, disk.DiskId, "In_use", ALICLOUD_DEFAULT_SHORT_TIMEOUT); err != nil {
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

func WaitForDisk(regionId string, diskId string, status string, timeout int) error {
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
		describeDisksReq := ecs.CreateDescribeDisksRequest()

		describeDisksReq.RegionId = regionId
		describeDisksReq.DiskIds = diskId
		resp, err := client.DescribeDisks(describeDisksReq)
		if err != nil {
			return err
		}
		disk := resp.Disks.Disk
		if disk != nil || len(disk) == 0 {
			return fmt.Errorf("Not found disk")
		}
		if disk[0].Status == status {
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

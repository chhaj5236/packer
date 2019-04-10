package ecs

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"

	"github.com/hashicorp/packer/common/uuid"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/errors"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

type stepCreateAlicloudInstance struct {
	IOOptimized             bool
	InstanceType            string
	UserData                string
	UserDataFile            string
	instanceId              string
	RegionId                string
	InternetChargeType      string
	InternetMaxBandwidthOut int
	InstanceName            string
	ZoneId                  string
	instance                *ecs.Instance
}

func (s *stepCreateAlicloudInstance) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	client := state.Get("client").(*ecs.Client)
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)
	source_image := state.Get("source_image").(*ecs.Image)
	network_type := state.Get("networktype").(InstanceNetWork)
	securityGroupId := state.Get("securitygroupid").(string)
	var instanceId string
	var err error

	ioOptimized := "None"
	if s.IOOptimized {
		ioOptimized = "optimized"
	}
	password := config.Comm.SSHPassword
	if password == "" && config.Comm.WinRMPassword != "" {
		password = config.Comm.WinRMPassword
	}
	ui.Say("Creating instance.")
	if network_type == VpcNet {
		userData, err := s.getUserData(state)
		if err != nil {
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		vswitchId := state.Get("vswitchid").(string)
		clientToken := uuid.TimeOrderedUUID()
		systemDisk := config.AlicloudImageConfig.ECSSystemDiskMapping
		imageDisks := config.AlicloudImageConfig.ECSImagesDiskMappings

		createInstanceReq := ecs.CreateCreateInstanceRequest()

		createInstanceReq.RegionId = s.RegionId
		createInstanceReq.ImageId = source_image.ImageId
		createInstanceReq.InstanceType = s.InstanceType
		createInstanceReq.InternetChargeType = s.InternetChargeType //"PayByTraffic"
		createInstanceReq.InternetMaxBandwidthOut = requests.Integer(strconv.Itoa(s.InternetMaxBandwidthOut))
		createInstanceReq.UserData = userData
		createInstanceReq.IoOptimized = ioOptimized
		createInstanceReq.VSwitchId = vswitchId
		createInstanceReq.SecurityGroupId = securityGroupId
		createInstanceReq.InstanceName = s.InstanceName
		createInstanceReq.Password = password
		createInstanceReq.ZoneId = s.ZoneId
		createInstanceReq.SystemDiskDiskName = systemDisk.DiskName
		createInstanceReq.SystemDiskCategory = systemDisk.DiskCategory
		createInstanceReq.SystemDiskSize = requests.Integer(strconv.Itoa(systemDisk.DiskSize))
		createInstanceReq.SystemDiskDescription = systemDisk.Description
		createInstanceReq.ClientToken = clientToken

		var datadisks []ecs.CreateInstanceDataDisk

		for _, imageDisk := range imageDisks {
			var datadisk ecs.CreateInstanceDataDisk
			datadisk.DiskName = imageDisk.DiskName
			datadisk.Category = imageDisk.DiskCategory
			datadisk.Size = strconv.Itoa(imageDisk.DiskSize)
			datadisk.SnapshotId = imageDisk.SnapshotId
			datadisk.Description = imageDisk.Description
			datadisk.DeleteWithInstance = strconv.FormatBool(imageDisk.DeleteWithInstance)
			datadisk.Device = imageDisk.Device

			datadisks = append(datadisks, datadisk)
		}
		createInstanceReq.DataDisk = &datadisks
		instance, enr := client.CreateInstance(createInstanceReq)
		if enr != nil {
			enr := fmt.Errorf("Error creating instance: %s", enr)
			state.Put("error", enr)
			ui.Error(enr.Error())
			return multistep.ActionHalt
		}
		instanceId = instance.InstanceId
	} else {
		if s.InstanceType == "" {
			s.InstanceType = "PayByTraffic"
		}
		if s.InternetMaxBandwidthOut == 0 {
			s.InternetMaxBandwidthOut = 5
		}
		imageDisks := config.AlicloudImageConfig.ECSImagesDiskMappings
		clientToken := uuid.TimeOrderedUUID()

		createInstanceReq := ecs.CreateCreateInstanceRequest()

		createInstanceReq.RegionId = s.RegionId
		createInstanceReq.ImageId = source_image.ImageId
		createInstanceReq.InstanceType = s.InstanceType
		createInstanceReq.InternetChargeType = s.InternetChargeType //"PayByTraffic"
		createInstanceReq.InternetMaxBandwidthOut = requests.Integer(strconv.Itoa(s.InternetMaxBandwidthOut))
		createInstanceReq.IoOptimized = ioOptimized
		createInstanceReq.SecurityGroupId = securityGroupId
		createInstanceReq.InstanceName = s.InstanceName
		createInstanceReq.Password = password
		createInstanceReq.ZoneId = s.ZoneId
		createInstanceReq.ClientToken = clientToken

		var datadisks []ecs.CreateInstanceDataDisk

		for _, imageDisk := range imageDisks {
			var datadisk ecs.CreateInstanceDataDisk
			datadisk.DiskName = imageDisk.DiskName
			datadisk.Category = imageDisk.DiskCategory
			datadisk.Size = strconv.Itoa(imageDisk.DiskSize)
			datadisk.SnapshotId = imageDisk.SnapshotId
			datadisk.Description = imageDisk.Description
			datadisk.DeleteWithInstance = strconv.FormatBool(imageDisk.DeleteWithInstance)
			datadisk.Device = imageDisk.Device

			datadisks = append(datadisks, datadisk)
		}
		createInstanceReq.DataDisk = &datadisks
		instance, err := client.CreateInstance(createInstanceReq)
		if err != nil {
			err := fmt.Errorf("Error creating instance: %s", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		instanceId = instance.InstanceId
	}

	waitForParam := AlicloudAccessConfig{AlicloudRegion: s.RegionId, WaitForInstanceId: instanceId, WaitForStatus: "Stopped"}
	if err := WaitForExpected(waitForParam.DescribeInstances, waitForParam.EvaluatorInstance, ALICLOUD_DEFAULT_TIMEOUT); err != nil {
		err := fmt.Errorf("Error waiting create instance: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	describeInstancesReq := ecs.CreateDescribeInstancesRequest()

	describeInstancesReq.InstanceIds = "[\"" + instanceId + "\"]"
	instances, err := client.DescribeInstances(describeInstancesReq)
	if err != nil {
		ui.Say(err.Error())
		return multistep.ActionHalt
	}
	instance := instances.Instances.Instance[0]
	s.instance = &instance
	state.Put("instance", instance)

	return multistep.ActionContinue
}

func (s *stepCreateAlicloudInstance) Cleanup(state multistep.StateBag) {
	if s.instance == nil {
		return
	}
	message(state, "instance")
	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)

	deleteInstanceReq := ecs.CreateDeleteInstanceRequest()

	deleteInstanceReq.InstanceId = s.instance.InstanceId
	deleteInstanceReq.Force = "true"
	if _, err := client.DeleteInstance(deleteInstanceReq); err != nil {
		e := err.(errors.Error)
		if e.ErrorCode() == "IncorrectInstanceStatus.Initializing" {
			waitForParam := AlicloudAccessConfig{AlicloudRegion: s.RegionId, WaitForInstanceId: s.instance.InstanceId}
			if err := WaitForExpected(waitForParam.DeleteInstance, waitForParam.EvaluatorDeleteInstance, 60); err != nil {
				ui.Say(fmt.Sprintf("Failed to clean up instance %s: %v", s.instance.InstanceId, err.Error()))
			}
		}
	}

}

func (s *stepCreateAlicloudInstance) getUserData(state multistep.StateBag) (string, error) {
	userData := s.UserData
	if s.UserDataFile != "" {
		data, err := ioutil.ReadFile(s.UserDataFile)
		if err != nil {
			return "", err
		}
		userData = string(data)
	}
	log.Printf(userData)
	return userData, nil

}

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
	ui := state.Get("ui").(packer.Ui)

	ui.Say("Creating instance...")
	createInstanceRequest, err := s.buildCreateInstanceRequest(state)
	if err != nil {
		return halt(state, err, "")
	}
  	instance, err := client.CreateInstance(createInstanceRequest)
	if err != nil {
		return halt(state, err,"Error creating instance")
	}

	instanceId := instance.InstanceId

	waitForParam := AlicloudAccessConfig{AlicloudRegion: s.RegionId, WaitForInstanceId: instanceId, WaitForStatus: "Stopped"}
	if err := WaitForExpected(waitForParam.DescribeInstances, waitForParam.EvaluatorInstance, ALICLOUD_DEFAULT_TIMEOUT); err != nil {
		err := fmt.Errorf("Error waiting create instance: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	describeInstancesRequest := ecs.CreateDescribeInstancesRequest()
	describeInstancesRequest.InstanceIds = "[\"" + instanceId + "\"]"

	instances, err := client.DescribeInstances(describeInstancesRequest)
	if err != nil {
		return halt(state, err, "")
	}

	s.instance = &instances.Instances.Instance[0]
	state.Put("instance", *s.instance)

	return multistep.ActionContinue
}

func (s *stepCreateAlicloudInstance) Cleanup(state multistep.StateBag) {
	if s.instance == nil {
		return
	}
	cleanUpMessage(state, "instance")
	client := state.Get("client").(*ecs.Client)
	ui := state.Get("ui").(packer.Ui)

	deleteInstanceRequest := ecs.CreateDeleteInstanceRequest()

	deleteInstanceRequest.InstanceId = s.instance.InstanceId
	deleteInstanceRequest.Force = "true"
	if _, err := client.DeleteInstance(deleteInstanceRequest); err != nil {
		e := err.(errors.Error)
		if e.ErrorCode() == "IncorrectInstanceStatus.Initializing" {
			waitForParam := AlicloudAccessConfig{AlicloudRegion: s.RegionId, WaitForInstanceId: s.instance.InstanceId}
			if err := WaitForExpected(waitForParam.DeleteInstance, waitForParam.EvaluatorDeleteInstance, 60); err != nil {
				ui.Say(fmt.Sprintf("Failed to clean up instance %s: %v", s.instance.InstanceId, err.Error()))
			}
		}
	}

}

func (s *stepCreateAlicloudInstance) buildCreateInstanceRequest(state multistep.StateBag) (*ecs.CreateInstanceRequest, error){
	request := ecs.CreateCreateInstanceRequest()
	request.ClientToken = uuid.TimeOrderedUUID()
	request.RegionId = s.RegionId
	request.InstanceType = s.InstanceType
	request.InstanceName = s.InstanceName
	request.ZoneId = s.ZoneId

	sourceImage := state.Get("source_image").(*ecs.Image)
	request.ImageId = sourceImage.ImageId

	securityGroupId := state.Get("securitygroupid").(string)
	request.SecurityGroupId = securityGroupId

	networkType := state.Get("networktype").(InstanceNetWork)
	if networkType == VpcNet {
		vswitchId := state.Get("vswitchid").(string)
		request.VSwitchId = vswitchId

		userData, err := s.getUserData(state)
		if err != nil {
			return nil, err
		}

		request.UserData = userData
	} else {
		if s.InternetChargeType == "" {
			s.InternetChargeType = "PayByTraffic"
		}

		if s.InternetMaxBandwidthOut == 0 {
			s.InternetMaxBandwidthOut = 5
		}
	}
	request.InternetChargeType = s.InternetChargeType
	request.InternetMaxBandwidthOut = requests.Integer(convertNumber(s.InternetMaxBandwidthOut))

	ioOptimized := IOOptimizedNone
	if s.IOOptimized {
		ioOptimized = IOOptimizedOptimized
	}
	request.IoOptimized = ioOptimized

	config := state.Get("config").(*Config)
	password := config.Comm.SSHPassword
	if password == "" && config.Comm.WinRMPassword != "" {
		password = config.Comm.WinRMPassword
	}
	request.Password = password

	systemDisk := config.AlicloudImageConfig.ECSSystemDiskMapping
	request.SystemDiskDiskName = systemDisk.DiskName
	request.SystemDiskCategory = systemDisk.DiskCategory
	request.SystemDiskSize = requests.Integer(convertNumber(systemDisk.DiskSize))
	request.SystemDiskDescription = systemDisk.Description

	imageDisks := config.AlicloudImageConfig.ECSImagesDiskMappings
	var dataDisks []ecs.CreateInstanceDataDisk
	for _, imageDisk := range imageDisks {
		var dataDisk ecs.CreateInstanceDataDisk
		dataDisk.DiskName = imageDisk.DiskName
		dataDisk.Category = imageDisk.DiskCategory
		dataDisk.Size = string(convertNumber(imageDisk.DiskSize))
		dataDisk.SnapshotId = imageDisk.SnapshotId
		dataDisk.Description = imageDisk.Description
		dataDisk.DeleteWithInstance = strconv.FormatBool(imageDisk.DeleteWithInstance)
		dataDisk.Device = imageDisk.Device

		dataDisks = append(dataDisks, dataDisk)
	}
	request.DataDisk = &dataDisks

	return request, nil
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

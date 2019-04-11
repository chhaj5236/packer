package ecs

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"
	"time"

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

	if err := WaitForInstance(s.RegionId, instanceId, "Stopped", ALICLOUD_DEFAULT_TIMEOUT); err != nil {
		return halt(state, err, "Error waiting create instance")
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
			if err := WaitForDeleteInstance(s.RegionId, s.instance.InstanceId, 60); err != nil {
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

func WaitForInstance(regionId string, instanceId string, status string, timeout int) error {
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
		describeInstancesReq := ecs.CreateDescribeInstancesRequest()

		describeInstancesReq.InstanceIds = "[\"" + instanceId + "\"]"
		resp, err := client.DescribeInstances(describeInstancesReq)
		if err != nil {
			return err
		}
		instance := resp.Instances.Instance[0]
		if instance.Status == status {
			//TODO
			//Sleep one more time for timing issues
			time.Sleep(5 * time.Second)
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

func WaitForDeleteInstance(regionId string, instanceId string, timeout int) error {
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
		deleteInstanceReq := ecs.CreateDeleteInstanceRequest()

		deleteInstanceReq.InstanceId = instanceId
		deleteInstanceReq.Force = "true"
		if _, err := client.DeleteInstance(deleteInstanceReq); err != nil {
			e := err.(errors.Error)
			if e.ErrorCode() == "IncorrectInstanceStatus.Initializing" {
				timeout = timeout - 5
				if timeout <= 0 {
					return fmt.Errorf("Timeout")
				}
				time.Sleep(5 * time.Second)
			}
		} else {
			break
		}
	}
	return nil
}

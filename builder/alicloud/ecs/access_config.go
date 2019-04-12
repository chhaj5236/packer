package ecs

import (
	"fmt"
	"os"
	"time"
	"github.com/hashicorp/packer/version"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/errors"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/hashicorp/packer/template/interpolate"
)

// Config of alicloud
type AlicloudAccessConfig struct {
	AlicloudAccessKey      string `mapstructure:"access_key"`
	AlicloudSecretKey      string `mapstructure:"secret_key"`
	AlicloudRegion         string `mapstructure:"region"`
	AlicloudSkipValidation bool   `mapstructure:"skip_region_validation"`
	SecurityToken          string `mapstructure:"security_token"`

	client *ecs.Client

	// waitFor request
	WaitForInstanceId  string
	WaitForStatus      string
	WaitForAllocatedId string
	WaitForDiskId      string
	WaitForSnapshotId  string
	WaitForImageId     string
	WaitForVpcId       string
	WaitForVSwitchId   string
	WaitForTimeout     int
}

const Packer = "HashiCorp-Packer"
const DefaultRequestReadTimeout = 10 * time.Second

// Client for AlicloudClient
func (c *AlicloudAccessConfig) Client() (*ecs.Client, error) {
	if c.client != nil {
		return c.client, nil
	}
	if c.SecurityToken == "" {
		c.SecurityToken = os.Getenv("SECURITY_TOKEN")
	}

	client, err := ecs.NewClientWithStsToken(c.AlicloudRegion, c.AlicloudAccessKey,
		c.AlicloudSecretKey, c.SecurityToken)
	if err != nil {
		return nil, err
	}

	client.AppendUserAgent(Packer, version.FormattedVersion())
	client.SetReadTimeout(DefaultRequestReadTimeout)
	c.client = client

	return c.client, nil
}

func (c *AlicloudAccessConfig) Prepare(ctx *interpolate.Context) []error {
	var errs []error
	if err := c.Config(); err != nil {
		errs = append(errs, err)
	}

	if c.AlicloudRegion == "" {
		c.AlicloudRegion = os.Getenv("ALICLOUD_REGION")
	}

	if c.AlicloudRegion == "" {
		errs = append(errs, fmt.Errorf("region option or ALICLOUD_REGION must be provided in template file or environment variables."))
	}

	if len(errs) > 0 {
		return errs
	}

	return nil
}

func (c *AlicloudAccessConfig) Config() error {
	if c.AlicloudAccessKey == "" {
		c.AlicloudAccessKey = os.Getenv("ALICLOUD_ACCESS_KEY")
	}
	if c.AlicloudSecretKey == "" {
		c.AlicloudSecretKey = os.Getenv("ALICLOUD_SECRET_KEY")
	}
	if c.AlicloudAccessKey == "" || c.AlicloudSecretKey == "" {
		return fmt.Errorf("ALICLOUD_ACCESS_KEY and ALICLOUD_SECRET_KEY must be set in template file or environment variables.")
	}
	return nil

}

func (c *AlicloudAccessConfig) ValidateRegion(region string) error {

	supportedRegions, err := c.getSupportedRegions()
	if err != nil {
		return err
	}

	for _, supportedRegion := range supportedRegions {
		if region == supportedRegion {
			return nil
		}
	}

	return fmt.Errorf("Not a valid alicloud region: %s", region)
}

func (c *AlicloudAccessConfig) getSupportedRegions() ([]string, error) {
	client, err := c.Client()
	if err != nil {
		return nil, err
	}

	regionsRequest := ecs.CreateDescribeRegionsRequest()

	regionsResponse, err := client.DescribeRegions(regionsRequest)
	if err != nil {
		return nil, err
	}

	validRegions := make([]string, len(regionsResponse.Regions.Region))
	for _, valid := range regionsResponse.Regions.Region {
		validRegions = append(validRegions, valid.RegionId)
	}

	return validRegions, nil
}

func WaitForExpected(response func() interface{}, evaluator func(interface{}) interface{}, timeout int) error {

	if timeout <= 0 {
		timeout = 60
	}
	for {
		if resp := response(); resp != nil {
			evaluate := evaluator(resp)
			eval, ok := evaluate.(bool)
			if !ok {
				return fmt.Errorf("evaluator failed : %s", resp)
			}
			if eval {
				break
			}
		}
		timeout := timeout - 5
		if timeout <= 0 {
			return fmt.Errorf("Timeout ")
		}
		time.Sleep(5 * time.Second)
	}
	return nil
}

func (c *AlicloudAccessConfig) DescribeInstances() interface{} {

	if err := c.Config(); err != nil {
		return err
	}
	client, err := c.Client()
	if err != nil {
		return err
	}

	describeInstancesReq := ecs.CreateDescribeInstancesRequest()

	describeInstancesReq.InstanceIds = "[\"" + c.WaitForInstanceId + "\"]"
	response, err := client.DescribeInstances(describeInstancesReq)
	if err != nil {
		return fmt.Errorf("describe failed: %s", err)
	}
	return response
}

func (c *AlicloudAccessConfig) EvaluatorInstance(response interface{}) interface{} {

	instancesResp, ok := response.(*ecs.DescribeInstancesResponse)
	if !ok {
		return response
	}
	instances := instancesResp.Instances.Instance
	for _, instance := range instances {
		if c.WaitForStatus == instance.Status {
			return true
		}
	}
	return false
}

func (c *AlicloudAccessConfig) DeleteInstance() interface{} {

	if err := c.Config(); err != nil {
		return err
	}
	client, err := c.Client()
	if err != nil {
		return err
	}

	deleteInstanceReq := ecs.CreateDeleteInstanceRequest()

	deleteInstanceReq.InstanceId = c.WaitForInstanceId
	deleteInstanceReq.Force = "true"
	if _, err := client.DeleteInstance(deleteInstanceReq); err != nil {
		return err
	}
	return true
}

func (c *AlicloudAccessConfig) EvaluatorDeleteInstance(response interface{}) interface{} {

	e, ok := response.(errors.Error)
	if !ok {
		if _, ok := response.(bool); ok {
			return true
		}
	}
	if e.ErrorCode() == "IncorrectInstanceStatus.Initializing" {
		return false
	}
	return response
}

func (c *AlicloudAccessConfig) DescribeEipAddresses() interface{} {

	if err := c.Config(); err != nil {
		return err
	}
	client, err := c.Client()
	if err != nil {
		return err
	}

	describeEipAddressesReq := ecs.CreateDescribeEipAddressesRequest()

	describeEipAddressesReq.RegionId = c.AlicloudRegion
	describeEipAddressesReq.AllocationId = c.WaitForAllocatedId
	response, err := client.DescribeEipAddresses(describeEipAddressesReq)
	if err != nil {
		return fmt.Errorf("describe failed: %s", err)
	}
	if len(response.EipAddresses.EipAddress) == 0 {
		return fmt.Errorf("Not found ")
	}
	return response
}

func (c *AlicloudAccessConfig) EvaluatorEipAddress(response interface{}) interface{} {

	eipAddressesResp, ok := response.(*ecs.DescribeEipAddressesResponse)
	if !ok {
		return response
	}
	eipAddresses := eipAddressesResp.EipAddresses.EipAddress
	for _, eipAddress := range eipAddresses {
		if c.WaitForStatus == eipAddress.Status {
			return true
		}
	}
	return false
}

func (c *AlicloudAccessConfig) DescribeVpcs() interface{} {

	if err := c.Config(); err != nil {
		return err
	}
	client, err := c.Client()
	if err != nil {
		return err
	}

	describeVpcsReq := ecs.CreateDescribeVpcsRequest()

	describeVpcsReq.RegionId = c.AlicloudRegion
	describeVpcsReq.VpcId = c.WaitForVpcId
	response, err := client.DescribeVpcs(describeVpcsReq)
	if err != nil {
		return fmt.Errorf("describe failed: %s", err)
	}
	return response
}

func (c *AlicloudAccessConfig) EvaluatorVpcs(response interface{}) interface{} {

	vpcsResp, ok := response.(*ecs.DescribeVpcsResponse)
	if !ok {
		return response
	}
	vpcs := vpcsResp.Vpcs.Vpc
	if len(vpcs) > 0 {
		for _, vpc := range vpcs {
			if c.WaitForStatus == vpc.Status {
				return true
			}
		}
	}
	return false
}

func (c *AlicloudAccessConfig) DescribeVSwitches() interface{} {

	if err := c.Config(); err != nil {
		return err
	}
	client, err := c.Client()
	if err != nil {
		return err
	}

	describeVSwitchesReq := ecs.CreateDescribeVSwitchesRequest()

	describeVSwitchesReq.VpcId = c.WaitForVpcId
	describeVSwitchesReq.VSwitchId = c.WaitForVSwitchId
	response, err := client.DescribeVSwitches(describeVSwitchesReq)
	if err != nil {
		return fmt.Errorf("describe failed: %s", err)
	}
	return response
}

func (c *AlicloudAccessConfig) EvaluatorVSwitches(response interface{}) interface{} {

	vSwitchesResp, ok := response.(*ecs.DescribeVSwitchesResponse)
	if !ok {
		return response
	}
	vSwitches := vSwitchesResp.VSwitches.VSwitch
	if len(vSwitches) > 0 {
		for _, vSwitch := range vSwitches {
			if c.WaitForStatus == vSwitch.Status {
				return true
			}
		}
	}
	return false
}

func (c *AlicloudAccessConfig) DescribeImages() interface{} {

	if err := c.Config(); err != nil {
		return err
	}
	client, err := c.Client()
	if err != nil {
		return err
	}

	describeImagesReq := ecs.CreateDescribeImagesRequest()

	describeImagesReq.ImageId = c.WaitForImageId
	describeImagesReq.RegionId = c.AlicloudRegion
	describeImagesReq.Status = "Creating"
	response, err := client.DescribeImages(describeImagesReq)
	if err != nil {
		return fmt.Errorf("describe failed: %s", err)
	}
	images := response.Images.Image
	if images == nil || len(images) == 0 {
		describeImagesReq.Status = "Available"
		resp, err := client.DescribeImages(describeImagesReq)
		if err == nil && len(resp.Images.Image) == 1 {
			return true
		}
		return fmt.Errorf("describe failed: %s", err)
	}
	return response
}

func (c *AlicloudAccessConfig) EvaluatorImages(response interface{}) interface{} {

	imagesResp, ok := response.(*ecs.DescribeImagesResponse)
	if !ok {
		if _, ok := response.(bool); ok {
			return true
		}
		return response
	}
	images := imagesResp.Images.Image
	for _, image := range images {
		if image.Progress == "100%" {
			return true
		}
	}
	return false
}

func (c *AlicloudAccessConfig) DescribeSnapshots() interface{} {

	if err := c.Config(); err != nil {
		return err
	}
	client, err := c.Client()
	if err != nil {
		return err
	}

	describeSnapshotsReq := ecs.CreateDescribeSnapshotsRequest()

	describeSnapshotsReq.RegionId = c.AlicloudRegion
	describeSnapshotsReq.SnapshotIds = c.WaitForSnapshotId
	response, err := client.DescribeSnapshots(describeSnapshotsReq)
	if err != nil {
		return fmt.Errorf("describe failed: %s", err)
	}
	snapshots := response.Snapshots.Snapshot
	if snapshots == nil || len(snapshots) == 0 {
		return fmt.Errorf("Not found snapshot ")
	}
	return response
}

func (c *AlicloudAccessConfig) EvaluatorSnapshots(response interface{}) interface{} {

	snapshotsResp, ok := response.(*ecs.DescribeSnapshotsResponse)
	if !ok {
		return response
	}
	snapshots := snapshotsResp.Snapshots.Snapshot
	for _, snapshot := range snapshots {
		if snapshot.Progress == "100%" {
			return true
		}
	}
	return false
}

func (c *AlicloudAccessConfig) DescribeDisks() interface{} {

	if err := c.Config(); err != nil {
		return err
	}
	client, err := c.Client()
	if err != nil {
		return err
	}

	describeDisksReq := ecs.CreateDescribeDisksRequest()

	describeDisksReq.RegionId = c.AlicloudRegion
	describeDisksReq.DiskIds = c.WaitForDiskId
	response, err := client.DescribeDisks(describeDisksReq)
	if err != nil {
		return fmt.Errorf("describe failed: %s", err)
	}
	disks := response.Disks.Disk
	if disks == nil || len(disks) == 0 {
		return fmt.Errorf("Not found disk ")
	}
	return response
}

func (c *AlicloudAccessConfig) EvaluatorDisks(response interface{}) interface{} {

	disksResp, ok := response.(*ecs.DescribeDisksResponse)
	if !ok {
		return response
	}
	disks := disksResp.Disks.Disk
	for _, disk := range disks {
		if c.WaitForStatus == disk.Status {
			return true
		}
	}
	return false
}

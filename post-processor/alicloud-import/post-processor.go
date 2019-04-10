package alicloudimport

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/packer/version"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/errors"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ram"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	packerecs "github.com/hashicorp/packer/builder/alicloud/ecs"
	"github.com/hashicorp/packer/common"
	"github.com/hashicorp/packer/helper/config"
	"github.com/hashicorp/packer/packer"
	"github.com/hashicorp/packer/template/interpolate"
)

const (
	BuilderId                             = "packer.post-processor.alicloud-import"
	OSSSuffix                             = "oss-"
	RAWFileFormat                         = "raw"
	VHDFileFormat                         = "vhd"
	PolicyType                            = "System"
	DefaultRoleName                       = "AliyunECSImageImportDefaultRole"
	NoSetRole                             = "NoSetRoletoECSServiceAccount"
	PolicyName                            = "AliyunECSImageImportRolePolicy"
	Packer                                = "HashiCorp-Packer"
	AliyunECSImageImportDefaultRolePolicy = `{
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Effect": "Allow",
      "Principal": {
        "Service": [
          "ecs.aliyuncs.com"
        ]
      }
    }
  ],
  "Version": "1"
}`
)

// Configuration of this post processor
type Config struct {
	common.PackerConfig `mapstructure:",squash"`
	packerecs.Config    `mapstructure:",squash"`

	// Variables specific to this post processor
	OSSBucket                       string            `mapstructure:"oss_bucket_name"`
	OSSKey                          string            `mapstructure:"oss_key_name"`
	SkipClean                       bool              `mapstructure:"skip_clean"`
	Tags                            map[string]string `mapstructure:"tags"`
	AlicloudImageName               string            `mapstructure:"image_name"`
	AlicloudImageVersion            string            `mapstructure:"image_version"`
	AlicloudImageDescription        string            `mapstructure:"image_description"`
	AlicloudImageShareAccounts      []string          `mapstructure:"image_share_account"`
	AlicloudImageDestinationRegions []string          `mapstructure:"image_copy_regions"`
	OSType                          string            `mapstructure:"image_os_type"`
	Platform                        string            `mapstructure:"image_platform"`
	Architecture                    string            `mapstructure:"image_architecture"`
	Size                            string            `mapstructure:"image_system_size"`
	Format                          string            `mapstructure:"format"`
	AlicloudImageForceDelete        bool              `mapstructure:"image_force_delete"`

	ctx interpolate.Context
}

type PostProcessor struct {
	config            Config
	DiskDeviceMapping []ecs.DiskDeviceMapping
}

// Entry point for configuration parsing when we've defined
func (p *PostProcessor) Configure(raws ...interface{}) error {
	err := config.Decode(&p.config, &config.DecodeOpts{
		Interpolate:        true,
		InterpolateContext: &p.config.ctx,
		InterpolateFilter: &interpolate.RenderFilter{
			Exclude: []string{
				"oss_key_name",
			},
		},
	}, raws...)
	if err != nil {
		return err
	}

	errs := new(packer.MultiError)

	// Check and render oss_key_name
	if err = interpolate.Validate(p.config.OSSKey, &p.config.ctx); err != nil {
		errs = packer.MultiErrorAppend(
			errs, fmt.Errorf("Error parsing oss_key_name template: %s", err))
	}

	// Check we have alicloud access variables defined somewhere
	errs = packer.MultiErrorAppend(errs, p.config.AlicloudAccessConfig.Prepare(&p.config.ctx)...)

	// define all our required parameters
	templates := map[string]*string{
		"oss_bucket_name": &p.config.OSSBucket,
	}
	// Check out required params are defined
	for key, ptr := range templates {
		if *ptr == "" {
			errs = packer.MultiErrorAppend(
				errs, fmt.Errorf("%s must be set", key))
		}
	}

	// Anything which flagged return back up the stack
	if len(errs.Errors) > 0 {
		return errs
	}

	packer.LogSecretFilter.Set(p.config.AlicloudAccessKey, p.config.AlicloudSecretKey)
	log.Println(p.config)
	return nil
}

func (p *PostProcessor) PostProcess(ui packer.Ui, artifact packer.Artifact) (packer.Artifact, bool, error) {
	var err error

	// Render this key since we didn't in the configure phase
	p.config.OSSKey, err = interpolate.Render(p.config.OSSKey, &p.config.ctx)
	if err != nil {
		return nil, false, fmt.Errorf("Error rendering oss_key_name template: %s", err)
	}
	if p.config.OSSKey == "" {
		p.config.OSSKey = "Packer_" + strconv.Itoa(time.Now().Nanosecond())
	}
	log.Printf("Rendered oss_key_name as %s", p.config.OSSKey)

	log.Println("Looking for RAW or VHD in artifact")
	// Locate the files output from the builder
	source := ""
	for _, path := range artifact.Files() {
		if strings.HasSuffix(path, VHDFileFormat) || strings.HasSuffix(path, RAWFileFormat) {
			source = path
			break
		}
	}

	// Hope we found something useful
	if source == "" {
		return nil, false, fmt.Errorf("No vhd or raw file found in artifact from builder")
	}

	ecsClient, err := p.config.AlicloudAccessConfig.Client()
	if err != nil {
		return nil, false, fmt.Errorf("Failed to connect alicloud ecs  %s", err)
	}
	ecsClient.AppendUserAgent(Packer, version.FormattedVersion())

	alicloudRegion := p.config.AlicloudRegion
	describeImagesReq := ecs.CreateDescribeImagesRequest()

	describeImagesReq.RegionId = alicloudRegion
	describeImagesReq.ImageName = p.config.AlicloudImageName
	images, err := ecsClient.DescribeImages(describeImagesReq)
	if err != nil {
		return nil, false, fmt.Errorf("Failed to start import from %s/%s: %s",
			getEndPonit(p.config.OSSBucket), p.config.OSSKey, err)
	}

	image := images.Images.Image
	if len(image) > 0 && !p.config.AlicloudImageForceDelete {
		return nil, false, fmt.Errorf("Duplicated image exists, please delete the existing images " +
			"or set the 'image_force_delete' value as true")
	}

	// Set up the OSS client
	log.Println("Creating OSS Client")
	client, err := oss.New(getEndPonit(p.config.AlicloudRegion), p.config.AlicloudAccessKey,
		p.config.AlicloudSecretKey)
	if err != nil {
		return nil, false, fmt.Errorf("Creating oss connection failed: %s", err)
	}
	bucket, err := queryOrCreateBucket(p.config.OSSBucket, client)
	if err != nil {
		return nil, false, fmt.Errorf("Failed to query or create bucket %s: %s", p.config.OSSBucket, err)
	}

	if err != nil {
		return nil, false, fmt.Errorf("Failed to open %s: %s", source, err)
	}

	err = bucket.PutObjectFromFile(p.config.OSSKey, source)
	if err != nil {
		return nil, false, fmt.Errorf("Failed to upload image %s: %s", source, err)
	}
	if len(image) > 0 && p.config.AlicloudImageForceDelete {
		deleteImageReq := ecs.CreateDeleteImageRequest()

		deleteImageReq.RegionId = alicloudRegion
		deleteImageReq.ImageId = image[0].ImageId
		_, err := ecsClient.DeleteImage(deleteImageReq)
		if err != nil {
			return nil, false, fmt.Errorf("Delete duplicated image %s failed", image[0].ImageName)
		}
	}

	diskDeviceMapping := ecs.DiskDeviceMapping{
		Size:            p.config.Size,
		Format:          p.config.Format,
		ImportOSSBucket: p.config.OSSBucket,
		ImportOSSObject: p.config.OSSKey,
	}

	importImageReq := ecs.CreateImportImageRequest()

	importImageReq.RegionId = alicloudRegion
	importImageReq.ImageName = p.config.AlicloudImageName
	importImageReq.Description = p.config.AlicloudImageDescription
	importImageReq.Architecture = p.config.Architecture
	importImageReq.OSType = p.config.OSType
	importImageReq.Platform = p.config.Platform

	var importImageDiskDeviceMappings []ecs.ImportImageDiskDeviceMapping
	var importImageDiskDeviceMapping ecs.ImportImageDiskDeviceMapping

	importImageDiskDeviceMapping.DiskImSize = diskDeviceMapping.Size
	importImageDiskDeviceMapping.Format = diskDeviceMapping.Format
	importImageDiskDeviceMapping.OSSBucket = diskDeviceMapping.ImportOSSBucket
	importImageDiskDeviceMapping.OSSObject = diskDeviceMapping.ImportOSSObject

	importImageReq.DiskDeviceMapping = &importImageDiskDeviceMappings
	importimage, err := ecsClient.ImportImage(importImageReq)

	if err != nil {
		e := err.(errors.Error)
		if e.ErrorCode() == NoSetRole {
			ramClient, _ := ram.NewClient()
			getRoleReq := ram.CreateGetRoleRequest()

			getRoleReq.RoleName = DefaultRoleName
			roleResponse, err := ramClient.GetRole(getRoleReq)
			if err != nil {
				return nil, false, fmt.Errorf("Failed to start import from %s/%s: %s",
					getEndPonit(p.config.OSSBucket), p.config.OSSKey, err)
			}
			if roleResponse.Role.RoleId == "" {

				if err := p.createAttachRole(); err != nil {
					return nil, false, fmt.Errorf("Failed to create_attach : %s", err)
				}
			} else {

				if err := p.attachUpdateRole(); err != nil {
					return nil, false, fmt.Errorf("Failed to attach_update : %s", err)
				}
			}
			for i := 10; i > 0; i = i - 1 {
				_, err := ecsClient.ImportImage(importImageReq)
				if err != nil {
					e = err.(errors.Error)
					if e.ErrorCode() == NoSetRole {
						time.Sleep(5 * time.Second)
						continue
					} else if e.ErrorCode() == "ImageIsImporting" ||
						e.ErrorCode() == "InvalidImageName.Duplicated" {
						break
					}
					return nil, false, fmt.Errorf("Failed to start import from %s/%s: %s",
						getEndPonit(p.config.OSSBucket), p.config.OSSKey, err)
				}
				break
			}

		} else {

			return nil, false, fmt.Errorf("Failed to start import from %s/%s: %s",
				getEndPonit(p.config.OSSBucket), p.config.OSSKey, err)
		}
	}

	waitForParam := packerecs.AlicloudAccessConfig{AlicloudRegion: alicloudRegion, WaitForImageId: importimage.ImageId}
	err = packerecs.WaitForExpected(waitForParam.DescribeImages, waitForParam.EvaluatorImages, packerecs.ALICLOUD_DEFAULT_LONG_TIMEOUT)
	// Add the reported Alicloud image ID to the artifact list
	log.Printf("Importing created alicloud image ID %s in region %s Finished.", importimage.ImageId, p.config.AlicloudRegion)
	artifact = &packerecs.Artifact{
		AlicloudImages: map[string]string{
			p.config.AlicloudRegion: importimage.ImageId,
		},
		BuilderIdValue: BuilderId,
		Client:         ecsClient,
	}

	if !p.config.SkipClean {
		ui.Message(fmt.Sprintf("Deleting import source %s/%s/%s",
			getEndPonit(p.config.AlicloudRegion), p.config.OSSBucket, p.config.OSSKey))
		if err = bucket.DeleteObject(p.config.OSSKey); err != nil {
			return nil, false, fmt.Errorf("Failed to delete %s/%s/%s: %s",
				getEndPonit(p.config.AlicloudRegion), p.config.OSSBucket, p.config.OSSKey, err)
		}
	}

	return artifact, false, nil
}

func queryOrCreateBucket(bucketName string, client *oss.Client) (*oss.Bucket, error) {
	isExist, err := client.IsBucketExist(bucketName)
	if err != nil {
		return nil, err
	}
	if !isExist {
		err = client.CreateBucket(bucketName)
		if err != nil {
			return nil, err
		}
	}
	bucket, err := client.Bucket(bucketName)
	if err != nil {
		return nil, err
	}
	return bucket, nil

}

func getEndPonit(region string) string {
	return "https://" + GetOSSRegion(region) + ".aliyuncs.com"
}

func GetOSSRegion(region string) string {
	if strings.HasPrefix(region, OSSSuffix) {
		return region
	}
	return OSSSuffix + region
}

func GetECSRegion(region string) string {
	if strings.HasPrefix(region, OSSSuffix) {
		return strings.TrimSuffix(region, OSSSuffix)
	}
	return region

}

func (p *PostProcessor) createAttachRole() error {
	ramClient, _ := ram.NewClient()
	createRoleReq := ram.CreateCreateRoleRequest()

	createRoleReq.RoleName = DefaultRoleName
	createRoleReq.AssumeRolePolicyDocument = AliyunECSImageImportDefaultRolePolicy
	if _, err := ramClient.CreateRole(createRoleReq); err != nil {
		return fmt.Errorf("Failed to start import from %s/%s: %s",
			getEndPonit(p.config.OSSBucket), p.config.OSSKey, err)
	}
	attachPolicyToRoleReq := ram.CreateAttachPolicyToRoleRequest()

	attachPolicyToRoleReq.PolicyName = PolicyName
	attachPolicyToRoleReq.PolicyType = PolicyType
	attachPolicyToRoleReq.RoleName = DefaultRoleName
	if _, err := ramClient.AttachPolicyToRole(attachPolicyToRoleReq); err != nil {
		return fmt.Errorf("Failed to start import from %s/%s: %s",
			getEndPonit(p.config.OSSBucket), p.config.OSSKey, err)
	}
	return nil
}

func (p *PostProcessor) attachUpdateRole() error {
	ramClient, _ := ram.NewClient()
	listPoliciesForRoleReq := ram.CreateListPoliciesForRoleRequest()

	listPoliciesForRoleReq.RoleName = DefaultRoleName
	policyListResponse, err := ramClient.ListPoliciesForRole(listPoliciesForRoleReq)
	if err != nil {
		return fmt.Errorf("Failed to start import from %s/%s: %s",
			getEndPonit(p.config.OSSBucket), p.config.OSSKey, err)
	}
	isAliyunECSImageImportRolePolicyNotExit := true
	for _, policy := range policyListResponse.Policies.Policy {
		if policy.PolicyName == PolicyName &&
			policy.PolicyType == PolicyType {
			isAliyunECSImageImportRolePolicyNotExit = false
			break
		}
	}
	if isAliyunECSImageImportRolePolicyNotExit {
		attachPolicyToRoleReq := ram.CreateAttachPolicyToRoleRequest()

		attachPolicyToRoleReq.PolicyName = PolicyName
		attachPolicyToRoleReq.PolicyType = PolicyType
		attachPolicyToRoleReq.RoleName = DefaultRoleName
		if _, err := ramClient.AttachPolicyToRole(attachPolicyToRoleReq); err != nil {
			return fmt.Errorf("Failed to start import from %s/%s: %s",
				getEndPonit(p.config.OSSBucket), p.config.OSSKey, err)
		}
	}
	updateRoleReq := ram.CreateUpdateRoleRequest()

	updateRoleReq.RoleName = DefaultRoleName
	updateRoleReq.NewAssumeRolePolicyDocument = AliyunECSImageImportDefaultRolePolicy
	if _, err := ramClient.UpdateRole(updateRoleReq); err != nil {
		return fmt.Errorf("Failed to start import from %s/%s: %s",
			getEndPonit(p.config.OSSBucket), p.config.OSSKey, err)
	}
	return nil
}

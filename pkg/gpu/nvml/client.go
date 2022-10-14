//go:build nvml

package nvml

import (
	"fmt"
	nvlib "gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvlib/device"
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvml"
	"k8s.io/klog/v2"
)

type clientImpl struct {
	nvmlClient  nvml.Interface
	nvlibClient nvlib.Interface
}

func NewClient() Client {
	nvmlClient := nvml.New()
	return &clientImpl{
		nvmlClient:  nvmlClient,
		nvlibClient: nvlib.New(nvlib.WithNvml(nvmlClient)),
	}
}

func (c *clientImpl) init() error {
	if ret := c.nvmlClient.Init(); ret != nvml.SUCCESS {
		return fmt.Errorf("unable to initialize NVML: %s", ret.Error())
	}
	return nil
}

func (c *clientImpl) shutdown() {
	if ret := c.nvmlClient.Shutdown(); ret != nvml.SUCCESS {
		klog.Errorf("unable to shut down NVML: %s", ret.Error())
	}
}

// GetGpuIndex returns the index of the GPU associated to the
// MIG device provided as arg. Returns err if the device
// is not found or any error occurs while retrieving it.
func (c *clientImpl) GetGpuIndex(migDeviceId string) (int, error) {
	if err := c.init(); err != nil {
		return 0, err
	}
	defer c.shutdown()

	klog.V(1).InfoS("retrieving GPU index of MIG device", "MIGDeviceUUID", migDeviceId)
	var result int
	var err error
	var found bool
	err = c.nvlibClient.VisitMigDevices(func(gpuIndex int, _ nvlib.Device, migIndex int, m nvlib.MigDevice) error {
		if found {
			return nil
		}
		uuid, ret := m.GetUUID()
		if ret != nvml.SUCCESS {
			return fmt.Errorf(
				"error getting UUID of MIG device with index %d on GPU %v: %s",
				migIndex,
				gpuIndex,
				ret.Error(),
			)
		}
		klog.V(3).InfoS(
			"visiting MIG device",
			"GPUIndex",
			gpuIndex,
			"MIGDeviceIndex",
			migIndex,
			"MIGDeviceUUID",
			uuid,
		)
		if uuid == migDeviceId {
			result = gpuIndex
			found = true
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("error getting GPU index of MIG device %s: not found", migDeviceId)
	}
	return result, err
}

func (c *clientImpl) DeleteMigDevice(id string) error {
	if err := c.init(); err != nil {
		return err
	}
	defer c.shutdown()

	// Fetch MIG device handle
	d, ret := c.nvmlClient.DeviceGetHandleByUUID(id)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting MIG device with UUID %s: %s", id, ret.Error())
	}
	isMig, ret := d.IsMigDeviceHandle()
	if ret != nvml.SUCCESS {
		return fmt.Errorf(
			"error determining whether the device with UUID %s is a MIG device: %s",
			id,
			ret.Error(),
		)
	}
	if !isMig {
		return fmt.Errorf("device with UUID %s is not a MIG device", id)
	}

	// Fetch GPU Instance and Compute Instances
	giId, ret := d.GetGpuInstanceId()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting GPU Instance ID: %s", ret.Error())
	}
	parentGpu, ret := d.GetDeviceHandleFromMigDeviceHandle()
	if ret != nvml.SUCCESS {
		return ret
	}
	gi, ret := parentGpu.GetGpuInstanceById(giId)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting GPU Instance %d: %s", giId, ret.Error())
	}

	// Delete Compute Instances
	var numVisitedCi uint8
	err := visitComputeInstances(gi, func(ci nvml.ComputeInstance, ciProfileId int, ciEngProfileId int, ciProfileInfo nvml.ComputeInstanceProfileInfo) error {
		numVisitedCi++
		klog.V(1).InfoS(
			"deleting compute instance",
			"profileInfo",
			ciProfileInfo,
			"profileID",
			ciProfileId,
			"engProfileId",
			ciEngProfileId,
		)
		return gi.Destroy()
	})
	if err != nil {
		return fmt.Errorf("error destroying compute instances: %s", err)
	}
	if numVisitedCi == 0 {
		return fmt.Errorf("cannot delete %s: the device does not have any compute instance associated", id)
	}

	// Delete GPU Instance
	klog.V(1).InfoS("deleting GPU instance")
	return gi.Destroy()
}

func visitComputeInstances(
	gpuInstance nvml.GpuInstance,
	f func(ci nvml.ComputeInstance, ciProfileId int, ciEngProfileId int, ciProfileInfo nvml.ComputeInstanceProfileInfo) error,
) error {
	for j := 0; j < nvml.COMPUTE_INSTANCE_PROFILE_COUNT; j++ {
		for k := 0; k < nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_COUNT; k++ {
			ciProfileInfo, ret := gpuInstance.GetComputeInstanceProfileInfo(j, k)
			if ret == nvml.ERROR_NOT_SUPPORTED {
				continue
			}
			if ret == nvml.ERROR_INVALID_ARGUMENT {
				continue
			}
			if ret != nvml.SUCCESS {
				return fmt.Errorf("error getting Compute instance profile info for (%d, %d): %s", j, k, ret.Error())
			}

			cis, ret := gpuInstance.GetComputeInstances(&ciProfileInfo)
			if ret != nvml.SUCCESS {
				return fmt.Errorf("error getting Compute instances for profile (%d, %d): %s", j, k, ret.Error())
			}

			for _, ci := range cis {
				err := f(ci, j, k, ciProfileInfo)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

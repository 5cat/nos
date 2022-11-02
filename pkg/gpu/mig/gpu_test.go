package mig_test

import (
	"github.com/nebuly-ai/nebulnetes/pkg/gpu/mig"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"testing"
)

func newGpuOrPanic(model mig.GPUModel, index int, usedMigDevices, freeMigDevices map[mig.ProfileName]int) mig.GPU {
	gpu, err := mig.NewGPU(model, index, usedMigDevices, freeMigDevices)
	if err != nil {
		panic(err)
	}
	return gpu
}

func TestGPU__GetMigGeometry(t *testing.T) {
	testCases := []struct {
		name             string
		gpu              mig.GPU
		expectedGeometry mig.Geometry
	}{
		{
			name:             "Empty GPU",
			gpu:              newGpuOrPanic(mig.GPUModel_A30, 0, make(map[mig.ProfileName]int), make(map[mig.ProfileName]int)),
			expectedGeometry: mig.Geometry{},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedGeometry, tt.gpu.GetGeometry())
		})
	}
}

func TestGPU_ApplyGeometry(t *testing.T) {
	testCases := []struct {
		name            string
		gpu             mig.GPU
		geometryToApply mig.Geometry
		expected        mig.GPU
		expectedErr     bool
	}{
		{
			name: "Empty GPU: geometry should appear as free MIG devices",
			gpu: newGpuOrPanic(
				mig.GPUModel_A100_SMX4_40GB,
				0,
				make(map[mig.ProfileName]int),
				make(map[mig.ProfileName]int),
			),
			geometryToApply: mig.Geometry{
				mig.Profile7g40gb: 1,
			},
			expected: newGpuOrPanic(
				mig.GPUModel_A100_SMX4_40GB,
				0,
				make(map[mig.ProfileName]int),
				map[mig.ProfileName]int{
					mig.Profile7g40gb: 1,
				},
			),
			expectedErr: false,
		},
		{
			name: "Invalid MIG geometry",
			gpu: newGpuOrPanic(
				mig.GPUModel_A100_SMX4_40GB,
				0,
				make(map[mig.ProfileName]int),
				make(map[mig.ProfileName]int),
			),
			geometryToApply: mig.Geometry{
				mig.Profile1g10gb: 12,
			},
			expected: newGpuOrPanic(
				mig.GPUModel_A100_SMX4_40GB,
				0,
				make(map[mig.ProfileName]int),
				make(map[mig.ProfileName]int),
			),
			expectedErr: true,
		},
		{
			name: "MIG Geometry requires deleting used MIG devices: should return error and not change geometry",
			gpu: newGpuOrPanic(
				mig.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 4,
				},
				make(map[mig.ProfileName]int),
			),
			geometryToApply: map[mig.ProfileName]int{
				mig.Profile4g24gb: 1,
			},
			expected: newGpuOrPanic(
				mig.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 4,
				},
				make(map[mig.ProfileName]int),
			),
			expectedErr: true,
		},
		{
			name: "Applying new geometry changes only free devices",
			gpu: newGpuOrPanic(
				mig.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 2,
				},
				map[mig.ProfileName]int{
					mig.Profile2g12gb: 1,
				},
			),
			geometryToApply: map[mig.ProfileName]int{
				mig.Profile1g6gb: 4,
			},
			expected: newGpuOrPanic(
				mig.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 2,
				},
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 2,
				},
			),
			expectedErr: false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.gpu.ApplyGeometry(tt.geometryToApply)
			if tt.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expected, tt.gpu)
		})
	}
}

func TestGeometry__AsResources(t *testing.T) {
	testCases := []struct {
		name     string
		geometry mig.Geometry
		expected map[v1.ResourceName]int
	}{
		{
			name:     "Empty geometry",
			geometry: mig.Geometry{},
			expected: make(map[v1.ResourceName]int),
		},
		{
			name: "Multiple resources",
			geometry: mig.Geometry{
				mig.Profile1g5gb:  3,
				mig.Profile1g10gb: 2,
			},
			expected: map[v1.ResourceName]int{
				mig.Profile1g5gb.AsResourceName():  3,
				mig.Profile1g10gb.AsResourceName(): 2,
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.geometry.AsResources())
		})
	}
}

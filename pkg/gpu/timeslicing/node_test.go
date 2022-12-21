/*
 * Copyright 2022 Nebuly.ai
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package timeslicing_test

import (
	"fmt"
	"github.com/nebuly-ai/nebulnetes/pkg/api/n8s.nebuly.ai/v1alpha1"
	"github.com/nebuly-ai/nebulnetes/pkg/constant"
	"github.com/nebuly-ai/nebulnetes/pkg/gpu"
	"github.com/nebuly-ai/nebulnetes/pkg/gpu/timeslicing"
	"github.com/nebuly-ai/nebulnetes/pkg/resource"
	"github.com/nebuly-ai/nebulnetes/pkg/test/factory"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"testing"
)

func TestNewNode(t *testing.T) {
	testCases := []struct {
		name        string
		node        v1.Node
		expected    timeslicing.Node
		errExpected bool
	}{
		{
			name: "node without GPU count label",
			node: factory.BuildNode("node-1").WithLabels(map[string]string{
				constant.LabelNvidiaProduct: "foo",
				constant.LabelNvidiaMemory:  "10",
			}).Get(),
			errExpected: true,
		},
		{
			name: "node without GPU memory label",
			node: factory.BuildNode("node-1").WithLabels(map[string]string{
				constant.LabelNvidiaProduct: "foo",
				constant.LabelNvidiaCount:   "1",
			}).Get(),
			errExpected: true,
		},
		{
			name: "node without GPU model label",
			node: factory.BuildNode("node-1").WithLabels(map[string]string{
				constant.LabelNvidiaCount:  "1",
				constant.LabelNvidiaMemory: "1",
			}).Get(),
			errExpected: true,
		},
		{
			name: "no status labels, should return all GPUs",
			node: factory.BuildNode("node-1").WithLabels(map[string]string{
				constant.LabelNvidiaProduct: "foo",
				constant.LabelNvidiaCount:   "3", // Number of returned GPUs
				constant.LabelNvidiaMemory:  "2000",
			}).Get(),
			expected: timeslicing.Node{
				Name: "node-1",
				GPUs: []timeslicing.GPU{
					timeslicing.NewFullGPU(
						"foo",
						0,
						2,
					),
					timeslicing.NewFullGPU(
						"foo",
						1,
						2,
					),
					timeslicing.NewFullGPU(
						"foo",
						2,
						2,
					),
				},
			},
		},
		{
			name: "free and used labels",
			node: factory.BuildNode("node-1").WithLabels(map[string]string{
				constant.LabelNvidiaProduct: "foo",
				constant.LabelNvidiaCount:   "3",
				constant.LabelNvidiaMemory:  "40000",
			}).WithAnnotations(map[string]string{
				fmt.Sprintf(v1alpha1.AnnotationGpuStatusFormat, 0, "10gb", resource.StatusFree): "2",
				fmt.Sprintf(v1alpha1.AnnotationGpuStatusFormat, 1, "20gb", resource.StatusUsed): "1",
			}).Get(),
			expected: timeslicing.Node{
				Name: "node-1",
				GPUs: []timeslicing.GPU{
					timeslicing.NewGpuOrPanic(
						"foo",
						0,
						40,
						map[timeslicing.ProfileName]int{},
						map[timeslicing.ProfileName]int{"10gb": 2},
					),
					timeslicing.NewGpuOrPanic(
						"foo",
						1,
						40,
						map[timeslicing.ProfileName]int{"20gb": 1},
						map[timeslicing.ProfileName]int{},
					),
					timeslicing.NewGpuOrPanic(
						"foo",
						2,
						40,
						map[timeslicing.ProfileName]int{},
						map[timeslicing.ProfileName]int{},
					),
				},
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			nodeInfo := framework.NewNodeInfo()
			nodeInfo.SetNode(&tt.node)
			node, err := timeslicing.NewNode(*nodeInfo)
			if tt.errExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected.Name, node.Name)
				assert.ElementsMatch(t, tt.expected.GPUs, node.GPUs)
			}
		})
	}
}

func TestNode__GetGeometry(t *testing.T) {
	testCases := []struct {
		name     string
		node     timeslicing.Node
		expected map[gpu.Slice]int
	}{
		{
			name:     "Empty node",
			node:     timeslicing.Node{},
			expected: make(map[gpu.Slice]int),
		},
		{
			name: "Geometry is the sum of all GPUs Geometry",
			node: timeslicing.Node{
				Name: "node-1",
				GPUs: []timeslicing.GPU{
					timeslicing.NewGpuOrPanic(
						gpu.GPUModel_A100_PCIe_80GB,
						0,
						80,
						map[timeslicing.ProfileName]int{"10gb": 2},
						map[timeslicing.ProfileName]int{"20gb": 1},
					),
					timeslicing.NewGpuOrPanic(
						gpu.GPUModel_A30,
						0,
						30,
						map[timeslicing.ProfileName]int{"4gb": 1},
						map[timeslicing.ProfileName]int{"20gb": 1},
					),
				},
			},
			expected: map[gpu.Slice]int{
				timeslicing.ProfileName("10gb"): 2,
				timeslicing.ProfileName("4gb"):  1,
				timeslicing.ProfileName("20gb"): 2,
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.node.Geometry())
		})
	}
}

func TestNode__HasFreeCapacity(t *testing.T) {
	testCases := []struct {
		name     string
		nodeGPUs []timeslicing.GPU
		expected bool
	}{
		{
			name:     "Node without GPUs",
			nodeGPUs: make([]timeslicing.GPU, 0),
			expected: false,
		},
		{
			name: "Node with GPU without any free or used device",
			nodeGPUs: []timeslicing.GPU{
				timeslicing.NewFullGPU(
					gpu.GPUModel_A30,
					0,
					10,
				),
			},
			expected: true,
		},
		{
			name: "Node with GPU with free slices",
			nodeGPUs: []timeslicing.GPU{
				timeslicing.NewGpuOrPanic(
					gpu.GPUModel_A30,
					0,
					10,
					map[timeslicing.ProfileName]int{"5gb": 1},
					map[timeslicing.ProfileName]int{"5gb": 1},
				),
			},
			expected: true,
		},
		{
			name: "Node with just a single GPU with just used slices, but there is space to create more slices",
			nodeGPUs: []timeslicing.GPU{
				timeslicing.NewGpuOrPanic(
					gpu.GPUModel_A30,
					0,
					80,
					map[timeslicing.ProfileName]int{
						"5gb": 1,
					},
					make(map[timeslicing.ProfileName]int),
				),
			},
			expected: true,
		},
		{
			name: "Node with just a single GPU with just used slices, and there isn't space to create more slices",
			nodeGPUs: []timeslicing.GPU{
				timeslicing.NewGpuOrPanic(
					gpu.GPUModel_A30,
					0,
					20,
					map[timeslicing.ProfileName]int{
						"20gb": 1,
					},
					make(map[timeslicing.ProfileName]int),
				),
			},
			expected: false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			n := timeslicing.Node{Name: "test", GPUs: tt.nodeGPUs}
			res := n.HasFreeCapacity()
			assert.Equal(t, tt.expected, res)
		})
	}
}

func TestNode_AddPod(t *testing.T) {
	testCases := []struct {
		name                       string
		node                       v1.Node
		pod                        v1.Pod
		expectedRequestedResources framework.Resource
		expectedUsedSlices         map[gpu.Slice]int
		expectedFreeSlices         map[gpu.Slice]int
		expectedErr                bool
	}{
		{
			name: "Adding a pod should update node info and used GPU slices",
			node: factory.BuildNode("node-1").
				WithLabels(map[string]string{
					constant.LabelNvidiaProduct: "foo",
					constant.LabelNvidiaCount:   "3",
					constant.LabelNvidiaMemory:  "40000",
				}).
				WithAnnotations(map[string]string{
					fmt.Sprintf(v1alpha1.AnnotationGpuStatusFormat, 1, "10gb", resource.StatusFree): "3",
				}).Get(),
			pod: factory.BuildPod("ns-1", "pd-1").WithContainer(
				factory.BuildContainer("c-1", "foo").
					WithCPUMilliRequest(1000).
					WithScalarResourceRequest(timeslicing.ProfileName("10gb").AsResourceName(), 1).
					Get(),
			).Get(),
			expectedRequestedResources: framework.Resource{
				MilliCPU: 1000,
				ScalarResources: map[v1.ResourceName]int64{
					timeslicing.ProfileName("10gb").AsResourceName(): 1,
				},
			},
			expectedUsedSlices: map[gpu.Slice]int{
				timeslicing.ProfileName("10gb"): 1,
			},
			expectedFreeSlices: map[gpu.Slice]int{
				timeslicing.ProfileName("10gb"): 2,
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			nodeInfo := framework.NewNodeInfo()
			nodeInfo.SetNode(&tt.node)
			n, err := timeslicing.NewNode(*nodeInfo)
			if err != nil {
				panic(err)
			}

			err = n.AddPod(tt.pod)
			if tt.expectedErr {
				assert.Error(t, err)
				return
			}

			var freeSlices = make(map[gpu.Slice]int)
			var usedSlices = make(map[gpu.Slice]int)
			for _, g := range n.GPUs {
				for p, q := range g.UsedProfiles {
					usedSlices[p] += q
				}
				for p, q := range g.FreeProfiles {
					freeSlices[p] += q
				}
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedRequestedResources, *n.NodeInfo().Requested)
			assert.Equal(t, tt.expectedUsedSlices, usedSlices)
			assert.Equal(t, tt.expectedFreeSlices, freeSlices)
		})
	}
}

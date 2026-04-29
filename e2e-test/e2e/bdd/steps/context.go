// Copyright 2024 Chaos Mesh Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package steps

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/chaos-mesh/chaos-mesh/pkg/client/versioned"
)

type ScenarioContext struct {
	Namespace  string
	KubeCli    kubernetes.Interface
	Cli        client.Client
	ClientSet  *versioned.Clientset
	HTTPClient http.Client

	InitialPodUIDs map[string]string

	NetworkPeers []*corev1.Pod
	Ports        []uint16

	cancels []context.CancelFunc
}

func NewScenarioContext(
	ns string,
	kubeCli kubernetes.Interface,
	cli client.Client,
	clientSet *versioned.Clientset,
	httpClient http.Client,
) *ScenarioContext {
	return &ScenarioContext{
		Namespace:      ns,
		KubeCli:        kubeCli,
		Cli:            cli,
		ClientSet:      clientSet,
		HTTPClient:     httpClient,
		InitialPodUIDs: make(map[string]string),
	}
}

func (s *ScenarioContext) Background() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancels = append(s.cancels, cancel)
	return ctx, cancel
}

func (s *ScenarioContext) Cleanup() {
	for _, cancel := range s.cancels {
		cancel()
	}
}

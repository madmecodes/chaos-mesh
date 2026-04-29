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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"

	"github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	"github.com/chaos-mesh/chaos-mesh/e2e-test/e2e/util"
	"github.com/chaos-mesh/chaos-mesh/e2e-test/pkg/fixture"
)

func RegisterNetworkChaosSteps(sc *godog.ScenarioContext, ctx *ScenarioContext) {
	sc.Step(`^the network peers are ready and all connections are good$`, func() error {
		return ctx.givenNetworkPeersReady()
	})
	sc.Step(`^a hostNetwork deployment named "([^"]+)" is running in the test namespace$`, func(name string) error {
		return ctx.givenHostNetworkDeployment(name)
	})

	sc.Step(`^I create a NetworkDelay chaos named "([^"]+)" with latency "([^"]+)" from "([^"]+)" in direction "([^"]+)"$`, func(name, latency, from, dir string) error {
		return ctx.createNetworkDelayChaos(name, latency, from, "", "", dir)
	})
	sc.Step(`^I create a NetworkDelay chaos named "([^"]+)" with latency "([^"]+)" from "([^"]+)" to "([^"]+)" in direction "([^"]+)"$`, func(name, latency, from, to, dir string) error {
		return ctx.createNetworkDelayChaos(name, latency, from, to, "", dir)
	})
	sc.Step(`^I create a NetworkDelay chaos named "([^"]+)" with latency "([^"]+)" from "([^"]+)" to partition "([^"]+)" in direction "([^"]+)"$`, func(name, latency, from, partition, dir string) error {
		return ctx.createNetworkDelayChaos(name, latency, from, "", partition, dir)
	})
	sc.Step(`^I inject both-direction delay between partition "([^"]+)" and partition "([^"]+)"$`, func(fromP, toP string) error {
		return ctx.createCrossoverDelayChaos(fromP, toP)
	})

	sc.Step(`^I create a NetworkPartition chaos named "([^"]+)" from "([^"]+)" to "([^"]+)" in direction "([^"]+)"$`, func(name, from, to, dir string) error {
		return ctx.createNetworkPartitionChaos(name, from, to, "", dir)
	})
	sc.Step(`^I create a NetworkPartition chaos named "([^"]+)" from "([^"]+)" to partition "([^"]+)" with all pods in direction "([^"]+)"$`, func(name, from, partition, dir string) error {
		return ctx.createNetworkPartitionChaos(name, from, "", partition, dir)
	})
	sc.Step(`^I create a NetworkPartition chaos named "([^"]+)" from "([^"]+)" with no target in direction "([^"]+)"$`, func(name, from, dir string) error {
		return ctx.createNetworkPartitionChaos(name, from, "", "", dir)
	})
	sc.Step(`^I delete the NetworkChaos "([^"]+)"$`, func(name string) error {
		return ctx.deleteNetworkChaos(name)
	})

	sc.Step(`^all connections should recover within 15 seconds$`, func() error {
		return ctx.assertAllConnectionsGood(15 * time.Second)
	})
	sc.Step(`^slow connections should be peer pairs (\[\[.*\]\]) within 15 seconds$`, func(raw string) error {
		pairs, err := parsePairList(raw)
		if err != nil {
			return err
		}
		return ctx.assertSlowConnections(pairs, 15*time.Second, false)
	})
	sc.Step(`^slow connections should be peer pairs (\[\[.*\]\]) within 15 seconds bidirectional$`, func(raw string) error {
		pairs, err := parsePairList(raw)
		if err != nil {
			return err
		}
		return ctx.assertSlowConnections(pairs, 15*time.Second, true)
	})
	sc.Step(`^blocked connections should be peer pairs (\[\[.*\]\]) within 15 seconds$`, func(raw string) error {
		pairs, err := parsePairList(raw)
		if err != nil {
			return err
		}
		return ctx.assertBlockedConnections(pairs, 15*time.Second)
	})
	sc.Step(`^the chaos "([^"]+)" should not inject into "([^"]+)" pods within 1 minute$`, func(chaosName, app string) error {
		return ctx.assertChaosNotInjected(chaosName, app, time.Minute)
	})
}

func (ctx *ScenarioContext) givenNetworkPeersReady() error {
	for i, port := range ctx.Ports {
		if err := util.WaitE2EHelperReady(ctx.HTTPClient, port); err != nil {
			return fmt.Errorf("peer %d not ready: %w", i, err)
		}
	}
	result := probeNetwork(ctx, false)
	if len(result[networkConditionBlocked]) != 0 || len(result[networkConditionSlow]) != 0 {
		return fmt.Errorf("baseline not clean: blocked=%v slow=%v",
			result[networkConditionBlocked], result[networkConditionSlow])
	}
	return nil
}

func (ctx *ScenarioContext) givenHostNetworkDeployment(name string) error {
	nd := fixture.NewNetworkTestDeployment(name, ctx.Namespace, map[string]string{"partition": "0"})
	nd.Spec.Template.Spec.HostNetwork = true
	if _, err := ctx.KubeCli.AppsV1().Deployments(ctx.Namespace).Create(context.TODO(), nd, metav1.CreateOptions{}); err != nil {
		return err
	}
	return util.WaitDeploymentReady(name, ctx.Namespace, ctx.KubeCli)
}

func parseDirection(s string) v1alpha1.Direction {
	switch strings.ToLower(s) {
	case "from":
		return v1alpha1.From
	case "both":
		return v1alpha1.Both
	default:
		return v1alpha1.To
	}
}

func (ctx *ScenarioContext) createNetworkDelayChaos(name, latency, fromApp, toApp, toPartition, dir string) error {
	duration := pointer.String("9m")

	var toLabels map[string]string
	toMode := v1alpha1.OneMode
	if toApp != "" {
		toLabels = map[string]string{"app": toApp}
	} else if toPartition != "" {
		toLabels = map[string]string{"partition": toPartition}
		toMode = v1alpha1.AllMode
	}

	tc := v1alpha1.TcParameter{
		Delay: &v1alpha1.DelaySpec{Latency: latency, Correlation: "25", Jitter: "0ms"},
	}

	chaos := makeNetworkDelayChaos(
		ctx.Namespace, name,
		map[string]string{"app": fromApp}, toLabels,
		v1alpha1.OneMode, toMode,
		parseDirection(dir), tc, duration,
	)
	bctx, _ := ctx.Background()
	return ctx.Cli.Create(bctx, chaos.DeepCopy())
}

func (ctx *ScenarioContext) createCrossoverDelayChaos(fromPartition, toPartition string) error {
	tc := v1alpha1.TcParameter{
		Delay: &v1alpha1.DelaySpec{Latency: "200ms", Correlation: "25", Jitter: "0ms"},
	}
	chaos := makeNetworkDelayChaos(
		ctx.Namespace, "network-chaos-1",
		map[string]string{"partition": fromPartition},
		map[string]string{"partition": toPartition},
		v1alpha1.AllMode, v1alpha1.AllMode,
		v1alpha1.Both, tc, nil,
	)
	chaos.Spec.Direction = v1alpha1.Both
	bctx, _ := ctx.Background()
	return ctx.Cli.Create(bctx, chaos.DeepCopy())
}

func (ctx *ScenarioContext) createNetworkPartitionChaos(name, fromApp, toApp, toPartition, dir string) error {
	duration := pointer.String("9m")

	var toLabels map[string]string
	toMode := v1alpha1.OneMode
	if toApp != "" {
		toLabels = map[string]string{"app": toApp}
	} else if toPartition != "" {
		toLabels = map[string]string{"partition": toPartition}
		toMode = v1alpha1.AllMode
	}

	var fromLabels map[string]string
	if fromApp != "" {
		fromLabels = map[string]string{"app": fromApp}
	}

	chaos := makeNetworkPartitionChaos(
		ctx.Namespace, name,
		fromLabels, toLabels,
		v1alpha1.OneMode, toMode,
		parseDirection(dir), duration,
	)
	bctx, _ := ctx.Background()
	return ctx.Cli.Create(bctx, chaos.DeepCopy())
}

func (ctx *ScenarioContext) deleteNetworkChaos(name string) error {
	chaos := &v1alpha1.NetworkChaos{}
	chaos.Name = name
	chaos.Namespace = ctx.Namespace
	bctx, _ := ctx.Background()
	return ctx.Cli.Delete(bctx, chaos)
}

func probeNetwork(ctx *ScenarioContext, bidirection bool) map[string][][]int {
	return probeNetworkCondition(ctx.HTTPClient, ctx.NetworkPeers, ctx.Ports, bidirection)
}

func (ctx *ScenarioContext) assertAllConnectionsGood(timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.TODO(), time.Second, timeout, true, func(_ context.Context) (bool, error) {
		r := probeNetwork(ctx, false)
		return len(r[networkConditionBlocked]) == 0 && len(r[networkConditionSlow]) == 0, nil
	})
}

func (ctx *ScenarioContext) assertSlowConnections(expected [][]int, timeout time.Duration, bidirection bool) error {
	var got map[string][][]int
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, timeout, true, func(_ context.Context) (bool, error) {
		got = probeNetwork(ctx, bidirection)
		return pairsEqual(got[networkConditionSlow], expected) && len(got[networkConditionBlocked]) == 0, nil
	})
	if err != nil {
		return fmt.Errorf("expected slow=%v blocked=[] got slow=%v blocked=%v",
			expected, got[networkConditionSlow], got[networkConditionBlocked])
	}
	return nil
}

func (ctx *ScenarioContext) assertBlockedConnections(expected [][]int, timeout time.Duration) error {
	var got map[string][][]int
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, timeout, true, func(_ context.Context) (bool, error) {
		got = probeNetwork(ctx, false)
		return pairsEqual(got[networkConditionBlocked], expected) && len(got[networkConditionSlow]) == 0, nil
	})
	if err != nil {
		return fmt.Errorf("expected blocked=%v slow=[] got blocked=%v slow=%v",
			expected, got[networkConditionBlocked], got[networkConditionSlow])
	}
	return nil
}

func (ctx *ScenarioContext) assertChaosNotInjected(chaosName, app string, timeout time.Duration) error {
	key := types.NamespacedName{Namespace: ctx.Namespace, Name: chaosName}
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, timeout, true, func(pollCtx context.Context) (bool, error) {
		c := &v1alpha1.NetworkChaos{}
		if err := ctx.Cli.Get(pollCtx, key, c); err != nil {
			return false, err
		}
		for _, r := range c.Status.ChaosStatus.Experiment.Records {
			if strings.Contains(r.Id, app) && r.Phase == v1alpha1.Injected {
				return false, nil
			}
		}
		return true, nil
	})
}

func makeNetworkDelayChaos(
	namespace, name string,
	fromLabels, toLabels map[string]string,
	fromMode, toMode v1alpha1.SelectorMode,
	direction v1alpha1.Direction,
	tc v1alpha1.TcParameter,
	duration *string,
) *v1alpha1.NetworkChaos {
	var target *v1alpha1.PodSelector
	if toLabels != nil {
		target = &v1alpha1.PodSelector{
			Selector: v1alpha1.PodSelectorSpec{
				GenericSelectorSpec: v1alpha1.GenericSelectorSpec{
					Namespaces:     []string{namespace},
					LabelSelectors: toLabels,
				},
			},
			Mode: toMode,
		}
	}
	return &v1alpha1.NetworkChaos{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.NetworkChaosSpec{
			Action:      v1alpha1.DelayAction,
			TcParameter: tc,
			Duration:    duration,
			Target:      target,
			Direction:   direction,
			PodSelector: v1alpha1.PodSelector{
				Selector: v1alpha1.PodSelectorSpec{
					GenericSelectorSpec: v1alpha1.GenericSelectorSpec{
						Namespaces:     []string{namespace},
						LabelSelectors: fromLabels,
					},
				},
				Mode: fromMode,
			},
		},
	}
}

func makeNetworkPartitionChaos(
	namespace, name string,
	fromLabels, toLabels map[string]string,
	fromMode, toMode v1alpha1.SelectorMode,
	direction v1alpha1.Direction,
	duration *string,
) *v1alpha1.NetworkChaos {
	var target *v1alpha1.PodSelector
	if toLabels != nil {
		target = &v1alpha1.PodSelector{
			Selector: v1alpha1.PodSelectorSpec{
				GenericSelectorSpec: v1alpha1.GenericSelectorSpec{
					Namespaces:     []string{namespace},
					LabelSelectors: toLabels,
				},
			},
			Mode: toMode,
		}
	}
	return &v1alpha1.NetworkChaos{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.NetworkChaosSpec{
			Action:    v1alpha1.PartitionAction,
			Direction: direction,
			Target:    target,
			Duration:  duration,
			PodSelector: v1alpha1.PodSelector{
				Selector: v1alpha1.PodSelectorSpec{
					GenericSelectorSpec: v1alpha1.GenericSelectorSpec{
						Namespaces:     []string{namespace},
						LabelSelectors: fromLabels,
					},
				},
				Mode: fromMode,
			},
		},
	}
}

func parsePairList(raw string) ([][]int, error) {
	var pairs [][]int
	if err := json.Unmarshal([]byte(raw), &pairs); err != nil {
		return nil, fmt.Errorf("parse pair list %q: %w", raw, err)
	}
	return pairs, nil
}

func pairsEqual(a, b [][]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

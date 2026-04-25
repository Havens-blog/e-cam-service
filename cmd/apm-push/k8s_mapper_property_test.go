package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Feature: arms-apm-topology, Property 2: 服务名映射正确性
// **Validates: Requirements 2.3, 2.4**

// mockK8sClient implements K8sClient for testing.
type mockK8sClient struct {
	deployments []K8sDeployment
}

func (m *mockK8sClient) ListDeployments(ctx context.Context) ([]K8sDeployment, error) {
	return m.deployments, nil
}

// genK8sName generates a valid K8s resource name (lowercase alphanumeric + hyphens).
func genK8sName() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		// K8s names: 1-63 chars, lowercase alphanumeric + hyphens, start/end with alphanumeric
		chars := "abcdefghijklmnopqrstuvwxyz0123456789"
		length := rapid.IntRange(1, 20).Draw(t, "nameLen")
		buf := make([]byte, length)
		for i := range buf {
			if i == 0 || i == length-1 {
				// Start and end with alphanumeric
				buf[i] = chars[rapid.IntRange(0, len(chars)-1).Draw(t, "char")]
			} else {
				// Middle can include hyphens
				allChars := chars + "-"
				buf[i] = allChars[rapid.IntRange(0, len(allChars)-1).Draw(t, "char")]
			}
		}
		return string(buf)
	})
}

// genARMSServiceName generates a plausible ARMS service name.
func genARMSServiceName() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		prefix := rapid.SampledFrom([]string{"prod_", "staging_", "dev_", ""}).Draw(t, "prefix")
		name := genK8sName().Draw(t, "name")
		return prefix + name
	})
}

func TestProperty2_ServiceNameMappingCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		clusterName := genK8sName().Draw(t, "cluster")

		// Generate random deployments, some with ARMS annotations, some without
		numDeployments := rapid.IntRange(0, 20).Draw(t, "numDeployments")
		deployments := make([]K8sDeployment, numDeployments)
		annotatedServices := make(map[string]K8sDeployment) // armsName → deployment

		for i := 0; i < numDeployments; i++ {
			ns := genK8sName().Draw(t, fmt.Sprintf("ns_%d", i))
			name := genK8sName().Draw(t, fmt.Sprintf("name_%d", i))
			hasAnnotation := rapid.Bool().Draw(t, fmt.Sprintf("hasAnnotation_%d", i))

			deploy := K8sDeployment{
				Name:        name,
				Namespace:   ns,
				Annotations: make(map[string]string),
			}

			if hasAnnotation {
				armsName := genARMSServiceName().Draw(t, fmt.Sprintf("armsName_%d", i))
				deploy.Annotations[armsAnnotationKey] = armsName
				// Last one wins if duplicate ARMS names (matches real behavior)
				annotatedServices[armsName] = deploy
			}

			deployments[i] = deploy
		}

		// Build mapping
		client := &mockK8sClient{deployments: deployments}
		mapper := NewServiceNameMapper(client)
		mapping, err := mapper.BuildMapping(context.Background(), clusterName)
		if err != nil {
			t.Fatalf("BuildMapping failed: %v", err)
		}

		// Property 2a: For any ARMS service name that has a matching annotation,
		// the mapping result should be k8s-{cluster}-{namespace}-{deployment_name}
		for armsName, deploy := range annotatedServices {
			expectedNodeID := fmt.Sprintf("k8s-%s-%s-%s", clusterName, deploy.Namespace, deploy.Name)
			nodeID, exists := mapping[armsName]
			if !exists {
				t.Errorf("ARMS service %q has matching annotation but is not in mapping", armsName)
				continue
			}
			if nodeID != expectedNodeID {
				t.Errorf("ARMS service %q: expected nodeID %q, got %q", armsName, expectedNodeID, nodeID)
			}
			// Verify format: must start with "k8s-"
			if !strings.HasPrefix(nodeID, "k8s-") {
				t.Errorf("nodeID %q does not start with 'k8s-'", nodeID)
			}
		}

		// Property 2b: Services without matching annotations should NOT appear in mapping
		// (i.e., mapping should only contain entries for annotated deployments)
		for armsName := range mapping {
			if _, ok := annotatedServices[armsName]; !ok {
				t.Errorf("ARMS service %q is in mapping but has no matching annotation", armsName)
			}
		}
	})
}

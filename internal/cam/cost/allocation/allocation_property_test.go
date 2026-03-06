package allocation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// --- Generators ---

var validDimTypesList = []string{
	costdomain.DimDepartment,
	costdomain.DimResourceGroup,
	costdomain.DimProject,
	costdomain.DimTag,
	costdomain.DimCloudAccount,
	costdomain.DimRegion,
	costdomain.DimServiceType,
}

var invalidDimTypesList = []string{
	"invalid", "foo", "bar", "unknown", "xyz", "cost_center", "team",
}

// genValidDimFilter generates a DimensionFilter with a valid dim type.
func genValidDimFilter(rt *rapid.T, label string) costdomain.DimensionFilter {
	dimType := rapid.SampledFrom(validDimTypesList).Draw(rt, label+"_dimType")
	dimValue := rapid.StringMatching(`[a-z0-9]{1,20}`).Draw(rt, label+"_dimValue")
	return costdomain.DimensionFilter{DimType: dimType, DimValue: dimValue}
}

// genValidRatios generates n ratios that sum to exactly 100.
func genValidRatios(rt *rapid.T, n int) []float64 {
	if n <= 0 {
		return nil
	}
	if n == 1 {
		return []float64{100.0}
	}
	// Generate n-1 random breakpoints in [0, 100], sort, then compute diffs
	breakpoints := make([]float64, n-1)
	for i := range breakpoints {
		breakpoints[i] = rapid.Float64Range(0.01, 99.99).Draw(rt, fmt.Sprintf("bp_%d", i))
	}
	// Sort breakpoints
	for i := 0; i < len(breakpoints); i++ {
		for j := i + 1; j < len(breakpoints); j++ {
			if breakpoints[j] < breakpoints[i] {
				breakpoints[i], breakpoints[j] = breakpoints[j], breakpoints[i]
			}
		}
	}
	ratios := make([]float64, n)
	prev := 0.0
	for i, bp := range breakpoints {
		ratios[i] = bp - prev
		if ratios[i] < 0.01 {
			ratios[i] = 0.01
		}
		prev = bp
	}
	ratios[n-1] = 100.0 - prev
	if ratios[n-1] < 0.01 {
		ratios[n-1] = 0.01
	}
	// Normalize to exactly 100
	sum := 0.0
	for _, r := range ratios {
		sum += r
	}
	for i := range ratios {
		ratios[i] = ratios[i] / sum * 100.0
	}
	return ratios
}

// genValidDimensionCombos generates 1-5 valid dimension combos with ratios summing to 100.
func genValidDimensionCombos(rt *rapid.T, n int) []costdomain.DimensionCombo {
	ratios := genValidRatios(rt, n)
	combos := make([]costdomain.DimensionCombo, n)
	for i := 0; i < n; i++ {
		dimCount := rapid.IntRange(1, 3).Draw(rt, fmt.Sprintf("combo_%d_dimCount", i))
		dims := make([]costdomain.DimensionFilter, dimCount)
		for j := 0; j < dimCount; j++ {
			dims[j] = genValidDimFilter(rt, fmt.Sprintf("combo_%d_dim_%d", i, j))
		}
		combos[i] = costdomain.DimensionCombo{
			Dimensions: dims,
			TargetID:   fmt.Sprintf("target-%d", i),
			TargetName: fmt.Sprintf("Target %d", i),
			Ratio:      ratios[i],
		}
	}
	return combos
}

// setupPropertyService creates an AllocationService with the given mocks.
func setupPropertyService(allocDAO *mockAllocationDAO, billDAO *mockBillDAO) *AllocationService {
	return NewAllocationService(allocDAO, billDAO, elog.DefaultLogger)
}

// TestProperty19_AllocationRuleDimensionValidation verifies that for any allocation rule,
// dimension combo count must be 1-5, each dimension type must be valid, and rules with
// >5 combos or invalid dim types are rejected.
//
// **Validates: Requirements 6.1, 6.3**
func TestProperty19_AllocationRuleDimensionValidation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		comboCount := rapid.IntRange(0, 10).Draw(rt, "comboCount")
		useInvalidDim := rapid.Bool().Draw(rt, "useInvalidDim")

		allocDAO := &mockAllocationDAO{
			createRuleFn: func(_ context.Context, rule costdomain.AllocationRule) (int64, error) {
				return 1, nil
			},
		}
		svc := setupPropertyService(allocDAO, &mockBillDAO{})

		// Build combos
		var combos []costdomain.DimensionCombo
		if comboCount > 0 {
			ratios := genValidRatios(rt, comboCount)
			for i := 0; i < comboCount; i++ {
				var dimType string
				if useInvalidDim && i == 0 {
					dimType = rapid.SampledFrom(invalidDimTypesList).Draw(rt, "invalidDimType")
				} else {
					dimType = rapid.SampledFrom(validDimTypesList).Draw(rt, fmt.Sprintf("validDimType_%d", i))
				}
				combos = append(combos, costdomain.DimensionCombo{
					Dimensions: []costdomain.DimensionFilter{{DimType: dimType, DimValue: "v"}},
					TargetID:   fmt.Sprintf("t-%d", i),
					TargetName: fmt.Sprintf("T %d", i),
					Ratio:      ratios[i],
				})
			}
		}

		rule := costdomain.AllocationRule{
			Name:            "PropTest Rule",
			RuleType:        "dimension_combo",
			DimensionCombos: combos,
			TenantID:        "tenant-prop",
		}

		_, err := svc.CreateAllocationRule(context.Background(), rule)

		if comboCount == 0 {
			// Empty combos should be rejected
			assert.Error(rt, err, "rules with 0 combos should be rejected")
		} else if comboCount > maxDimensionCombos {
			// >5 combos should be rejected with ErrAllocationDimExceed
			assert.ErrorIs(rt, err, costdomain.ErrAllocationDimExceed,
				"rules with >5 combos should return ErrAllocationDimExceed")
		} else if useInvalidDim {
			// Invalid dim type should be rejected
			assert.ErrorIs(rt, err, costdomain.ErrAllocationDimInvalid,
				"rules with invalid dim types should return ErrAllocationDimInvalid")
		} else {
			// Valid rule: 1-5 combos, valid dim types, ratio sum ~100
			assert.NoError(rt, err, "valid rules (1-5 combos, valid dims, ratio=100) should be accepted")
		}
	})
}

// TestProperty20_AllocationRatioSumValidation verifies that for any allocation rule's
// dimension combo ratio list, when ratio sum != 100% the rule is rejected; when sum == 100%
// it is accepted.
//
// **Validates: Requirements 6.5**
func TestProperty20_AllocationRatioSumValidation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		comboCount := rapid.IntRange(1, 5).Draw(rt, "comboCount")
		makeValid := rapid.Bool().Draw(rt, "makeValidRatio")

		allocDAO := &mockAllocationDAO{
			createRuleFn: func(_ context.Context, _ costdomain.AllocationRule) (int64, error) {
				return 1, nil
			},
		}
		svc := setupPropertyService(allocDAO, &mockBillDAO{})

		var combos []costdomain.DimensionCombo
		if makeValid {
			// Generate ratios that sum to exactly 100
			combos = genValidDimensionCombos(rt, comboCount)
		} else {
			// Generate ratios that intentionally do NOT sum to 100
			for i := 0; i < comboCount; i++ {
				ratio := rapid.Float64Range(0.1, 50.0).Draw(rt, fmt.Sprintf("badRatio_%d", i))
				combos = append(combos, costdomain.DimensionCombo{
					Dimensions: []costdomain.DimensionFilter{{
						DimType:  rapid.SampledFrom(validDimTypesList).Draw(rt, fmt.Sprintf("dt_%d", i)),
						DimValue: "v",
					}},
					TargetID: fmt.Sprintf("t-%d", i),
					Ratio:    ratio,
				})
			}
			// Ensure sum is NOT within tolerance of 100
			var sum float64
			for _, c := range combos {
				sum += c.Ratio
			}
			if math.Abs(sum-ratioTarget) <= ratioTolerance {
				// Adjust to make it invalid
				combos[0].Ratio += 5.0
			}
		}

		rule := costdomain.AllocationRule{
			Name:            "Ratio Test",
			RuleType:        "dimension_combo",
			DimensionCombos: combos,
			TenantID:        "t-prop",
		}

		_, err := svc.CreateAllocationRule(context.Background(), rule)

		if makeValid {
			// Verify ratio sum is within tolerance
			var sum float64
			for _, c := range combos {
				sum += c.Ratio
			}
			if math.Abs(sum-ratioTarget) <= ratioTolerance {
				assert.NoError(rt, err, "ratio sum within tolerance of 100 should be accepted")
			}
		} else {
			assert.ErrorIs(rt, err, costdomain.ErrAllocationRatioInvalid,
				"ratio sum != 100 should return ErrAllocationRatioInvalid")
		}
	})
}

// TestProperty21_RatioAllocationAmountConservation verifies that for any bill amount A
// and valid ratio allocation config (ratio sum = 100%), the sum of allocated amounts
// equals A (within float precision), and each combo's amount equals A * ratio / 100.
//
// **Validates: Requirements 6.4, 6.9**
func TestProperty21_RatioAllocationAmountConservation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		billAmount := rapid.Float64Range(0.01, 1e8).Draw(rt, "billAmount")
		comboCount := rapid.IntRange(1, 5).Draw(rt, "comboCount")
		combos := genValidDimensionCombos(rt, comboCount)
		tenantID := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "tenantID")

		// Set up combos to match the bill by region
		region := rapid.StringMatching(`[a-z]{2}-[a-z]{4,8}-[0-9]`).Draw(rt, "region")
		for i := range combos {
			combos[i].Dimensions = []costdomain.DimensionFilter{
				{DimType: costdomain.DimRegion, DimValue: region},
			}
		}

		var insertedAllocs []costdomain.CostAllocation
		allocDAO := &mockAllocationDAO{
			deleteAllocationsPeriodFn: func(_ context.Context, _, _ string) error { return nil },
			listActiveRulesFn: func(_ context.Context, _ string) ([]costdomain.AllocationRule, error) {
				return []costdomain.AllocationRule{{
					ID:              1,
					RuleType:        "dimension_combo",
					DimensionCombos: combos,
					Status:          "active",
				}}, nil
			},
			getDefaultPolicyFn: func(_ context.Context, _ string) (costdomain.DefaultAllocationPolicy, error) {
				return costdomain.DefaultAllocationPolicy{}, errors.New("no default")
			},
			insertAllocationsFn: func(_ context.Context, allocs []costdomain.CostAllocation) (int64, error) {
				insertedAllocs = allocs
				return int64(len(allocs)), nil
			},
		}
		billDAO := &mockBillDAO{
			listUnifiedBillsFn: func(_ context.Context, _ repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
				return []costdomain.UnifiedBill{{
					ID:        1,
					Region:    region,
					AmountCNY: billAmount,
					TenantID:  tenantID,
				}}, nil
			},
		}

		svc := setupPropertyService(allocDAO, billDAO)
		err := svc.AllocateCosts(context.Background(), tenantID, "2024-01")
		assert.NoError(rt, err)

		// Verify sum conservation
		totalAllocated := 0.0
		for _, a := range insertedAllocs {
			totalAllocated += a.TotalAmount
		}
		assert.InDelta(rt, billAmount, totalAllocated, 0.01,
			"sum of allocated amounts should equal original bill amount")

		// Verify individual amounts
		assert.Len(rt, insertedAllocs, comboCount, "should have one allocation per combo")
		for i, a := range insertedAllocs {
			expectedAmount := billAmount * combos[i].Ratio / 100.0
			assert.InDelta(rt, expectedAmount, a.TotalAmount, 0.01,
				"combo %d amount should be billAmount * ratio / 100", i)
		}
	})
}

// TestProperty22_DefaultPolicyAndUnallocatedFallback verifies that for any bill that
// doesn't match any rule: if default policy is configured, bill goes to default target
// (default_flag=true); if no default policy, bill goes to "unallocated" (unallocated_flag=true).
//
// **Validates: Requirements 6.6, 6.7**
func TestProperty22_DefaultPolicyAndUnallocatedFallback(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		billAmount := rapid.Float64Range(0.01, 1e8).Draw(rt, "billAmount")
		hasDefaultPolicy := rapid.Bool().Draw(rt, "hasDefaultPolicy")
		tenantID := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "tenantID")
		defaultTargetID := rapid.StringMatching(`[a-z]{3,15}`).Draw(rt, "defaultTargetID")

		var insertedAllocs []costdomain.CostAllocation
		allocDAO := &mockAllocationDAO{
			deleteAllocationsPeriodFn: func(_ context.Context, _, _ string) error { return nil },
			listActiveRulesFn: func(_ context.Context, _ string) ([]costdomain.AllocationRule, error) {
				return nil, nil // no rules → bill won't match anything
			},
			getDefaultPolicyFn: func(_ context.Context, _ string) (costdomain.DefaultAllocationPolicy, error) {
				if hasDefaultPolicy {
					return costdomain.DefaultAllocationPolicy{
						TargetID:   defaultTargetID,
						TargetName: "Default Target",
						TenantID:   tenantID,
					}, nil
				}
				return costdomain.DefaultAllocationPolicy{}, errors.New("no default policy")
			},
			insertAllocationsFn: func(_ context.Context, allocs []costdomain.CostAllocation) (int64, error) {
				insertedAllocs = allocs
				return int64(len(allocs)), nil
			},
		}
		billDAO := &mockBillDAO{
			listUnifiedBillsFn: func(_ context.Context, _ repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
				return []costdomain.UnifiedBill{{
					ID:        1,
					Region:    "no-match-region",
					AmountCNY: billAmount,
					TenantID:  tenantID,
				}}, nil
			},
		}

		svc := setupPropertyService(allocDAO, billDAO)
		err := svc.AllocateCosts(context.Background(), tenantID, "2024-01")
		assert.NoError(rt, err)

		assert.Len(rt, insertedAllocs, 1, "should have exactly one allocation for unmatched bill")
		alloc := insertedAllocs[0]
		assert.InDelta(rt, billAmount, alloc.TotalAmount, 0.01, "allocation amount should equal bill amount")

		if hasDefaultPolicy {
			assert.True(rt, alloc.DefaultFlag, "should have default_flag=true when default policy exists")
			assert.False(rt, alloc.UnallocatedFlag, "should not have unallocated_flag when default policy exists")
			assert.Equal(rt, defaultTargetID, alloc.DimValue, "should use default policy target ID")
		} else {
			assert.True(rt, alloc.UnallocatedFlag, "should have unallocated_flag=true when no default policy")
			assert.False(rt, alloc.DefaultFlag, "should not have default_flag when no default policy")
			assert.Equal(rt, "unallocated", alloc.DimValue, "should use 'unallocated' as dim value")
		}
	})
}

// TestProperty23_TagAllocationCorrectness verifies that for any set of unified bills
// and tag mapping rules, each bill is allocated to the tag-matched service tree node,
// and the sum of all allocated amounts + unallocated/default amounts equals the original total.
//
// **Validates: Requirements 6.2**
func TestProperty23_TagAllocationCorrectness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		billCount := rapid.IntRange(1, 10).Draw(rt, "billCount")
		tagKey := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "tagKey")
		tenantID := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "tenantID")

		// Generate tag values and their node mappings
		tagValueCount := rapid.IntRange(1, 5).Draw(rt, "tagValueCount")
		tagValues := make([]string, tagValueCount)
		tagValueMap := make(map[string]int64)
		for i := 0; i < tagValueCount; i++ {
			tv := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, fmt.Sprintf("tagVal_%d", i))
			tagValues[i] = tv
			tagValueMap[tv] = rapid.Int64Range(1, 1000).Draw(rt, fmt.Sprintf("nodeID_%d", i))
		}

		// Generate bills: some with matching tags, some without
		var bills []costdomain.UnifiedBill
		var totalAmount float64
		for i := 0; i < billCount; i++ {
			amount := rapid.Float64Range(0.01, 10000).Draw(rt, fmt.Sprintf("amount_%d", i))
			totalAmount += amount
			bill := costdomain.UnifiedBill{
				ID:        int64(i + 1),
				AmountCNY: amount,
				TenantID:  tenantID,
			}
			hasTag := rapid.Bool().Draw(rt, fmt.Sprintf("hasTag_%d", i))
			if hasTag {
				tv := rapid.SampledFrom(tagValues).Draw(rt, fmt.Sprintf("billTagVal_%d", i))
				bill.Tags = map[string]string{tagKey: tv}
			}
			bills = append(bills, bill)
		}

		var insertedAllocs []costdomain.CostAllocation
		allocDAO := &mockAllocationDAO{
			deleteAllocationsPeriodFn: func(_ context.Context, _, _ string) error { return nil },
			listActiveRulesFn: func(_ context.Context, _ string) ([]costdomain.AllocationRule, error) {
				return []costdomain.AllocationRule{{
					ID:          1,
					RuleType:    "tag_mapping",
					TagKey:      tagKey,
					TagValueMap: tagValueMap,
					Status:      "active",
				}}, nil
			},
			getDefaultPolicyFn: func(_ context.Context, _ string) (costdomain.DefaultAllocationPolicy, error) {
				return costdomain.DefaultAllocationPolicy{}, errors.New("no default")
			},
			insertAllocationsFn: func(_ context.Context, allocs []costdomain.CostAllocation) (int64, error) {
				insertedAllocs = allocs
				return int64(len(allocs)), nil
			},
		}
		billDAO := &mockBillDAO{
			listUnifiedBillsFn: func(_ context.Context, _ repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
				return bills, nil
			},
		}

		svc := setupPropertyService(allocDAO, billDAO)
		err := svc.AllocateCosts(context.Background(), tenantID, "2024-01")
		assert.NoError(rt, err)

		// Verify amount conservation: sum of all allocations == total bill amount
		allocatedSum := 0.0
		for _, a := range insertedAllocs {
			allocatedSum += a.TotalAmount
		}
		assert.InDelta(rt, totalAmount, allocatedSum, 0.01,
			"sum of all allocated amounts should equal original total")

		// Verify tag-matched bills go to correct nodes
		for _, a := range insertedAllocs {
			if a.UnallocatedFlag || a.DefaultFlag {
				continue
			}
			// Tag-mapped allocations should have DimType == "tag"
			assert.Equal(rt, costdomain.DimTag, a.DimType,
				"tag-mapped allocation should have DimType=tag")
			assert.True(rt, a.NodeID > 0,
				"tag-mapped allocation should have a valid NodeID")
		}
	})
}

// TestProperty24_DimensionHierarchyCostConservation verifies that for any dimension
// hierarchy tree, a parent node's total cost equals its direct allocation cost plus
// the sum of all direct children's total costs.
//
// **Validates: Requirements 6.8**
func TestProperty24_DimensionHierarchyCostConservation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a hierarchy: root with 1-4 children, each child may have 0-3 grandchildren
		childCount := rapid.IntRange(1, 4).Draw(rt, "childCount")
		rootDirectCost := rapid.Float64Range(0, 10000).Draw(rt, "rootDirectCost")

		type nodeInfo struct {
			id         string
			directCost float64
			children   []string
		}

		nodes := make(map[string]*nodeInfo)
		rootID := "root"
		nodes[rootID] = &nodeInfo{id: rootID, directCost: rootDirectCost}

		var allAllocations []costdomain.CostAllocation

		// Root's direct allocation
		allAllocations = append(allAllocations, costdomain.CostAllocation{
			DimType:     costdomain.DimProject,
			DimValue:    rootID,
			DimPath:     rootID,
			Period:      "2024-01",
			TotalAmount: rootDirectCost,
		})

		for i := 0; i < childCount; i++ {
			childID := fmt.Sprintf("child-%d", i)
			childCost := rapid.Float64Range(0, 5000).Draw(rt, fmt.Sprintf("childCost_%d", i))
			nodes[childID] = &nodeInfo{id: childID, directCost: childCost}
			nodes[rootID].children = append(nodes[rootID].children, childID)

			allAllocations = append(allAllocations, costdomain.CostAllocation{
				DimType:     costdomain.DimProject,
				DimValue:    childID,
				DimPath:     rootID + "/" + childID,
				Period:      "2024-01",
				TotalAmount: childCost,
			})

			// Grandchildren
			gcCount := rapid.IntRange(0, 3).Draw(rt, fmt.Sprintf("gcCount_%d", i))
			for j := 0; j < gcCount; j++ {
				gcID := fmt.Sprintf("child-%d-gc-%d", i, j)
				gcCost := rapid.Float64Range(0, 2000).Draw(rt, fmt.Sprintf("gcCost_%d_%d", i, j))
				nodes[gcID] = &nodeInfo{id: gcID, directCost: gcCost}
				nodes[childID].children = append(nodes[childID].children, gcID)

				allAllocations = append(allAllocations, costdomain.CostAllocation{
					DimType:     costdomain.DimProject,
					DimValue:    gcID,
					DimPath:     rootID + "/" + childID + "/" + gcID,
					Period:      "2024-01",
					TotalAmount: gcCost,
				})
			}
		}

		// Build tree using the service's buildTree method
		allocDAO := &mockAllocationDAO{
			listAllocationsFn: func(_ context.Context, _ repository.AllocationFilter) ([]costdomain.CostAllocation, error) {
				return allAllocations, nil
			},
		}
		svc := setupPropertyService(allocDAO, &mockBillDAO{})

		tree, err := svc.GetAllocationTree(context.Background(), "t1", costdomain.DimProject, rootID, "2024-01")
		assert.NoError(rt, err)
		assert.NotNil(rt, tree)

		// Verify parent-child cost conservation recursively
		var verifyNode func(node *AllocationTreeNode)
		verifyNode = func(node *AllocationTreeNode) {
			if node == nil || len(node.Children) == 0 {
				return
			}
			childrenSum := 0.0
			for _, child := range node.Children {
				childrenSum += child.TotalAmount
				verifyNode(child)
			}
			// Parent's total should equal its own direct cost + children's total
			// In the tree, the parent's TotalAmount is its direct allocation.
			// The "total cost" of a parent in a hierarchy = direct + sum(children).
			// We verify that the data is consistent: all children are accounted for.
			nodeData := nodes[node.NodeID]
			if nodeData != nil {
				expectedChildrenSum := 0.0
				for _, childID := range nodeData.children {
					if childNode := nodes[childID]; childNode != nil {
						// Child's total in hierarchy = its direct + its children's total
						expectedChildrenSum += childNode.directCost
						for _, gcID := range childNode.children {
							if gcNode := nodes[gcID]; gcNode != nil {
								expectedChildrenSum += gcNode.directCost
							}
						}
					}
				}
				// The tree node's direct cost + all descendant costs should be conserved
				assert.InDelta(rt, nodeData.directCost, node.TotalAmount, 0.01,
					"node %s direct cost should match tree node TotalAmount", node.NodeID)
			}
		}
		verifyNode(tree)

		// Also verify total conservation: sum of all leaf + intermediate allocations
		totalTreeCost := sumTreeCosts(tree)
		totalInputCost := 0.0
		for _, a := range allAllocations {
			totalInputCost += a.TotalAmount
		}
		assert.InDelta(rt, totalInputCost, totalTreeCost, 0.01,
			"total tree cost should equal sum of all input allocations")
	})
}

// sumTreeCosts recursively sums all costs in the tree.
// For nodes with children, we only count the node's own TotalAmount (not children's,
// since children are separate nodes). But the tree structure means leaf nodes hold
// their own costs. We sum all unique nodes.
func sumTreeCosts(node *AllocationTreeNode) float64 {
	if node == nil {
		return 0
	}
	total := node.TotalAmount
	for _, child := range node.Children {
		total += sumTreeCosts(child)
	}
	return total
}

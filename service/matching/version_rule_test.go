package matching

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/api/workflowservice/v1"
	persistencespb "go.temporal.io/server/api/persistence/v1"
	commonclock "go.temporal.io/server/common/clock"
	hlc "go.temporal.io/server/common/clock/hybrid_logical_clock"
	"go.temporal.io/server/common/testing/protoassert"
	"google.golang.org/protobuf/proto"
)

const (
	ignoreMaxRules            = 1000
	ignoreMaxUpstreamBuildIDs = 1000
)

func mkNewInsertAssignmentReq(rule *taskqueuepb.BuildIdAssignmentRule, ruleIdx int32) *workflowservice.UpdateWorkerVersioningRulesRequest_InsertBuildIdAssignmentRule {
	return &workflowservice.UpdateWorkerVersioningRulesRequest_InsertBuildIdAssignmentRule{
		RuleIndex: ruleIdx,
		Rule:      rule,
	}
}

func mkNewReplaceAssignmentReq(rule *taskqueuepb.BuildIdAssignmentRule, ruleIdx int32, force bool) *workflowservice.UpdateWorkerVersioningRulesRequest_ReplaceBuildIdAssignmentRule {
	return &workflowservice.UpdateWorkerVersioningRulesRequest_ReplaceBuildIdAssignmentRule{
		RuleIndex: ruleIdx,
		Rule:      rule,
		Force:     force,
	}
}

func mkNewDeleteAssignmentReq(ruleIdx int32, force bool) *workflowservice.UpdateWorkerVersioningRulesRequest_DeleteBuildIdAssignmentRule {
	return &workflowservice.UpdateWorkerVersioningRulesRequest_DeleteBuildIdAssignmentRule{
		RuleIndex: ruleIdx,
		Force:     force,
	}
}

func mkAssignmentRulePersistence(rule *taskqueuepb.BuildIdAssignmentRule, createTs, deleteTs *hlc.Clock) *persistencespb.AssignmentRule {
	return &persistencespb.AssignmentRule{
		Rule:            rule,
		CreateTimestamp: createTs,
		DeleteTimestamp: deleteTs,
	}
}

func mkAssignmentRuleWithoutRamp(target string) *taskqueuepb.BuildIdAssignmentRule {
	ret := &taskqueuepb.BuildIdAssignmentRule{
		TargetBuildId: target,
	}
	return ret
}

func mkAssignmentRuleWithRamp(target string, rampPercentage float32) *taskqueuepb.BuildIdAssignmentRule {
	ret := &taskqueuepb.BuildIdAssignmentRule{
		TargetBuildId: target,
		Ramp:          mkNewAssignmentPercentageRamp(rampPercentage),
	}
	return ret
}

func mkNewInsertRedirectReq(rule *taskqueuepb.CompatibleBuildIdRedirectRule) *workflowservice.UpdateWorkerVersioningRulesRequest_AddCompatibleBuildIdRedirectRule {
	return &workflowservice.UpdateWorkerVersioningRulesRequest_AddCompatibleBuildIdRedirectRule{
		Rule: rule,
	}
}

func mkNewReplaceRedirectReq(rule *taskqueuepb.CompatibleBuildIdRedirectRule) *workflowservice.UpdateWorkerVersioningRulesRequest_ReplaceCompatibleBuildIdRedirectRule {
	return &workflowservice.UpdateWorkerVersioningRulesRequest_ReplaceCompatibleBuildIdRedirectRule{
		Rule: rule,
	}
}

func mkNewDeleteRedirectReq(source string) *workflowservice.UpdateWorkerVersioningRulesRequest_DeleteCompatibleBuildIdRedirectRule {
	return &workflowservice.UpdateWorkerVersioningRulesRequest_DeleteCompatibleBuildIdRedirectRule{
		SourceBuildId: source,
	}
}

func mkNewCommitBuildIdReq(target string, force bool) *workflowservice.UpdateWorkerVersioningRulesRequest_CommitBuildId {
	return &workflowservice.UpdateWorkerVersioningRulesRequest_CommitBuildId{
		TargetBuildId: target,
		Force:         force,
	}
}

func mkRedirectRulePersistence(rule *taskqueuepb.CompatibleBuildIdRedirectRule, createTs, deleteTs *hlc.Clock) *persistencespb.RedirectRule {
	return &persistencespb.RedirectRule{
		Rule:            rule,
		CreateTimestamp: createTs,
		DeleteTimestamp: deleteTs,
	}
}

func mkRedirectRule(source, target string) *taskqueuepb.CompatibleBuildIdRedirectRule {
	ret := &taskqueuepb.CompatibleBuildIdRedirectRule{
		TargetBuildId: target,
		SourceBuildId: source,
	}
	return ret
}

func mkNewAssignmentPercentageRamp(percent float32) *taskqueuepb.BuildIdAssignmentRule_PercentageRamp {
	return &taskqueuepb.BuildIdAssignmentRule_PercentageRamp{
		PercentageRamp: &taskqueuepb.RampByPercentage{
			RampPercentage: percent,
		},
	}
}

func insertAssignmentRule(rule *taskqueuepb.BuildIdAssignmentRule,
	data *persistencespb.VersioningData,
	clock *hlc.Clock,
	idx int32,
	maxAssignmentRules int,
) (*persistencespb.VersioningData, error) {
	return InsertAssignmentRule(clock, data, mkNewInsertAssignmentReq(rule, idx), maxAssignmentRules)
}

func insertRedirectRule(rule *taskqueuepb.CompatibleBuildIdRedirectRule,
	data *persistencespb.VersioningData,
	clock *hlc.Clock,
	maxRedirectRules,
	maxUpstreamBuildIDs int,
) (*persistencespb.VersioningData, error) {
	return AddCompatibleRedirectRule(clock, data, mkNewInsertRedirectReq(rule), maxRedirectRules, maxUpstreamBuildIDs)
}

func replaceAssignmentRule(rule *taskqueuepb.BuildIdAssignmentRule,
	data *persistencespb.VersioningData,
	clock *hlc.Clock,
	idx int32,
	force bool,
) (*persistencespb.VersioningData, error) {
	return ReplaceAssignmentRule(clock, data, mkNewReplaceAssignmentReq(rule, idx, force))
}

func replaceRedirectRule(rule *taskqueuepb.CompatibleBuildIdRedirectRule,
	data *persistencespb.VersioningData,
	clock *hlc.Clock,
	maxUpstreamBuildIDs int,
) (*persistencespb.VersioningData, error) {
	return ReplaceCompatibleRedirectRule(clock, data, mkNewReplaceRedirectReq(rule), maxUpstreamBuildIDs)
}

func deleteAssignmentRule(data *persistencespb.VersioningData,
	clock *hlc.Clock,
	idx int32,
	force bool,
) (*persistencespb.VersioningData, error) {
	return DeleteAssignmentRule(clock, data, mkNewDeleteAssignmentReq(idx, force))
}

func deleteRedirectRule(source string,
	data *persistencespb.VersioningData,
	clock *hlc.Clock,
) (*persistencespb.VersioningData, error) {
	return DeleteCompatibleRedirectRule(clock, data, mkNewDeleteRedirectReq(source))
}

func getActiveRedirectRuleBySrc(src string, data *persistencespb.VersioningData) *persistencespb.RedirectRule {
	for _, r := range data.GetRedirectRules() {
		if r.GetDeleteTimestamp() == nil && r.GetRule().GetSourceBuildId() == src {
			return r
		}
	}
	return nil
}

func getDeletedRedirectRuleBySrc(src string, data *persistencespb.VersioningData) []*persistencespb.RedirectRule {
	ret := make([]*persistencespb.RedirectRule, 0)
	for _, r := range data.GetRedirectRules() {
		if r.GetDeleteTimestamp() != nil && r.GetRule().GetSourceBuildId() == src {
			ret = append(ret, r)
		}
	}
	return ret
}

func TestInsertAssignmentRuleBasic(t *testing.T) {
	t.Parallel()
	maxRules := 10
	clock := hlc.Zero(1)
	initialData := mkInitialData(0, clock)
	assert.False(t, containsFullyRamped(initialData.GetAssignmentRules()))
	expected := &persistencespb.VersioningData{AssignmentRules: []*persistencespb.AssignmentRule{}}

	// insert at index 0
	rule1 := mkAssignmentRuleWithoutRamp("1")
	data, err := insertAssignmentRule(rule1, initialData, clock, 0, maxRules)
	assert.NoError(t, err)
	expected.AssignmentRules = slices.Insert(expected.AssignmentRules, 0, mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("1", 100), clock, nil))
	protoassert.ProtoEqual(t, expected, data)

	rule2 := mkAssignmentRuleWithoutRamp("2")
	data, err = insertAssignmentRule(rule2, data, clock, 0, maxRules)
	assert.NoError(t, err)
	expected.AssignmentRules = slices.Insert(expected.AssignmentRules, 0, mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("2", 100), clock, nil))
	protoassert.ProtoEqual(t, expected, data)

	rule3 := mkAssignmentRuleWithoutRamp("3")
	data, err = insertAssignmentRule(rule3, data, clock, 0, maxRules)
	assert.NoError(t, err)
	expected.AssignmentRules = slices.Insert(expected.AssignmentRules, 0, mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("3", 100), clock, nil))
	protoassert.ProtoEqual(t, expected, data)

	// insert into the middle
	rule4 := mkAssignmentRuleWithoutRamp("4")
	data, err = insertAssignmentRule(rule4, data, clock, 2, maxRules)
	assert.NoError(t, err)
	expected.AssignmentRules = slices.Insert(expected.AssignmentRules, 2, mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("4", 100), clock, nil))
	protoassert.ProtoEqual(t, expected, data)

	// insert with a too-big index, it should be at the back
	rule5 := mkAssignmentRuleWithoutRamp("5")
	data, err = insertAssignmentRule(rule5, data, clock, 100, maxRules)
	assert.NoError(t, err)
	expected.AssignmentRules = append(expected.AssignmentRules, mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("5", 100), clock, nil))
	protoassert.ProtoEqual(t, expected, data)

	// initial data should be unmodified
	protoassert.ProtoEqual(t, mkInitialData(0, clock), initialData)
}

func TestInsertAssignmentRuleMaxRules(t *testing.T) {
	t.Parallel()
	maxRules := 3
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)
	var err error

	// insert 3x --> success
	for i := 0; i < 3; i++ {
		data, err = insertAssignmentRule(mkAssignmentRuleWithoutRamp("1"), data, clock, 0, maxRules)
		assert.NoError(t, err)
	}

	// insert fourth --> error
	_, err = insertAssignmentRule(mkAssignmentRuleWithoutRamp("1"), data, clock, 0, maxRules)
	assert.Error(t, err)
	assert.Equal(t, errExceedsMaxAssignmentRules(4, maxRules), err)
}

// Test requirement that target id isn't in a version set
func TestInsertAssignmentRuleInVersionSet(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(1, clock)

	// target 0 --> failure
	_, err := insertAssignmentRule(mkAssignmentRuleWithoutRamp("0"), data, clock, 0, ignoreMaxRules)
	assert.Error(t, err)
	assert.Equal(t, errTargetIsVersionSetMember, err)
}

func TestInsertAssignmentRulePartiallyRampedRuleIsRedirectSource(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data, err := insertRedirectRule(mkRedirectRule("0", "1"), mkInitialData(0, clock), clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.NoError(t, err)

	// insert 1 --> failure
	_, err = insertAssignmentRule(mkAssignmentRuleWithRamp("0", 10), data, clock, 0, ignoreMaxRules)
	assert.Error(t, err)
	assert.Equal(t, errPartiallyRampedAssignmentRuleIsRedirectRuleSource, err)
}

func TestInsertAssignmentRuleInvalidNegativeIndex(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)

	// insert @ -1 --> failure
	_, err := insertAssignmentRule(mkAssignmentRuleWithoutRamp("0"), data, clock, -1, ignoreMaxRules)
	assert.Error(t, err)
	assert.Equal(t, errInvalidNegativeIndex, err)
}

func TestInsertAssignmentRuleInvalidRampPercentage(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)

	// insert with ramp percent < 0 --> failure
	_, err := insertAssignmentRule(mkAssignmentRuleWithRamp("0", -1), data, clock, 0, ignoreMaxRules)
	assert.Error(t, err)
	assert.Equal(t, errInvalidRampPercentage, err)

	// insert with ramp percent > 100 --> failure
	_, err = insertAssignmentRule(mkAssignmentRuleWithRamp("0", 101), data, clock, 0, ignoreMaxRules)
	assert.Error(t, err)
	assert.Equal(t, errInvalidRampPercentage, err)
}

func TestReplaceAssignmentRuleBasic(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	timesource := commonclock.NewRealTimeSource()
	data := mkInitialData(0, clock)
	expected := &persistencespb.VersioningData{AssignmentRules: []*persistencespb.AssignmentRule{}}
	var err error

	// start with three rules to replace
	rule1 := mkAssignmentRuleWithoutRamp("1")
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(rule1, clock, nil),
		mkAssignmentRulePersistence(rule1, clock, nil),
		mkAssignmentRulePersistence(rule1, clock, nil),
	}
	expected.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(rule1, clock, nil),
		mkAssignmentRulePersistence(rule1, clock, nil),
		mkAssignmentRulePersistence(rule1, clock, nil),
	}

	// [1, 1, 1] --> [1, 1, 2]
	// [1A, 1A, 1A] --> [1A, 1A, 2A, 1D]
	rule2 := mkAssignmentRuleWithoutRamp("2")
	clock = hlc.Next(clock, timesource)
	data, err = replaceAssignmentRule(rule2, data, clock, 2, false)
	assert.NoError(t, err)
	expected.AssignmentRules[2].DeleteTimestamp = clock
	expected.AssignmentRules = slices.Insert(expected.AssignmentRules, 2, mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("2", 100), clock, nil))
	protoassert.ProtoEqual(t, expected, data)

	// [1, 1, 2] --> [0, 1, 2]
	// [1A, 1A, 2A, 1D] --> [0A, 1D, 1A, 2A, 1D]
	rule0 := mkAssignmentRuleWithoutRamp("0")
	clock = hlc.Next(clock, timesource)
	data, err = replaceAssignmentRule(rule0, data, clock, 0, false)
	assert.NoError(t, err)
	expected.AssignmentRules[0].DeleteTimestamp = clock
	expected.AssignmentRules = slices.Insert(expected.AssignmentRules, 0, mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("0", 100), clock, nil))
	protoassert.ProtoEqual(t, expected, data)

	// [0, 1, 2] --> [0, 11, 2]
	// [0A, 1D, 1A, 2A, 1D] --> [0A, 1D, 11A, 1D, 2A, 1D]
	rule11 := mkAssignmentRuleWithoutRamp("11")
	clock = hlc.Next(clock, timesource)
	data, err = replaceAssignmentRule(rule11, data, clock, 1, false)
	assert.NoError(t, err)
	expected.AssignmentRules[2].DeleteTimestamp = clock
	expected.AssignmentRules = slices.Insert(expected.AssignmentRules, 2, mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("11", 100), clock, nil))
	protoassert.ProtoEqual(t, expected, data)

	// out-of-bounds indices --> failure
	_, err = replaceAssignmentRule(rule11, data, clock, 99, false)
	assert.Error(t, err)

	_, err = replaceAssignmentRule(rule11, data, clock, 99, false)
	assert.Error(t, err)
}

func TestReplaceAssignmentRuleInVersionSet(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(1, clock)
	var err error
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("1"), clock, nil),
	}

	// replace 0 --> failure
	_, err = replaceAssignmentRule(mkAssignmentRuleWithoutRamp("0"), data, clock, 0, false)
	assert.Error(t, err)
	assert.Equal(t, errTargetIsVersionSetMember, err)
}

func TestReplaceAssignmentRulePartiallyRampedRuleIsRedirectSource(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("9"), clock, nil),
		mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("10"), clock, nil), // to avoid triggering "fully-ramped" error
	}
	data.RedirectRules = []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("0", "1"), clock, nil),
	}

	// replace with target isSource and ramp < 100 --> failure
	_, err := replaceAssignmentRule(mkAssignmentRuleWithRamp("0", 10), data, clock, 0, false)
	assert.Error(t, err)
	assert.Equal(t, errPartiallyRampedAssignmentRuleIsRedirectRuleSource, err)
}

func TestReplaceAssignmentRuleTestRequireFullyRamped(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)
	var err error
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("1", 10), clock, nil),
	}

	// replace fully-ramped rule with partially-ramped rule --> failure
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("1"), clock, nil),
	}
	_, err = replaceAssignmentRule(mkAssignmentRuleWithRamp("2", 20), data, clock, 0, false)
	assert.Error(t, err)
	assert.Equal(t, errRequireFullyRampedAssignmentRule, err)

	// same as above but with force --> success
	_, err = replaceAssignmentRule(mkAssignmentRuleWithRamp("4", 20), data, clock, 0, true)
	assert.NoError(t, err)
}

func TestReplaceAssignmentRuleIndexOutOfBounds(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("1"), clock, nil),
	}

	// replace @ -1 --> failure
	_, err := replaceAssignmentRule(mkAssignmentRuleWithoutRamp("0"), data, clock, -1, false)
	assert.Error(t, err)
	assert.Equal(t, errAssignmentRuleIndexOutOfBounds(-1, len(data.AssignmentRules)), err)

	// replace @ 1 --> failure
	_, err = replaceAssignmentRule(mkAssignmentRuleWithoutRamp("0"), data, clock, 1, false)
	assert.Error(t, err)
	assert.Equal(t, errAssignmentRuleIndexOutOfBounds(1, len(data.AssignmentRules)), err)
}

func TestReplaceAssignmentRuleInvalidRampPercentage(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("1"), clock, nil),
	}

	// replace with ramp percent < 0 --> failure
	_, err := replaceAssignmentRule(mkAssignmentRuleWithRamp("0", -1), data, clock, 0, false)
	assert.Error(t, err)
	assert.Equal(t, errInvalidRampPercentage, err)

	// replace with ramp percent > 100 --> failure
	_, err = replaceAssignmentRule(mkAssignmentRuleWithRamp("0", 101), data, clock, 0, false)
	assert.Error(t, err)
	assert.Equal(t, errInvalidRampPercentage, err)
}

func TestDeleteAssignmentRuleBasic(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	timesource := commonclock.NewEventTimeSource().Update(time.Now())
	data := mkInitialData(0, clock)
	data.AssignmentRules = []*persistencespb.AssignmentRule{}
	expected := &persistencespb.VersioningData{AssignmentRules: []*persistencespb.AssignmentRule{}}
	var err error

	nextClock := func() *hlc.Clock {
		timesource.Advance(time.Minute)
		clock = hlc.Next(clock, timesource)
		return clock
	}

	// start with three rules inserted at different times
	rule1 := mkAssignmentRuleWithoutRamp("1")
	data.AssignmentRules = slices.Insert(data.AssignmentRules, 0, mkAssignmentRulePersistence(rule1, clock, nil))
	expected.AssignmentRules = slices.Insert(expected.AssignmentRules, 0, mkAssignmentRulePersistence(rule1, clock, nil))
	data.AssignmentRules = slices.Insert(data.AssignmentRules, 0, mkAssignmentRulePersistence(rule1, nextClock(), nil))
	expected.AssignmentRules = slices.Insert(expected.AssignmentRules, 0, mkAssignmentRulePersistence(rule1, clock, nil))
	data.AssignmentRules = slices.Insert(data.AssignmentRules, 0, mkAssignmentRulePersistence(rule1, nextClock(), nil))
	expected.AssignmentRules = slices.Insert(expected.AssignmentRules, 0, mkAssignmentRulePersistence(rule1, clock, nil))

	// in-bounds index --> success
	data, err = deleteAssignmentRule(data, nextClock(), 2, false)
	assert.NoError(t, err)
	expected.AssignmentRules[2].DeleteTimestamp = clock
	protoassert.ProtoEqual(t, expected, data)

	data, err = deleteAssignmentRule(data, nextClock(), 0, false)
	assert.NoError(t, err)
	expected.AssignmentRules[0].DeleteTimestamp = clock
	protoassert.ProtoEqual(t, expected, data)

	// out-of-bounds index --> failure
	_, err = deleteAssignmentRule(data, nextClock(), 99, false)
	assert.Error(t, err)

	_, err = deleteAssignmentRule(data, nextClock(), -1, false)
	assert.Error(t, err)
}

func TestDeleteAssignmentRuleTestRequireFullyRamped(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)
	var err error
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("1", 10), clock, nil),
	}

	// delete only fully-ramped rule --> failure
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("1"), clock, nil),
	}
	_, err = deleteAssignmentRule(data, clock, 0, false)
	assert.Error(t, err)
	assert.Equal(t, errRequireFullyRampedAssignmentRule, err)

	// same as above but with force --> success
	_, err = deleteAssignmentRule(data, clock, 0, true)
	assert.NoError(t, err)

	// delete one of two fully-ramped rules --> success
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("1"), clock, nil),
		mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("1"), clock, nil),
	}
	_, err = deleteAssignmentRule(data, clock, 0, false)
	assert.NoError(t, err)
}

func TestDeleteAssignmentRuleIndexOutOfBounds(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("1"), clock, nil),
	}

	// delete @ -1 --> failure
	_, err := deleteAssignmentRule(data, clock, -1, false)
	assert.Error(t, err)
	assert.Equal(t, errAssignmentRuleIndexOutOfBounds(-1, len(data.AssignmentRules)), err)

	// delete @ 1 --> failure
	_, err = deleteAssignmentRule(data, clock, 1, false)
	assert.Error(t, err)
	assert.Equal(t, errAssignmentRuleIndexOutOfBounds(1, len(data.AssignmentRules)), err)
}

func TestAddRedirectRuleBasic(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	initialData := mkInitialData(0, clock)
	expectedSet := make([]*persistencespb.RedirectRule, 0)

	rule1 := mkRedirectRule("1", "0")
	data, err := insertRedirectRule(rule1, initialData, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.NoError(t, err)
	expectedSet = append(expectedSet, mkRedirectRulePersistence(rule1, clock, nil))
	for _, r := range data.RedirectRules {
		assert.Contains(t, expectedSet, r)
	}

	rule2 := mkRedirectRule("2", "0")
	data, err = insertRedirectRule(rule2, data, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.NoError(t, err)
	expectedSet = append(expectedSet, mkRedirectRulePersistence(rule2, clock, nil))
	for _, r := range data.RedirectRules {
		assert.Contains(t, expectedSet, r)
	}

	rule3 := mkRedirectRule("3", "0")
	data, err = insertRedirectRule(rule3, data, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.NoError(t, err)
	expectedSet = append(expectedSet, mkRedirectRulePersistence(rule3, clock, nil))
	for _, r := range data.RedirectRules {
		assert.Contains(t, expectedSet, r)
	}

	protoassert.ProtoEqual(t, mkInitialData(0, clock), initialData)
}

func TestAddRedirectRuleMaxRules(t *testing.T) {
	t.Parallel()
	maxRules := 3
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)
	var err error

	// insert 3x --> success
	for i := 0; i < 3; i++ {
		src := fmt.Sprintf("%d", i)
		dst := fmt.Sprintf("%d", i+1)
		data, err = insertRedirectRule(mkRedirectRule(src, dst), data, clock, maxRules, ignoreMaxUpstreamBuildIDs)
		assert.NoError(t, err)
	}

	// insert fourth --> error
	_, err = insertRedirectRule(mkRedirectRule("10", "20"), data, clock, maxRules, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errExceedsMaxRedirectRules(4, maxRules), err)
}

func TestAddRedirectRuleInVersionSet(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	// make version set with build ID "0" in it
	initialData := mkInitialData(1, clock)

	// insert with source build id "0" --> failure
	_, err := insertRedirectRule(mkRedirectRule("0", "1"), initialData, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errSourceIsVersionSetMember, err)

	// insert with target build id "0" --> failure
	_, err = insertRedirectRule(mkRedirectRule("1", "0"), initialData, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errTargetIsVersionSetMember, err)
}

func TestAddRedirectRuleSourceIsPartiallyRampedAssignmentRuleTarget(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("1", 10), clock, nil),
		mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("2"), clock, nil),
	}

	// insert redirect rule with target 1 --> failure
	_, err := insertRedirectRule(mkRedirectRule("1", "0"), data, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errSourceIsPartiallyRampedAssignmentRuleTarget, err)
}

func TestAddRedirectRuleAlreadyExists(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	initialData := mkInitialData(0, clock)

	// insert with source build id "0"
	data, err := insertRedirectRule(mkRedirectRule("0", "1"), initialData, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.NoError(t, err)

	// insert with source build id "0" --> failure
	_, err = insertRedirectRule(mkRedirectRule("0", "6"), data, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errSourceAlreadyExists("0", "1"), err)
}

func TestAddRedirectRuleCreateCycle(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	initialData := mkInitialData(0, clock)

	// insert with source -> target == "0" -> "0" --> failure
	_, err := insertRedirectRule(mkRedirectRule("0", "0"), initialData, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errIsCyclic, err)

	// insert with source -> target == "0" -> "1" --> success
	data, err := insertRedirectRule(mkRedirectRule("0", "1"), initialData, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.NoError(t, err)

	// insert with source build id "1" -> "0" --> failure
	_, err = insertRedirectRule(mkRedirectRule("1", "0"), data, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errIsCyclic, err)
}

func TestAddRedirectRuleMaxUpstreamBuildIDs(t *testing.T) {
	t.Parallel()
	maxUpstreamBuildIDs := 2
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)

	// insert (4->5)
	// 4 ---> 5
	data, err := insertRedirectRule(mkRedirectRule("4", "5"), data, clock, ignoreMaxRules, maxUpstreamBuildIDs)
	assert.NoError(t, err)

	// insert (5->6)
	// 4 ---> 5 ---> 6
	data, err = insertRedirectRule(mkRedirectRule("5", "6"), data, clock, ignoreMaxRules, maxUpstreamBuildIDs)
	assert.NoError(t, err)

	// insert (6->7)
	// 4 ---> 5 ---> 6 ---> 7
	_, err = insertRedirectRule(mkRedirectRule("6", "7"), data, clock, ignoreMaxRules, maxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errExceedsMaxUpstreamBuildIDs(3, maxUpstreamBuildIDs), err)
}

func TestAddRedirectRuleUnversionedTarget(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)

	// insert (1->"") errors
	_, err := insertRedirectRule(mkRedirectRule("1", ""), data, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.ErrorIs(t, err, errUnversionedRedirectRuleTarget)
}

func TestReplaceRedirectRuleBasic(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	timesource := commonclock.NewRealTimeSource()
	data := mkInitialData(0, clock)
	data.RedirectRules = []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("1", "0"), clock, nil),
		mkRedirectRulePersistence(mkRedirectRule("2", "0"), clock, nil),
		mkRedirectRulePersistence(mkRedirectRule("3", "0"), clock, nil),
	}
	var err error

	replaceTest := func(source, target string) {
		prevRule := getActiveRedirectRuleBySrc(source, data)
		rule := mkRedirectRule(source, target)
		data, err = replaceRedirectRule(rule, data, clock, ignoreMaxUpstreamBuildIDs)
		assert.NoError(t, err)
		newActive := getActiveRedirectRuleBySrc(source, data)
		protoassert.ProtoEqual(t, newActive.GetRule(), rule)
		deleted := getDeletedRedirectRuleBySrc(source, data)
		assert.Equal(t, deleted[0].GetRule().GetSourceBuildId(), prevRule.GetRule().GetSourceBuildId())
		assert.Equal(t, deleted[0].GetRule().GetTargetBuildId(), prevRule.GetRule().GetTargetBuildId())
		for _, dr := range deleted {
			assert.GreaterOrEqual(t, newActive.GetCreateTimestamp().GetWallClock(), dr.GetDeleteTimestamp().GetWallClock())
		}
	}

	clock = hlc.Next(clock, timesource)
	replaceTest("1", "100")
	replaceTest("2", "100")
	replaceTest("3", "100")
}

// Test requirement that target id and source id are not in a version set (success and failure)
func TestReplaceRedirectRuleInVersionSet(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	// make a version set with build ID 0
	data := mkInitialData(1, clock)
	data.RedirectRules = []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("1", "2"), clock, nil),
	}
	var err error

	// replace with target 0 --> failure
	_, err = replaceRedirectRule(mkRedirectRule("1", "0"), data, clock, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errTargetIsVersionSetMember, err)
}

func TestReplaceRedirectRuleCreateCycle(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)
	data.RedirectRules = []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("0", "1"), clock, nil),
		mkRedirectRulePersistence(mkRedirectRule("1", "2"), clock, nil),
		mkRedirectRulePersistence(mkRedirectRule("2", "3"), clock, nil),
	}
	var err error

	_, err = replaceRedirectRule(mkRedirectRule("0", "0"), data, clock, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errIsCyclic, err)

	_, err = replaceRedirectRule(mkRedirectRule("2", "0"), data, clock, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errIsCyclic, err)

	_, err = replaceRedirectRule(mkRedirectRule("1", "0"), data, clock, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errIsCyclic, err)

	_, err = replaceRedirectRule(mkRedirectRule("2", "1"), data, clock, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errIsCyclic, err)
}

func TestReplaceRedirectRuleMaxUpstreamBuildIDs(t *testing.T) {
	t.Parallel()
	maxUpstreamBuildIDs := 2
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)

	// 2 ---> 3, 4 ---> 5 ---> 6
	data.RedirectRules = []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("2", "3"), clock, nil),
		mkRedirectRulePersistence(mkRedirectRule("4", "5"), clock, nil),
		mkRedirectRulePersistence(mkRedirectRule("5", "6"), clock, nil),
	}

	// replace(2, new_target=1)
	// 2 ---> 1, 4 ---> 5 ---> 6
	data, err := replaceRedirectRule(mkRedirectRule("2", "1"), data, clock, maxUpstreamBuildIDs)
	assert.NoError(t, err)

	// replace(2, new_target=4)
	// 2 ---> 4 ---> 5 ---> 6
	_, err = replaceRedirectRule(mkRedirectRule("2", "4"), data, clock, maxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errExceedsMaxUpstreamBuildIDs(3, maxUpstreamBuildIDs), err)
}

func TestReplaceRedirectRuleUnversionedTarget(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)

	// insert (1->2) so that we can replace
	data, err := insertRedirectRule(mkRedirectRule("1", "2"), data, clock, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.NoError(t, err)

	// replace (1->"") errors
	_, err = replaceRedirectRule(mkRedirectRule("1", ""), data, clock, ignoreMaxUpstreamBuildIDs)
	assert.ErrorIs(t, err, errUnversionedRedirectRuleTarget)
}

func TestReplaceRedirectRuleNotFound(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)
	var err error

	// fails because no rules to replace
	_, err = replaceRedirectRule(mkRedirectRule("1", "100"), data, clock, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errSourceNotFound("1"), err)

	data.RedirectRules = []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("0", "1"), clock, nil),
	}

	// fails because source doesnt exist
	_, err = replaceRedirectRule(mkRedirectRule("1", "100"), data, clock, ignoreMaxUpstreamBuildIDs)
	assert.Error(t, err)
	assert.Equal(t, errSourceNotFound("1"), err)
}

func TestDeleteRedirectRuleBasic(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	timesource := commonclock.NewRealTimeSource()
	data := mkInitialData(0, clock)
	data.RedirectRules = []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("0", "1"), clock, nil),
		mkRedirectRulePersistence(mkRedirectRule("1", "2"), clock, nil),
		mkRedirectRulePersistence(mkRedirectRule("2", "3"), clock, nil),
	}
	var err error

	clock1 := hlc.Next(clock, timesource)
	data, err = deleteRedirectRule("1", data, clock1)
	assert.NoError(t, err)
	deleted := getDeletedRedirectRuleBySrc("1", data)
	assert.Equal(t, 1, len(deleted))
	assert.Equal(t, clock1.GetWallClock(), deleted[0].GetDeleteTimestamp().GetWallClock())
	assert.Equal(t, "1", deleted[0].GetRule().GetSourceBuildId())
	assert.Equal(t, "2", deleted[0].GetRule().GetTargetBuildId())

	clock2 := hlc.Next(clock1, timesource)
	data, err = deleteRedirectRule("2", data, clock2)
	assert.NoError(t, err)
	deleted = getDeletedRedirectRuleBySrc("2", data)
	assert.Equal(t, 1, len(deleted))
	assert.Equal(t, clock2.GetWallClock(), deleted[0].GetDeleteTimestamp().GetWallClock())
	assert.Equal(t, "2", deleted[0].GetRule().GetSourceBuildId())
	assert.Equal(t, "3", deleted[0].GetRule().GetTargetBuildId())
}

func TestDeleteRedirectRuleNotFound(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	data := mkInitialData(0, clock)

	// fails because no rules to delete
	_, err := deleteRedirectRule("1", data, clock)
	assert.Error(t, err)
	assert.Equal(t, errSourceNotFound("1"), err)

	// insert a rule to replace
	data.RedirectRules = []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("0", "1"), clock, nil),
	}

	// fails because no rule with that source
	_, err = deleteRedirectRule("1", data, clock)
	assert.Error(t, err)
	assert.Equal(t, errSourceNotFound("1"), err)
}

func TestGetWorkerVersioningRules(t *testing.T) {
	t.Parallel()
	clock1 := hlc.Zero(1)
	clock2 := hlc.Next(clock1, commonclock.NewRealTimeSource())
	data := &persistencespb.VersioningData{
		AssignmentRules: []*persistencespb.AssignmentRule{
			mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("1"), clock1, nil),
			mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("10"), clock2, nil),
			mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("10"), clock1, clock2),
			mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("100"), clock2, nil),
		},
		RedirectRules: []*persistencespb.RedirectRule{
			mkRedirectRulePersistence(mkRedirectRule("1", "2"), clock1, nil),
			mkRedirectRulePersistence(mkRedirectRule("3", "4"), clock2, nil),
			mkRedirectRulePersistence(mkRedirectRule("4", "5"), clock2, nil),
			mkRedirectRulePersistence(mkRedirectRule("4", "6"), clock1, clock2),
		},
	}

	// Call list successfully
	dummyClock := hlc.Zero(99) // used to generate conflict token, but not in this test
	resp, err := GetTimestampedWorkerVersioningRules(data, dummyClock)
	assert.NoError(t, err)

	// check assignment rules
	assignmentRules := resp.GetResponse().GetAssignmentRules()
	assert.Equal(t, 3, len(assignmentRules))
	protoassert.ProtoEqual(t, &taskqueuepb.TimestampedBuildIdAssignmentRule{
		Rule:       mkAssignmentRuleWithoutRamp("1"),
		CreateTime: hlc.ProtoTimestamp(clock1),
	}, assignmentRules[0])
	protoassert.ProtoEqual(t, &taskqueuepb.TimestampedBuildIdAssignmentRule{
		Rule:       mkAssignmentRuleWithoutRamp("10"),
		CreateTime: hlc.ProtoTimestamp(clock2),
	}, assignmentRules[1])
	protoassert.ProtoEqual(t, &taskqueuepb.TimestampedBuildIdAssignmentRule{
		Rule:       mkAssignmentRuleWithoutRamp("100"),
		CreateTime: hlc.ProtoTimestamp(clock2),
	}, assignmentRules[2])

	// check redirect rules, no ordering guarantee
	redirectRules := resp.GetResponse().GetCompatibleRedirectRules()
	assert.Equal(t, 3, len(redirectRules))
	contains := func(expected *taskqueuepb.TimestampedCompatibleBuildIdRedirectRule) bool {
		for _, r := range redirectRules {
			if proto.Equal(expected, r) {
				return true
			}
		}
		return false
	}
	assert.True(t, contains(&taskqueuepb.TimestampedCompatibleBuildIdRedirectRule{
		Rule:       mkRedirectRule("1", "2"),
		CreateTime: hlc.ProtoTimestamp(clock1),
	}))
	assert.True(t, contains(&taskqueuepb.TimestampedCompatibleBuildIdRedirectRule{
		Rule:       mkRedirectRule("3", "4"),
		CreateTime: hlc.ProtoTimestamp(clock2),
	}))
	assert.True(t, contains(&taskqueuepb.TimestampedCompatibleBuildIdRedirectRule{
		Rule:       mkRedirectRule("4", "5"),
		CreateTime: hlc.ProtoTimestamp(clock2),
	}))
	assert.False(t, contains(&taskqueuepb.TimestampedCompatibleBuildIdRedirectRule{
		Rule:       mkRedirectRule("4", "6"),
		CreateTime: hlc.ProtoTimestamp(clock1),
	}))
}

func TestCleanupRedirectRuleTombstones(t *testing.T) {
	t.Parallel()
	clock := hlc.Zero(1)
	initialData := mkInitialData(0, clock)

	// start time one hour ago
	timesource := commonclock.NewEventTimeSource().Update(time.Now().Add(-1 * time.Hour))

	// insert 3x to get three rules in there
	rule1 := mkRedirectRule("1", "10")
	clock1 := hlc.Next(clock, timesource)
	data, err := insertRedirectRule(rule1, initialData, clock1, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.NoError(t, err)
	rule2 := mkRedirectRule("2", "10")
	data, err = insertRedirectRule(rule2, data, clock1, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.NoError(t, err)
	rule3 := mkRedirectRule("3", "10")
	data, err = insertRedirectRule(rule3, data, clock1, ignoreMaxRules, ignoreMaxUpstreamBuildIDs)
	assert.NoError(t, err)

	// delete "now," ~1 hour ago
	clock4 := hlc.Next(clock, timesource)
	data, err = deleteRedirectRule("1", data, clock4)
	assert.NoError(t, err)
	// delete 35 min later, ~25 min ago
	timesource.Advance(35 * time.Minute)
	clock5 := hlc.Next(clock, timesource)
	data, err = deleteRedirectRule("2", data, clock5)
	assert.NoError(t, err)
	// delete 25 min later, ~now (real time.Now())
	timesource.Advance(25 * time.Minute)
	clock6 := hlc.Next(clock, timesource)
	data, err = deleteRedirectRule("3", data, clock6)
	assert.NoError(t, err)

	// Remove data that was deleted > 30 min ago --> remove first rule
	data = CleanupRuleTombstones(data, 30*time.Minute)
	sources := make([]string, 0)
	for _, r := range data.GetRedirectRules() {
		sources = append(sources, r.GetRule().GetSourceBuildId())
	}
	assert.NotContains(t, sources, "1")
	assert.Contains(t, sources, "2")
	assert.Contains(t, sources, "3")

	// Remove data that was deleted > 5 min ago --> remove second rule
	data = CleanupRuleTombstones(data, 5*time.Minute)
	sources = make([]string, 0)
	for _, r := range data.GetRedirectRules() {
		sources = append(sources, r.GetRule().GetSourceBuildId())
	}
	assert.NotContains(t, sources, "1")
	assert.NotContains(t, sources, "2")
	assert.Contains(t, sources, "3")
}

func TestCommitBuildIDBasic(t *testing.T) {
	t.Parallel()
	timesource := commonclock.NewRealTimeSource()
	clock1 := hlc.Zero(1)
	clock2 := hlc.Next(clock1, timesource)
	data := &persistencespb.VersioningData{
		AssignmentRules: []*persistencespb.AssignmentRule{
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("1", 1), clock1, nil),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("10", 1), clock1, nil),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("100", 100), clock1, nil),
		},
	}
	expected := &persistencespb.VersioningData{
		AssignmentRules: []*persistencespb.AssignmentRule{
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("1", 1), clock1, nil),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("10", 1), clock1, clock2),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("100", 100), clock1, clock2),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("10", 100), clock2, nil),
		},
	}
	var err error

	data, err = CommitBuildID(clock2, data, mkNewCommitBuildIdReq("10", false), true, ignoreMaxRules)
	assert.NoError(t, err)
	protoassert.ProtoEqual(t, expected, data)

	// make sure multiple commits are idempotent except for timestamps
	clock3 := hlc.Next(clock2, timesource)
	expected = &persistencespb.VersioningData{
		AssignmentRules: []*persistencespb.AssignmentRule{
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("1", 1), clock1, nil),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("10", 1), clock1, clock2),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("100", 100), clock1, clock2),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("10", 100), clock2, clock3),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("10", 100), clock3, nil),
		},
	}
	data, err = CommitBuildID(clock3, data, mkNewCommitBuildIdReq("10", false), true, ignoreMaxRules)
	assert.NoError(t, err)
	protoassert.ProtoEqual(t, expected, data)
}

func TestCommitBuildIDNoRecentPoller(t *testing.T) {
	// note: correctly generating hasRecentPoller needs to be tested in the end-to-end tests
	t.Parallel()
	timesource := commonclock.NewRealTimeSource()
	clock1 := hlc.Zero(1)
	clock2 := hlc.Next(clock1, timesource)
	data := &persistencespb.VersioningData{
		AssignmentRules: []*persistencespb.AssignmentRule{
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("1", 1), clock1, nil),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("10", 1), clock1, nil),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("100", 100), clock1, nil),
		},
	}
	var err error

	// without force --> fail
	_, err = CommitBuildID(clock2, data, mkNewCommitBuildIdReq("10", false), false, ignoreMaxRules)
	assert.Error(t, err)
	assert.Equal(t, errNoRecentPollerOnCommitVersion("10"), err)

	// with force --> success
	_, err = CommitBuildID(clock2, data, mkNewCommitBuildIdReq("10", true), false, ignoreMaxRules)
	assert.NoError(t, err)
}

func TestCommitBuildIDInVersionSet(t *testing.T) {
	t.Parallel()
	timesource := commonclock.NewRealTimeSource()
	clock1 := hlc.Zero(1)
	clock2 := hlc.Next(clock1, timesource)
	data := mkInitialData(1, clock1)
	data.AssignmentRules = []*persistencespb.AssignmentRule{
		mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("0", 1), clock1, nil),
		mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("10", 1), clock1, nil),
		mkAssignmentRulePersistence(mkAssignmentRuleWithoutRamp("100"), clock1, nil),
	}
	var err error

	// with target 0 --> fail
	_, err = CommitBuildID(clock2, data, mkNewCommitBuildIdReq("0", false), true, ignoreMaxRules)
	assert.Error(t, err)
	assert.Equal(t, errTargetIsVersionSetMember, err)
}

func TestCommitBuildIDMaxAssignmentRules(t *testing.T) {
	t.Parallel()
	maxRules := 3
	timesource := commonclock.NewRealTimeSource()
	clock1 := hlc.Zero(1)
	clock2 := hlc.Next(clock1, timesource)
	data := &persistencespb.VersioningData{
		AssignmentRules: []*persistencespb.AssignmentRule{
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("1", 1), clock1, nil),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("10", 1), clock1, nil),
			mkAssignmentRulePersistence(mkAssignmentRuleWithRamp("100", 1), clock1, nil),
		},
	}
	var err error

	// commit a new target, no rules to be deleted --> fail
	_, err = CommitBuildID(clock2, data, mkNewCommitBuildIdReq("1000", false), true, maxRules)
	assert.Error(t, err)
	assert.Equal(t, errExceedsMaxAssignmentRules(4, maxRules), err)
}

/*
e.g.
Redirect Rules:
1 ------> 2
^         |
|         v
5 <------ 3 ------> 4
*/
func TestIsCyclic(t *testing.T) {
	rules := []*persistencespb.RedirectRule{
		{Rule: &taskqueuepb.CompatibleBuildIdRedirectRule{SourceBuildId: "1", TargetBuildId: "2"}},
		{Rule: &taskqueuepb.CompatibleBuildIdRedirectRule{SourceBuildId: "5", TargetBuildId: "1"}},
		{Rule: &taskqueuepb.CompatibleBuildIdRedirectRule{SourceBuildId: "3", TargetBuildId: "4"}},
		{Rule: &taskqueuepb.CompatibleBuildIdRedirectRule{SourceBuildId: "3", TargetBuildId: "5"}},
		{Rule: &taskqueuepb.CompatibleBuildIdRedirectRule{SourceBuildId: "2", TargetBuildId: "3"}},
	}
	if !isCyclic(rules) {
		t.Fail()
	}

	rules = slices.Delete(rules, 3, 4)
	if isCyclic(rules) {
		t.Fail()
	}

	rules = append(rules, &persistencespb.RedirectRule{
		Rule: &taskqueuepb.CompatibleBuildIdRedirectRule{SourceBuildId: "4", TargetBuildId: "2"},
	})
	if !isCyclic(rules) {
		t.Fail()
	}
}

func TestFindTerminalBuildId(t *testing.T) {
	t.Parallel()
	/*
		e.g.
		Redirect Rules:
		10
		^
		|
		1 <------ 2
		^
		|
		5 <------ 3 <------ 4
	*/
	createTs := hlc.Zero(1)

	redirectRules := []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("1", "10"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("2", "1"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("3", "5"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("4", "3"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("5", "1"), createTs, nil),
	}

	assert.Equal(t, "10", findTerminalBuildId("1", redirectRules))
	assert.Equal(t, "10", findTerminalBuildId("2", redirectRules))
	assert.Equal(t, "10", findTerminalBuildId("3", redirectRules))
	assert.Equal(t, "10", findTerminalBuildId("4", redirectRules))
	assert.Equal(t, "10", findTerminalBuildId("5", redirectRules))
	assert.Equal(t, "10", findTerminalBuildId("10", redirectRules))

	// empty rule set
	assert.Equal(t, "11", findTerminalBuildId("11", []*persistencespb.RedirectRule{}))

	// single rule
	redirectRules = []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("1", "2"), createTs, nil),
	}
	assert.Equal(t, "2", findTerminalBuildId("1", redirectRules))
	assert.Equal(t, "2", findTerminalBuildId("2", redirectRules))

	// cyclic rule set
	redirectRules = []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("1", "2"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("2", "1"), createTs, nil),
	}
	assert.Equal(t, "", findTerminalBuildId("1", redirectRules))
	assert.Equal(t, "", findTerminalBuildId("2", redirectRules))
}

func TestGetUpstreamBuildIds_NoCycle(t *testing.T) {
	t.Parallel()
	/*
		e.g.
		Redirect Rules:
		10
		^
		|
		1 <------ 2
		^
		|
		5 <------ 3 <------ 4
	*/
	createTs := hlc.Zero(1)

	redirectRules := []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("1", "10"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("2", "1"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("3", "5"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("4", "3"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("5", "1"), createTs, nil),
	}

	expectedUpstreamBuildIds := []string{"2", "5", "3", "4"}
	upstreamBuildIds := getUpstreamBuildIds("1", redirectRules)
	slices.Sort(expectedUpstreamBuildIds)
	slices.Sort(upstreamBuildIds)
	assert.Equal(t, expectedUpstreamBuildIds, upstreamBuildIds)
}

func TestGetUpstreamBuildIds_WithCycle(t *testing.T) {
	t.Parallel()
	/*
		e.g.
		Redirect Rules:
		1 ------> 2
		^         |
		|         v
		5 <------ 3 ------> 4
	*/
	createTs := hlc.Zero(1)
	redirectRules := []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("1", "2"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("2", "3"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("3", "4"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("3", "5"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("5", "1"), createTs, nil),
	}
	expectedUpstreamBuildIds := []string{"5", "3", "2"}
	upstreamBuildIds := getUpstreamBuildIds("1", redirectRules)
	slices.Sort(expectedUpstreamBuildIds)
	slices.Sort(upstreamBuildIds)
	assert.Equal(t, expectedUpstreamBuildIds, upstreamBuildIds)

	/*
		e.g.
		Redirect Rules:
		1         2 <---
		^         |     \
		|         v      \
		5 <------ 3 ------> 4
	*/
	redirectRules = []*persistencespb.RedirectRule{
		mkRedirectRulePersistence(mkRedirectRule("2", "3"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("3", "4"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("3", "5"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("4", "2"), createTs, nil),
		mkRedirectRulePersistence(mkRedirectRule("5", "1"), createTs, nil),
	}
	expectedUpstreamBuildIds = []string{"5", "3", "2", "4"}
	upstreamBuildIds = getUpstreamBuildIds("1", redirectRules)
	slices.Sort(expectedUpstreamBuildIds)
	slices.Sort(upstreamBuildIds)
	assert.Equal(t, expectedUpstreamBuildIds, upstreamBuildIds)
}

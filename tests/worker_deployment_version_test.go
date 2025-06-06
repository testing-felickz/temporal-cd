package tests

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgryski/go-farm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	commonpb "go.temporal.io/api/common/v1"
	deploymentpb "go.temporal.io/api/deployment/v1"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	sdkclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.temporal.io/server/common/dynamicconfig"
	"go.temporal.io/server/common/testing/testvars"
	"go.temporal.io/server/common/tqid"
	"go.temporal.io/server/common/worker_versioning"
	"go.temporal.io/server/service/worker/workerdeployment"
	"go.temporal.io/server/tests/testcore"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maxConcurrentBatchOperations             = 3
	testVersionDrainageRefreshInterval       = 3 * time.Second
	testVersionDrainageVisibilityGracePeriod = 3 * time.Second
	testMaxVersionsInDeployment              = 5
)

type (
	DeploymentVersionSuite struct {
		testcore.FunctionalTestBase
		sdkClient sdkclient.Client
		useV32    bool
	}
)

var (
	testRandomMetadataValue = []byte("random metadata value")
)

func NewDeploymentVersionSuite(useV32 bool) *DeploymentVersionSuite {
	return &DeploymentVersionSuite{useV32: useV32}
}

func TestDeploymentVersionSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, NewDeploymentVersionSuite(true))
	suite.Run(t, NewDeploymentVersionSuite(false))
}

func (s *DeploymentVersionSuite) SetupSuite() {
	s.FunctionalTestBase.SetupSuiteWithCluster(testcore.WithDynamicConfigOverrides(map[dynamicconfig.Key]any{
		dynamicconfig.EnableDeploymentVersions.Key():                   true,
		dynamicconfig.FrontendEnableWorkerVersioningDataAPIs.Key():     true, // [wv-cleanup-pre-release]
		dynamicconfig.FrontendEnableWorkerVersioningWorkflowAPIs.Key(): true, // [wv-cleanup-pre-release]
		dynamicconfig.FrontendEnableWorkerVersioningRuleAPIs.Key():     true, // [wv-cleanup-pre-release]
		dynamicconfig.FrontendEnableExecuteMultiOperation.Key():        true,

		// Make sure we don't hit the rate limiter in tests
		dynamicconfig.FrontendGlobalNamespaceNamespaceReplicationInducingAPIsRPS.Key():                1000,
		dynamicconfig.FrontendMaxNamespaceNamespaceReplicationInducingAPIsBurstRatioPerInstance.Key(): 1,
		dynamicconfig.FrontendNamespaceReplicationInducingAPIsRPS.Key():                               1000,

		// Reduce the chance of hitting max batch job limit in tests
		dynamicconfig.FrontendMaxConcurrentBatchOperationPerNamespace.Key(): maxConcurrentBatchOperations,

		dynamicconfig.VersionDrainageStatusRefreshInterval.Key():       testVersionDrainageRefreshInterval,
		dynamicconfig.VersionDrainageStatusVisibilityGracePeriod.Key(): testVersionDrainageVisibilityGracePeriod,
	}))
}

// pollFromDeployment calls PollWorkflowTaskQueue to start deployment related workflows
func (s *DeploymentVersionSuite) pollFromDeployment(ctx context.Context, tv *testvars.TestVars) {
	_, _ = s.FrontendClient().PollWorkflowTaskQueue(ctx, &workflowservice.PollWorkflowTaskQueueRequest{
		Namespace:         s.Namespace().String(),
		TaskQueue:         tv.TaskQueue(),
		Identity:          "random",
		DeploymentOptions: tv.WorkerDeploymentOptions(true),
	})
}

func (s *DeploymentVersionSuite) pollActivityFromDeployment(ctx context.Context, tv *testvars.TestVars) {
	_, _ = s.FrontendClient().PollActivityTaskQueue(ctx, &workflowservice.PollActivityTaskQueueRequest{
		Namespace:         s.Namespace().String(),
		TaskQueue:         tv.TaskQueue(),
		Identity:          "random",
		DeploymentOptions: tv.WorkerDeploymentOptions(true),
	})
}

func (s *DeploymentVersionSuite) describeVersion(tv *testvars.TestVars) (*workflowservice.DescribeWorkerDeploymentVersionResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := &workflowservice.DescribeWorkerDeploymentVersionRequest{
		Namespace: s.Namespace().String(),
	}
	if s.useV32 {
		req.DeploymentVersion = tv.ExternalDeploymentVersion()
	} else {
		req.Version = tv.DeploymentVersionString() //nolint:staticcheck // SA1019: worker versioning v0.31
	}
	return s.FrontendClient().DescribeWorkerDeploymentVersion(ctx, req)
}

func (s *DeploymentVersionSuite) updateMetadata(tv *testvars.TestVars, upsertEntries map[string]*commonpb.Payload, removeEntries []string) (*workflowservice.UpdateWorkerDeploymentVersionMetadataResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := &workflowservice.UpdateWorkerDeploymentVersionMetadataRequest{
		Namespace:     s.Namespace().String(),
		UpsertEntries: upsertEntries,
		RemoveEntries: removeEntries,
	}
	if s.useV32 {
		req.DeploymentVersion = tv.ExternalDeploymentVersion()
	} else {
		req.Version = tv.DeploymentVersionString() //nolint:staticcheck // SA1019: worker versioning v0.31
	}
	return s.FrontendClient().UpdateWorkerDeploymentVersionMetadata(ctx, req)
}

func (s *DeploymentVersionSuite) startVersionWorkflow(ctx context.Context, tv *testvars.TestVars) {
	go s.pollFromDeployment(ctx, tv)
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := assert.New(t)
		resp, err := s.describeVersion(tv)
		a.NoError(err)
		// regardless of s.useV32, we want to read both version formats
		a.Equal(tv.DeploymentVersionString(), resp.GetWorkerDeploymentVersionInfo().GetVersion())
		a.Equal(tv.ExternalDeploymentVersion().GetDeploymentName(), resp.GetWorkerDeploymentVersionInfo().GetDeploymentVersion().GetDeploymentName())
		a.Equal(tv.ExternalDeploymentVersion().GetBuildId(), resp.GetWorkerDeploymentVersionInfo().GetDeploymentVersion().GetBuildId())

		newResp, err := s.FrontendClient().DescribeWorkerDeployment(ctx, &workflowservice.DescribeWorkerDeploymentRequest{
			Namespace:      s.Namespace().String(),
			DeploymentName: tv.DeploymentSeries(),
		})
		a.NoError(err)
		var versionSummaryNames []string
		var versionSummaryVersions []*deploymentpb.WorkerDeploymentVersion
		for _, versionSummary := range newResp.GetWorkerDeploymentInfo().GetVersionSummaries() {
			versionSummaryNames = append(versionSummaryNames, versionSummary.GetVersion())
			versionSummaryVersions = append(versionSummaryVersions, versionSummary.GetDeploymentVersion())
		}
		a.Contains(versionSummaryNames, tv.DeploymentVersionString())
		contains := slices.ContainsFunc(versionSummaryVersions, func(v *deploymentpb.WorkerDeploymentVersion) bool {
			return v.GetDeploymentName() == tv.ExternalDeploymentVersion().GetDeploymentName() &&
				v.GetBuildId() == tv.ExternalDeploymentVersion().GetBuildId()
		})
		a.True(contains)
	}, time.Second*5, time.Millisecond*200)
}

func (s *DeploymentVersionSuite) startVersionWorkflowExpectFailAddVersion(ctx context.Context, tv *testvars.TestVars) {
	go s.pollFromDeployment(ctx, tv)
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		newResp, err := s.FrontendClient().DescribeWorkerDeployment(ctx, &workflowservice.DescribeWorkerDeploymentRequest{
			Namespace:      s.Namespace().String(),
			DeploymentName: tv.DeploymentSeries(),
		})
		a.NoError(err)
		var versionSummaryNames []string
		var versionSummaryVersions []*deploymentpb.WorkerDeploymentVersion
		for _, versionSummary := range newResp.GetWorkerDeploymentInfo().GetVersionSummaries() {
			versionSummaryNames = append(versionSummaryNames, versionSummary.GetVersion())
			versionSummaryVersions = append(versionSummaryVersions, versionSummary.GetDeploymentVersion())
		}
		a.NotContains(versionSummaryNames, tv.DeploymentVersionString())
		contains := slices.ContainsFunc(versionSummaryVersions, func(v *deploymentpb.WorkerDeploymentVersion) bool {
			return v.GetDeploymentName() == tv.ExternalDeploymentVersion().GetDeploymentName() &&
				v.GetBuildId() == tv.ExternalDeploymentVersion().GetBuildId()
		})
		a.False(contains)
	}, time.Second*5, time.Millisecond*200)
}

func (s *DeploymentVersionSuite) TestForceCAN_NoOpenWFS() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	tv := testvars.New(s)

	// Start a version workflow
	s.startVersionWorkflow(ctx, tv)

	// Set the version as current
	err := s.setCurrent(tv, false)
	s.NoError(err)

	// ForceCAN
	versionWorkflowID := worker_versioning.GenerateVersionWorkflowID(tv.DeploymentSeries(), tv.BuildID())
	workflowExecution := &commonpb.WorkflowExecution{
		WorkflowId: versionWorkflowID,
	}

	err = s.SendSignal(s.Namespace().String(), workflowExecution, workerdeployment.ForceCANSignalName, nil, tv.ClientIdentity())
	s.NoError(err)

	// verifying we see our registered workers in the version deployment even after a CAN
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := assert.New(t)

		resp, err := s.describeVersion(tv)
		if !a.NoError(err) {
			return
		}
		a.Equal(tv.DeploymentVersionString(), resp.GetWorkerDeploymentVersionInfo().GetVersion()) //nolint:staticcheck // SA1019: worker versioning v0.31
		a.Equal(tv.ExternalDeploymentVersion().GetDeploymentName(), resp.GetWorkerDeploymentVersionInfo().GetDeploymentVersion().GetDeploymentName())
		a.Equal(tv.ExternalDeploymentVersion().GetBuildId(), resp.GetWorkerDeploymentVersionInfo().GetDeploymentVersion().GetBuildId())

		a.Equal(1, len(resp.GetWorkerDeploymentVersionInfo().GetTaskQueueInfos()))

		// verify that the version state is intact even after a CAN
		a.Equal(tv.TaskQueue().GetName(), resp.GetWorkerDeploymentVersionInfo().GetTaskQueueInfos()[0].Name)
		a.NotNil(resp.GetWorkerDeploymentVersionInfo().GetCurrentSinceTime())
		a.NotNil(resp.GetWorkerDeploymentVersionInfo().GetRoutingChangedTime())
		a.NotNil(resp.GetWorkerDeploymentVersionInfo().GetCurrentSinceTime())
		a.Nil(resp.GetWorkerDeploymentVersionInfo().GetDrainageInfo())
	}, time.Second*10, time.Millisecond*1000)
}

func (s *DeploymentVersionSuite) TestDescribeVersion_RegisterTaskQueue() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	tv := testvars.New(s)

	numberOfDeployments := 1

	// Starting a deployment workflow
	go s.pollFromDeployment(ctx, tv)

	// Querying the Deployment
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)

		resp, err := s.describeVersion(tv)
		a.NoError(err)

		a.Equal(tv.DeploymentVersionString(), resp.GetWorkerDeploymentVersionInfo().GetVersion()) //nolint:staticcheck // SA1019: worker versioning v0.31
		a.Equal(tv.ExternalDeploymentVersion().GetDeploymentName(), resp.GetWorkerDeploymentVersionInfo().GetDeploymentVersion().GetDeploymentName())
		a.Equal(tv.ExternalDeploymentVersion().GetBuildId(), resp.GetWorkerDeploymentVersionInfo().GetDeploymentVersion().GetBuildId())

		a.Equal(numberOfDeployments, len(resp.GetWorkerDeploymentVersionInfo().GetTaskQueueInfos()))
		a.Equal(tv.TaskQueue().GetName(), resp.GetWorkerDeploymentVersionInfo().GetTaskQueueInfos()[0].Name)
	}, time.Second*5, time.Millisecond*200)
}

func (s *DeploymentVersionSuite) TestDescribeVersion_RegisterTaskQueue_ConcurrentPollers() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()
	tv := testvars.New(s)

	root, err := tqid.PartitionFromProto(tv.TaskQueue(), s.Namespace().String(), enumspb.TASK_QUEUE_TYPE_WORKFLOW)
	s.NoError(err)
	// Making concurrent polls to 4 partitions, 3 polls to each
	for p := 0; p < 4; p++ {
		tv2 := tv.WithTaskQueue(root.TaskQueue().NormalPartition(p).RpcName())
		for i := 0; i < 3; i++ {
			go s.pollFromDeployment(ctx, tv2)
			go s.pollActivityFromDeployment(ctx, tv2)
		}
	}

	// Querying the Deployment
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := assert.New(t)

		resp, err := s.describeVersion(tv)
		if !a.NoError(err) {
			return
		}
		a.Equal(tv.DeploymentVersionString(), resp.GetWorkerDeploymentVersionInfo().GetVersion()) //nolint:staticcheck // SA1019: worker versioning v0.31
		a.Equal(tv.ExternalDeploymentVersion().GetDeploymentName(), resp.GetWorkerDeploymentVersionInfo().GetDeploymentVersion().GetDeploymentName())
		a.Equal(tv.ExternalDeploymentVersion().GetBuildId(), resp.GetWorkerDeploymentVersionInfo().GetDeploymentVersion().GetBuildId())

		a.Equal(2, len(resp.GetWorkerDeploymentVersionInfo().GetTaskQueueInfos()))
		a.Equal(tv.TaskQueue().GetName(), resp.GetWorkerDeploymentVersionInfo().GetTaskQueueInfos()[0].Name)
	}, time.Second*10, time.Millisecond*1000)
}

// Name is used by testvars. We use a shorten test name in variables so that physical task queue IDs
// do not grow larger that DB column limit (currently as low as 272 chars).
func (s *DeploymentVersionSuite) Name() string {
	fullName := s.T().Name()
	if len(fullName) <= 30 {
		return fullName
	}
	short := fmt.Sprintf("%s-%08x",
		fullName[len(fullName)-21:],
		farm.Fingerprint32([]byte(fullName)),
	)
	return strings.Replace(short, ".", "|", -1)
}

//nolint:forbidigo
func (s *DeploymentVersionSuite) TestDrainageStatus_SetCurrentVersion_NoOpenWFs() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv1 := testvars.New(s).WithBuildIDNumber(1)
	tv2 := testvars.New(s).WithBuildIDNumber(2)

	// Start deployment workflow 1 and wait for the deployment version to exist
	s.startVersionWorkflow(ctx, tv1)

	// Start deployment workflow 2 and wait for the deployment version to exist
	s.startVersionWorkflow(ctx, tv2)

	// non-current deployments have never been used and have no drainage info
	s.checkVersionDrainage(ctx, tv1, &deploymentpb.VersionDrainageInfo{}, false, false)
	s.checkVersionDrainage(ctx, tv2, &deploymentpb.VersionDrainageInfo{}, false, false)

	// SetCurrent tv1
	err := s.setCurrent(tv1, true)
	s.Nil(err)

	// both still nil since neither are draining
	s.checkVersionDrainage(ctx, tv1, &deploymentpb.VersionDrainageInfo{}, false, false)
	s.checkVersionDrainage(ctx, tv2, &deploymentpb.VersionDrainageInfo{}, false, false)

	// SetCurrent tv2 --> tv1 starts the child drainage workflow
	err = s.setCurrent(tv2, true)
	s.Nil(err)

	// tv1 should now be "draining" for visibilityGracePeriod duration
	s.checkVersionDrainage(ctx, tv1, &deploymentpb.VersionDrainageInfo{
		Status:          enumspb.VERSION_DRAINAGE_STATUS_DRAINING,
		LastChangedTime: nil, // don't test this now
		LastCheckedTime: nil, // don't test this now
	}, false, false)

	// tv1 should now be "drained"
	s.checkVersionDrainage(ctx, tv1, &deploymentpb.VersionDrainageInfo{
		Status:          enumspb.VERSION_DRAINAGE_STATUS_DRAINED,
		LastChangedTime: nil, // don't test this now
		LastCheckedTime: nil, // don't test this now
	}, true, false)
}

func (s *DeploymentVersionSuite) TestDrainageStatus_SetCurrentVersion_YesOpenWFs() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv1 := testvars.New(s).WithBuildIDNumber(1)
	tv2 := testvars.New(s).WithBuildIDNumber(2)

	// Start deployment workflow 1 and wait for the deployment version to exist
	s.startVersionWorkflow(ctx, tv1)

	// Start deployment workflow 2 and wait for the deployment version to exist
	s.startVersionWorkflow(ctx, tv2)

	// non-current deployments have never been used and have no drainage info
	s.checkVersionDrainage(ctx, tv1, &deploymentpb.VersionDrainageInfo{}, false, false)
	s.checkVersionDrainage(ctx, tv2, &deploymentpb.VersionDrainageInfo{}, false, false)

	// SetCurrent tv1
	err := s.setCurrent(tv1, true)
	s.Nil(err)

	// both still nil since neither are draining
	s.checkVersionDrainage(ctx, tv1, &deploymentpb.VersionDrainageInfo{}, false, false)
	s.checkVersionDrainage(ctx, tv2, &deploymentpb.VersionDrainageInfo{}, false, false)

	// start a pinned workflow on v1
	run := s.startPinnedWorkflow(ctx, tv1)

	// SetCurrent tv2 --> tv1 starts the child drainage workflow
	err = s.setCurrent(tv2, true)
	s.Nil(err)

	// tv1 should now be "draining" for visibilityGracePeriod duration
	s.checkVersionDrainage(ctx, tv1, &deploymentpb.VersionDrainageInfo{
		Status:          enumspb.VERSION_DRAINAGE_STATUS_DRAINING,
		LastChangedTime: nil, // don't test this now
		LastCheckedTime: nil, // don't test this now
	}, false, false)

	// tv1 should still be "draining" for visibilityGracePeriod duration
	s.checkVersionDrainage(ctx, tv1, &deploymentpb.VersionDrainageInfo{
		Status:          enumspb.VERSION_DRAINAGE_STATUS_DRAINING,
		LastChangedTime: nil, // don't test this now
		LastCheckedTime: nil, // don't test this now
	}, true, false)

	// terminate workflow
	_, err = s.FrontendClient().TerminateWorkflowExecution(ctx, &workflowservice.TerminateWorkflowExecutionRequest{
		Namespace: s.Namespace().String(),
		WorkflowExecution: &commonpb.WorkflowExecution{
			WorkflowId: run.GetID(),
			RunId:      run.GetRunID(),
		},
		Reason:   "test",
		Identity: tv1.ClientIdentity(),
	})
	s.Nil(err)

	// tv1 should now be "drained"
	s.checkVersionDrainage(ctx, tv1, &deploymentpb.VersionDrainageInfo{
		Status:          enumspb.VERSION_DRAINAGE_STATUS_DRAINED,
		LastChangedTime: nil, // don't test this now
		LastCheckedTime: nil, // don't test this now
	}, false, true)
}

func (s *DeploymentVersionSuite) startPinnedWorkflow(ctx context.Context, tv *testvars.TestVars) sdkclient.WorkflowRun {
	started := make(chan struct{}, 1)
	wf := func(ctx workflow.Context) (string, error) {
		started <- struct{}{}
		workflow.GetSignalChannel(ctx, "wait").Receive(ctx, nil)
		if workflow.GetInfo(ctx).Attempt == 1 {
			return "", errors.New("try again") //nolint:err113
		}
		panic("oops")
	}
	wId := testcore.RandomizeStr("id")
	w := worker.New(s.SdkClient(), tv.TaskQueue().String(), worker.Options{
		DeploymentOptions: worker.DeploymentOptions{
			DeploymentSeriesName: tv.DeploymentSeries(), //nolint:staticcheck // SA1019: worker versioning v0.30
		},
		UseBuildIDForVersioning: true,         //nolint:staticcheck // SA1019: old worker versioning
		BuildID:                 tv.BuildID(), //nolint:staticcheck // SA1019: old worker versioning
		Identity:                wId,
	})
	w.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{VersioningBehavior: workflow.VersioningBehaviorPinned})
	s.NoError(w.Start())
	defer w.Stop()
	run, err := s.SdkClient().ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{TaskQueue: tv.TaskQueue().String()}, wf)
	s.NoError(err)
	s.WaitForChannel(ctx, started)
	return run
}

func (s *DeploymentVersionSuite) TestVersionIgnoresDrainageSignalWhenCurrentOrRamping() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv1 := testvars.New(s).WithBuildIDNumber(1)

	// Start deployment workflow 1 and wait for the deployment version to exist
	s.startVersionWorkflow(ctx, tv1)

	// Make it current
	err := s.setCurrent(tv1, false)
	s.Nil(err)

	// Signal it to be drained. Only do this in tests.
	versionWorkflowID := worker_versioning.GenerateVersionWorkflowID(tv1.DeploymentSeries(), tv1.BuildID())
	workflowExecution := &commonpb.WorkflowExecution{
		WorkflowId: versionWorkflowID,
	}
	input := &deploymentpb.VersionDrainageInfo{
		Status:          enumspb.VERSION_DRAINAGE_STATUS_DRAINED,
		LastChangedTime: timestamppb.New(time.Now()),
		LastCheckedTime: timestamppb.New(time.Now()),
	}
	marshaledData, err := input.Marshal()
	s.NoError(err)
	signalPayload := &commonpb.Payloads{
		Payloads: []*commonpb.Payload{
			{
				Metadata: map[string][]byte{
					"encoding": []byte("binary/protobuf"),
				},
				Data: marshaledData,
			},
		},
	}
	err = s.SendSignal(s.Namespace().String(), workflowExecution, workerdeployment.SyncDrainageSignalName, signalPayload, tv1.ClientIdentity())
	s.Nil(err)

	// describe version and confirm that it is not drained
	// add a 3s time requirement so that it does not succeed immediately
	sentSignal := time.Now()
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		a.Greater(time.Since(sentSignal), 2*time.Second)
		resp, err := s.describeVersion(tv1)
		a.NoError(err)
		a.NotEqual(enumspb.VERSION_DRAINAGE_STATUS_DRAINED, resp.GetWorkerDeploymentVersionInfo().GetDrainageInfo().GetStatus())
	}, time.Second*10, time.Millisecond*1000)
}

func (s *DeploymentVersionSuite) TestDeleteVersion_DeleteCurrentVersion() {
	// Override the dynamic config so that we can verify we don't get any unexpected masked errors.
	s.OverrideDynamicConfig(dynamicconfig.FrontendMaskInternalErrorDetails, true)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tv1 := testvars.New(s).WithBuildIDNumber(1)

	// Create a deployment version
	s.startVersionWorkflow(ctx, tv1)

	// Set version as current
	err := s.setCurrent(tv1, false)
	s.Nil(err)

	// Deleting this version should fail since the version is current
	s.tryDeleteVersion(ctx, tv1, workerdeployment.ErrVersionIsCurrentOrRamping, false)

	// Verifying workflow is not in a locked state after an invalid delete request such as the one above. If the workflow were in a locked
	// state, the passed context would have timed out making the following operation fail.
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		resp, err := s.FrontendClient().DescribeWorkerDeployment(ctx, &workflowservice.DescribeWorkerDeploymentRequest{
			Namespace:      s.Namespace().String(),
			DeploymentName: tv1.DeploymentSeries(),
		})
		a.NoError(err)
		a.Equal(tv1.DeploymentVersionString(), resp.GetWorkerDeploymentInfo().GetRoutingConfig().GetCurrentVersion()) //nolint:staticcheck // SA1019: worker versioning v0.31
		a.Equal(tv1.ExternalDeploymentVersion(), resp.GetWorkerDeploymentInfo().GetRoutingConfig().GetCurrentDeploymentVersion())
	}, time.Second*5, time.Millisecond*200)

}

func (s *DeploymentVersionSuite) TestDeleteVersion_DeleteRampedVersion() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tv1 := testvars.New(s).WithBuildIDNumber(1)

	// Create a deployment version
	s.startVersionWorkflow(ctx, tv1)

	// Set version as ramping
	err := s.setRamping(tv1, 0)
	s.Nil(err)

	// Deleting this version should fail since the version is ramping
	s.tryDeleteVersion(ctx, tv1, workerdeployment.ErrVersionIsCurrentOrRamping, false)

	// Verifying workflow is not in a locked state after an invalid delete request such as the one above. If the workflow were in a locked
	// state, the passed context would have timed out making the following operation fail.
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		resp, err := s.FrontendClient().DescribeWorkerDeployment(ctx, &workflowservice.DescribeWorkerDeploymentRequest{
			Namespace:      s.Namespace().String(),
			DeploymentName: tv1.DeploymentSeries(),
		})
		a.NoError(err)
		a.Equal(tv1.DeploymentVersionString(), resp.GetWorkerDeploymentInfo().GetRoutingConfig().GetRampingVersion()) //nolint:staticcheck // SA1019: worker versioning v0.31
		a.Equal(tv1.ExternalDeploymentVersion(), resp.GetWorkerDeploymentInfo().GetRoutingConfig().GetRampingDeploymentVersion())
	}, time.Second*5, time.Millisecond*200)
}

func (s *DeploymentVersionSuite) TestDeleteVersion_NoWfs() {
	s.OverrideDynamicConfig(dynamicconfig.PollerHistoryTTL, 500*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv1 := testvars.New(s).WithBuildIDNumber(1)

	// Create a deployment version
	s.startVersionWorkflow(ctx, tv1)

	//nolint:forbidigo
	time.Sleep(2 * time.Second) // todo (Shivam): remove this after the above skip is removed

	// delete should succeed
	s.tryDeleteVersion(ctx, tv1, "", false)

	// deployment version does not exist in the deployment list
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		resp, err := s.FrontendClient().DescribeWorkerDeployment(ctx, &workflowservice.DescribeWorkerDeploymentRequest{
			Namespace:      s.Namespace().String(),
			DeploymentName: tv1.DeploymentSeries(),
		})
		a.NoError(err)
		if resp != nil {
			for _, vs := range resp.GetWorkerDeploymentInfo().GetVersionSummaries() {
				a.NotEqual(tv1.DeploymentVersionString(), vs.Version) //nolint:staticcheck // SA1019: worker versioning v0.31
				a.NotEqual(tv1.ExternalDeploymentVersion().GetDeploymentName(), vs.GetDeploymentVersion().GetDeploymentName())
				a.NotEqual(tv1.ExternalDeploymentVersion().GetBuildId(), vs.GetDeploymentVersion().GetBuildId())
			}
		}
	}, time.Second*5, time.Millisecond*200)
}

func (s *DeploymentVersionSuite) TestDeleteVersion_DrainingVersion() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv1 := testvars.New(s).WithBuildIDNumber(1)

	// Start deployment workflow 1 and wait for the deployment version to exist
	s.startVersionWorkflow(ctx, tv1)

	// Make the version current
	err := s.setCurrent(tv1, false)
	s.Nil(err)

	// Start another version workflow
	tv2 := testvars.New(s).WithBuildIDNumber(2)
	s.startVersionWorkflow(ctx, tv2)

	// Setting this version to current should start the drainage workflow for version1 and make it draining
	err = s.setCurrent(tv2, true)
	s.Nil(err)

	// Version should be draining
	s.checkVersionDrainage(ctx, tv1, &deploymentpb.VersionDrainageInfo{
		Status:          enumspb.VERSION_DRAINAGE_STATUS_DRAINING,
		LastChangedTime: nil, // don't test this now
		LastCheckedTime: nil, // don't test this now
	}, false, false)

	// delete should fail
	s.tryDeleteVersion(ctx, tv1, workerdeployment.ErrVersionIsDraining, false)

}

func (s *DeploymentVersionSuite) TestDeleteVersion_Drained_But_Pollers_Exist() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv1 := testvars.New(s).WithBuildIDNumber(1)

	// Start deployment workflow 1 and wait for the deployment version to exist
	s.startVersionWorkflow(ctx, tv1)

	// Make the version current
	err := s.setCurrent(tv1, false)
	s.Nil(err)

	// Start another version workflow
	tv2 := testvars.New(s).WithBuildIDNumber(2)
	s.startVersionWorkflow(ctx, tv2)

	// Setting this version to current should start the drainage workflow for version1
	err = s.setCurrent(tv2, true)
	s.Nil(err)

	// Signal the first version to be drained. Only do this in tests.
	versionWorkflowID := worker_versioning.GenerateVersionWorkflowID(tv1.DeploymentSeries(), tv1.BuildID())
	workflowExecution := &commonpb.WorkflowExecution{
		WorkflowId: versionWorkflowID,
	}
	input := &deploymentpb.VersionDrainageInfo{
		Status:          enumspb.VERSION_DRAINAGE_STATUS_DRAINED,
		LastChangedTime: timestamppb.New(time.Now()),
		LastCheckedTime: timestamppb.New(time.Now()),
	}
	marshaledData, err := input.Marshal()
	s.NoError(err)
	signalPayload := &commonpb.Payloads{
		Payloads: []*commonpb.Payload{
			{
				Metadata: map[string][]byte{
					"encoding": []byte("binary/protobuf"),
				},
				Data: marshaledData,
			},
		},
	}

	err = s.SendSignal(s.Namespace().String(), workflowExecution, workerdeployment.SyncDrainageSignalName, signalPayload, tv1.ClientIdentity())
	s.Nil(err)

	// Version will bypass "drained" check but delete should still fail since we have active pollers.
	s.tryDeleteVersion(ctx, tv1, workerdeployment.ErrVersionHasPollers, false)
}

func (s *DeploymentVersionSuite) signalAndWaitForDrained(ctx context.Context, tv *testvars.TestVars) {
	versionWorkflowID := worker_versioning.GenerateVersionWorkflowID(tv.DeploymentSeries(), tv.BuildID())
	workflowExecution := &commonpb.WorkflowExecution{
		WorkflowId: versionWorkflowID,
	}
	input := &deploymentpb.VersionDrainageInfo{
		Status:          enumspb.VERSION_DRAINAGE_STATUS_DRAINED,
		LastChangedTime: timestamppb.New(time.Now()),
		LastCheckedTime: timestamppb.New(time.Now()),
	}
	marshaledData, err := input.Marshal()
	s.NoError(err)
	signalPayload := &commonpb.Payloads{
		Payloads: []*commonpb.Payload{
			{
				Metadata: map[string][]byte{
					"encoding": []byte("binary/protobuf"),
				},
				Data: marshaledData,
			},
		},
	}
	err = s.SendSignal(s.Namespace().String(), workflowExecution, workerdeployment.SyncDrainageSignalName, signalPayload, tv.ClientIdentity())
	s.Nil(err)

	// wait for drained
	s.EventuallyWithT(func(t *assert.CollectT) {
		resp, err := s.describeVersion(tv)
		assert.NoError(t, err)
		assert.Equal(t, enumspb.VERSION_DRAINAGE_STATUS_DRAINED, resp.GetWorkerDeploymentVersionInfo().GetDrainageInfo().GetStatus())
	}, 10*time.Second, time.Second)
}

func (s *DeploymentVersionSuite) waitForNoPollers(ctx context.Context, tv *testvars.TestVars) {
	s.EventuallyWithT(func(t *assert.CollectT) {
		resp, err := s.FrontendClient().DescribeTaskQueue(ctx, &workflowservice.DescribeTaskQueueRequest{
			Namespace:     s.Namespace().String(),
			TaskQueue:     tv.TaskQueue(),
			TaskQueueType: enumspb.TASK_QUEUE_TYPE_WORKFLOW,
		})
		require.NoError(t, err)
		require.Empty(t, resp.Pollers)
	}, 10*time.Second, time.Second)
}

func (s *DeploymentVersionSuite) TestVersionScavenger_DeleteOnAdd() {
	s.OverrideDynamicConfig(dynamicconfig.PollerHistoryTTL, 500*time.Millisecond)
	s.OverrideDynamicConfig(dynamicconfig.MatchingMaxVersionsInDeployment, testMaxVersionsInDeployment)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tvs := make([]*testvars.TestVars, testMaxVersionsInDeployment)

	// max out the versions
	for i := 0; i < testMaxVersionsInDeployment; i++ {
		tvs[i] = testvars.New(s).WithBuildIDNumber(i).WithTaskQueue(fmt.Sprintf("%d", i))
		s.startVersionWorkflow(ctx, tvs[i])
	}
	tvMax := testvars.New(s).WithBuildIDNumber(9999)

	// try to add a version and it fails
	s.startVersionWorkflowExpectFailAddVersion(ctx, tvMax)

	// signal the second and third wfs to be drained (testing that we don't delete the first version just due to create time oldest)
	s.signalAndWaitForDrained(ctx, tvs[1])
	s.signalAndWaitForDrained(ctx, tvs[2])
	// Wait for pollers going away
	s.waitForNoPollers(ctx, tvs[1])
	s.waitForNoPollers(ctx, tvs[2])

	// try to add a version again, and it succeeds, after deleting the second version but not the third (both are eligible)
	// TODO: This fails if I try to add tvMax again...
	s.startVersionWorkflow(ctx, testvars.New(s).WithBuildIDNumber(1111))

	// second deployment version does not exist in the deployment list, third version does
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		resp, err := s.FrontendClient().DescribeWorkerDeployment(ctx, &workflowservice.DescribeWorkerDeploymentRequest{
			Namespace:      s.Namespace().String(),
			DeploymentName: tvMax.DeploymentSeries(),
		})
		a.NoError(err)
		var versions []string
		for _, vs := range resp.GetWorkerDeploymentInfo().GetVersionSummaries() {
			versions = append(versions, vs.Version) //nolint:staticcheck // SA1019: worker versioning v0.31
		}
		a.NotContains(versions, tvs[1].DeploymentVersionString())
		a.Contains(versions, tvs[2].DeploymentVersionString())
	}, time.Second*5, time.Millisecond*200)
}

func (s *DeploymentVersionSuite) TestDeleteVersion_ValidDelete() {
	s.OverrideDynamicConfig(dynamicconfig.PollerHistoryTTL, 500*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv1 := testvars.New(s).WithBuildIDNumber(1)

	// Start deployment workflow 1 and wait for the deployment version to exist
	s.startVersionWorkflow(ctx, tv1)

	// Signal the first version to be drained. Only do this in tests.
	s.signalAndWaitForDrained(ctx, tv1)

	// Wait for pollers going away
	s.waitForNoPollers(ctx, tv1)

	// delete succeeds
	s.tryDeleteVersion(ctx, tv1, "", false)

	// deployment version does not exist in the deployment list
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		resp, err := s.FrontendClient().DescribeWorkerDeployment(ctx, &workflowservice.DescribeWorkerDeploymentRequest{
			Namespace:      s.Namespace().String(),
			DeploymentName: tv1.DeploymentSeries(),
		})
		a.NoError(err)
		if resp != nil {
			for _, vs := range resp.GetWorkerDeploymentInfo().GetVersionSummaries() {
				a.NotEqual(tv1.DeploymentVersionString(), vs.Version) //nolint:staticcheck // SA1019: worker versioning v0.31
				a.NotEqual(tv1.ExternalDeploymentVersion().GetDeploymentName(), vs.GetDeploymentVersion().GetDeploymentName())
				a.NotEqual(tv1.ExternalDeploymentVersion().GetBuildId(), vs.GetDeploymentVersion().GetBuildId())
			}
		}
	}, time.Second*5, time.Millisecond*200)

	// idempotency check: deleting the same version again should succeed
	s.tryDeleteVersion(ctx, tv1, "", false)
}

func (s *DeploymentVersionSuite) TestDeleteVersion_ValidDelete_SkipDrainage() {
	s.OverrideDynamicConfig(dynamicconfig.PollerHistoryTTL, 500*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv1 := testvars.New(s).WithBuildIDNumber(1)

	// Start deployment workflow 1 and wait for the deployment version to exist
	s.startVersionWorkflow(ctx, tv1)

	// Wait for pollers going away
	s.EventuallyWithT(func(t *assert.CollectT) {
		resp, err := s.FrontendClient().DescribeTaskQueue(ctx, &workflowservice.DescribeTaskQueueRequest{
			Namespace:     s.Namespace().String(),
			TaskQueue:     tv1.TaskQueue(),
			TaskQueueType: enumspb.TASK_QUEUE_TYPE_WORKFLOW,
		})
		require.NoError(t, err)
		require.Empty(t, resp.Pollers)
	}, 5*time.Second, time.Second)

	// skipDrainage=true will make delete succeed
	s.tryDeleteVersion(ctx, tv1, "", false)

	// deployment version does not exist in the deployment list
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		resp, err := s.FrontendClient().DescribeWorkerDeployment(ctx, &workflowservice.DescribeWorkerDeploymentRequest{
			Namespace:      s.Namespace().String(),
			DeploymentName: tv1.DeploymentSeries(),
		})
		a.NoError(err)
		if resp != nil {
			for _, vs := range resp.GetWorkerDeploymentInfo().GetVersionSummaries() {
				a.NotEqual(tv1.DeploymentVersionString(), vs.Version) //nolint:staticcheck // SA1019: worker versioning v0.31
				a.NotEqual(tv1.ExternalDeploymentVersion().GetDeploymentName(), vs.GetDeploymentVersion().GetDeploymentName())
				a.NotEqual(tv1.ExternalDeploymentVersion().GetBuildId(), vs.GetDeploymentVersion().GetBuildId())
			}
		}
	}, time.Second*5, time.Millisecond*200)

	// idempotency check: deleting the same version again should succeed
	s.tryDeleteVersion(ctx, tv1, "", false)

	// Describe Worker Deployment should give not found
	// describe deployment version gives not found error
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := assert.New(t)
		_, err := s.describeVersion(tv1)
		a.Error(err)
		var nfe *serviceerror.NotFound
		a.True(errors.As(err, &nfe))
	}, time.Second*5, time.Millisecond*200)
}

func (s *DeploymentVersionSuite) TestDeleteVersion_ConcurrentDeleteVersion() {
	s.OverrideDynamicConfig(dynamicconfig.PollerHistoryTTL, 500*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv1 := testvars.New(s).WithBuildIDNumber(1)

	// Start deployment workflow 1 and wait for the deployment version to exist
	s.startVersionWorkflow(ctx, tv1)

	// Wait for pollers going away
	s.EventuallyWithT(func(t *assert.CollectT) {
		resp, err := s.FrontendClient().DescribeTaskQueue(ctx, &workflowservice.DescribeTaskQueueRequest{
			Namespace:     s.Namespace().String(),
			TaskQueue:     tv1.TaskQueue(),
			TaskQueueType: enumspb.TASK_QUEUE_TYPE_WORKFLOW,
		})
		require.NoError(t, err)
		require.Empty(t, resp.Pollers)
	}, 10*time.Second, time.Second)

	// concurrent delete version requests should not break the system.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.tryDeleteVersion(ctx, tv1, "", false)
	}()
	go func() {
		defer wg.Done()
		s.tryDeleteVersion(ctx, tv1, "", false)
	}()
	wg.Wait()

	// deployment version does not exist in the deployment list
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		resp, err := s.FrontendClient().DescribeWorkerDeployment(ctx, &workflowservice.DescribeWorkerDeploymentRequest{
			Namespace:      s.Namespace().String(),
			DeploymentName: tv1.DeploymentSeries(),
		})
		a.NoError(err)
		if resp != nil {
			for _, vs := range resp.GetWorkerDeploymentInfo().GetVersionSummaries() {
				a.NotEqual(tv1.DeploymentVersionString(), vs.Version) //nolint:staticcheck // SA1019: worker versioning v0.31
				a.NotEqual(tv1.ExternalDeploymentVersion().GetDeploymentName(), vs.GetDeploymentVersion().GetDeploymentName())
				a.NotEqual(tv1.ExternalDeploymentVersion().GetBuildId(), vs.GetDeploymentVersion().GetBuildId())
			}
		}
	}, time.Second*5, time.Millisecond*200)
}

// VersionMissingTaskQueues
func (s *DeploymentVersionSuite) TestVersionMissingTaskQueues_InvalidSetCurrentVersion() {
	// Override the dynamic config to verify we don't get any unexpected masked errors.
	s.OverrideDynamicConfig(dynamicconfig.FrontendMaskInternalErrorDetails, true)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv := testvars.New(s)
	tv1 := tv.WithBuildIDNumber(1).WithTaskQueue(tv.Any().String())

	// Start deployment workflow 1 and wait for the deployment version to exist
	pollerCtx1, pollerCancel1 := context.WithCancel(ctx)
	s.startVersionWorkflow(pollerCtx1, tv1)

	// SetCurrent so that the task queue puts the version in its versions info
	err := s.setCurrent(tv1, false)
	s.Nil(err)

	// new version with a different registered task-queue
	tv2 := testvars.New(s).WithBuildIDNumber(2).WithTaskQueue(testvars.New(s.T()).Any().String())
	s.startVersionWorkflow(ctx, tv2)

	// Cancel pollers on task_queue_1 to increase the backlog of tasks
	pollerCancel1()

	// Start a workflow on task_queue_1 to increase the add rate
	s.startWorkflow(tv1, tv1.VersioningOverridePinned(s.useV32))

	// SetCurrent tv2
	err = s.setCurrent(tv2, false)

	// SetCurrent should fail since task_queue_1 does not have a current version than the deployment's existing current version
	// and it either has a backlog of tasks being present or an add rate > 0.
	s.EqualErrorf(err, workerdeployment.ErrCurrentVersionDoesNotHaveAllTaskQueues, err.Error())
}

func (s *DeploymentVersionSuite) TestVersionMissingTaskQueues_ValidSetCurrentVersion() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv := testvars.New(s)

	tv1 := tv.WithBuildIDNumber(1).WithTaskQueue(tv.Any().String())
	s.startVersionWorkflow(ctx, tv1)

	// SetCurrent so that the task queue puts the version in its versions info
	err := s.setCurrent(tv1, false)
	s.Nil(err)

	// new version with a different registered task-queue
	tv2 := tv.WithBuildIDNumber(2).WithTaskQueue(tv.Any().String())
	s.startVersionWorkflow(ctx, tv2)

	// SetCurrent tv2
	err = s.setCurrent(tv2, false)

	// SetCurrent tv2 should succeed as task_queue_1, despite missing from the new current version, has no backlogged tasks/add-rate > 0
	s.Nil(err)
}

func (s *DeploymentVersionSuite) TestVersionMissingTaskQueues_InvalidSetRampingVersion() {
	// Override the dynamic config to verify we don't get any unexpected masked errors.
	s.OverrideDynamicConfig(dynamicconfig.FrontendMaskInternalErrorDetails, true)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv := testvars.New(s)
	tv1 := tv.WithBuildIDNumber(1).WithTaskQueue(tv.Any().String())

	// Start deployment workflow 1 and wait for the deployment version to exist
	pollerCtx1, pollerCancel1 := context.WithCancel(ctx)
	s.startVersionWorkflow(pollerCtx1, tv1)

	// SetCurrent so that the task queue puts the version in its versions info
	err := s.setCurrent(tv1, false)
	s.Nil(err)

	// new version with a different registered task-queue
	tv2 := tv.WithBuildIDNumber(2).WithTaskQueue(tv.Any().String())
	s.startVersionWorkflow(ctx, tv2)

	// Cancel pollers on task_queue_1 to increase the backlog of tasks
	pollerCancel1()

	// Start a workflow on task_queue_1 to increase the add rate
	s.startWorkflow(tv1, tv1.VersioningOverridePinned(s.useV32))

	// SetRampingVersion to tv2
	err = s.setRamping(tv2, 0)

	// SetRampingVersion should fail since task_queue_1 does not have a current version than the deployment's existing current version
	// and it either has a backlog of tasks being present or an add rate > 0.
	s.EqualErrorf(err, workerdeployment.ErrRampingVersionDoesNotHaveAllTaskQueues, err.Error())
}

func (s *DeploymentVersionSuite) setRamping(
	tv *testvars.TestVars,
	percentage float32,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	v := tv.DeploymentVersionString()
	bid := tv.BuildID()
	req := &workflowservice.SetWorkerDeploymentRampingVersionRequest{
		Namespace:      s.Namespace().String(),
		DeploymentName: tv.DeploymentSeries(),
		Percentage:     percentage,
		Identity:       tv.ClientIdentity(),
	}
	if s.useV32 {
		req.BuildId = bid
	} else {
		req.Version = v //nolint:staticcheck // SA1019: worker versioning v0.31
	}
	_, err := s.FrontendClient().SetWorkerDeploymentRampingVersion(ctx, req)
	return err
}

func (s *DeploymentVersionSuite) setCurrent(tv *testvars.TestVars, ignoreMissingTQs bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := &workflowservice.SetWorkerDeploymentCurrentVersionRequest{
		Namespace:               s.Namespace().String(),
		DeploymentName:          tv.DeploymentSeries(),
		IgnoreMissingTaskQueues: ignoreMissingTQs,
		Identity:                tv.ClientIdentity(),
	}
	if s.useV32 {
		req.BuildId = tv.BuildID()
	} else {
		req.Version = tv.DeploymentVersionString() //nolint:staticcheck // SA1019: worker versioning v0.31
	}
	_, err := s.FrontendClient().SetWorkerDeploymentCurrentVersion(ctx, req)
	return err
}

func (s *DeploymentVersionSuite) TestVersionMissingTaskQueues_ValidSetRampingVersion() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv := testvars.New(s)
	tv1 := tv.WithBuildIDNumber(1).WithTaskQueue(tv.Any().String())

	// Start deployment workflow 1 and wait for the deployment version to exist
	s.startVersionWorkflow(ctx, tv1)

	// SetCurrent so that the task queue puts the version in its versions info
	err := s.setCurrent(tv1, false)
	s.Nil(err)

	// new version with a different registered task-queue
	tv2 := tv.WithBuildIDNumber(2).WithTaskQueue(tv.Any().String())
	s.startVersionWorkflow(ctx, tv2)

	// SetRampingVersion to tv2
	err = s.setRamping(tv2, 0)

	// SetRampingVersion to tv2 should succeed as task_queue_1, despite missing from the new current version, has no backlogged tasks/add-rate > 0
	s.Nil(err)
}

func (s *DeploymentVersionSuite) startWorkflow(
	tv *testvars.TestVars,
	override *workflowpb.VersioningOverride,
) string {
	request := &workflowservice.StartWorkflowExecutionRequest{
		RequestId:          tv.Any().String(),
		Namespace:          s.Namespace().String(),
		WorkflowId:         tv.WorkflowID(),
		WorkflowType:       tv.WorkflowType(),
		TaskQueue:          tv.TaskQueue(),
		Identity:           tv.WorkerIdentity(),
		VersioningOverride: override,
	}

	we, err0 := s.FrontendClient().StartWorkflowExecution(testcore.NewContext(), request)
	s.NoError(err0)
	return we.GetRunId()
}

func (s *DeploymentVersionSuite) tryDeleteVersion(
	ctx context.Context,
	tv *testvars.TestVars,
	expectedError string,
	skipDrainage bool,
) {
	req := &workflowservice.DeleteWorkerDeploymentVersionRequest{
		Namespace:    s.Namespace().String(),
		SkipDrainage: skipDrainage,
	}
	if s.useV32 {
		req.DeploymentVersion = tv.ExternalDeploymentVersion()
	} else {
		req.Version = tv.DeploymentVersionString() //nolint:staticcheck // SA1019: worker versioning v0.31
	}
	_, err := s.FrontendClient().DeleteWorkerDeploymentVersion(ctx, req)
	if expectedError == "" {
		s.Nil(err)
	} else {
		s.EqualErrorf(err, expectedError, err.Error())
	}
}

func (s *DeploymentVersionSuite) TestUpdateVersionMetadata() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tv1 := testvars.New(s).WithBuildIDNumber(1)

	// Start deployment workflow 1 and wait for the deployment version to exist
	s.startVersionWorkflow(ctx, tv1)

	metadata := map[string]*commonpb.Payload{
		"key1": {Data: testRandomMetadataValue},
		"key2": {Data: testRandomMetadataValue},
	}
	_, err := s.updateMetadata(tv1, metadata, nil)
	s.NoError(err)

	resp, err := s.describeVersion(tv1)
	s.NoError(err)

	// validating the metadata
	entries := resp.GetWorkerDeploymentVersionInfo().GetMetadata().GetEntries()
	s.Equal(2, len(entries))
	s.Equal(testRandomMetadataValue, entries["key1"].Data)
	s.Equal(testRandomMetadataValue, entries["key2"].Data)

	// Remove all the entries
	_, err = s.updateMetadata(tv1, nil, []string{"key1", "key2"})
	s.NoError(err)

	resp, err = s.describeVersion(tv1)
	s.NoError(err)
	entries = resp.GetWorkerDeploymentVersionInfo().GetMetadata().GetEntries()
	s.Equal(0, len(entries))
}

func (s *DeploymentVersionSuite) checkVersionDrainage(
	ctx context.Context,
	tv *testvars.TestVars,
	expectedDrainageInfo *deploymentpb.VersionDrainageInfo,
	addGracePeriod, addRefreshInterval bool,
) {
	waitFor := 5 * time.Second
	if addGracePeriod {
		waitFor += testVersionDrainageVisibilityGracePeriod
	}
	if addRefreshInterval {
		waitFor += testVersionDrainageRefreshInterval
	}

	s.EventuallyWithT(func(t *assert.CollectT) {
		a := assert.New(t)
		resp, err := s.describeVersion(tv)
		a.NoError(err)
		dInfo := resp.GetWorkerDeploymentVersionInfo().GetDrainageInfo()
		a.Equal(expectedDrainageInfo.Status, dInfo.GetStatus())
		if expectedDrainageInfo.LastCheckedTime != nil {
			a.Equal(expectedDrainageInfo.LastCheckedTime, dInfo.GetLastCheckedTime())
		}
		if expectedDrainageInfo.LastChangedTime != nil {
			a.Equal(expectedDrainageInfo.LastChangedTime, dInfo.GetLastChangedTime())
		}
	}, waitFor, time.Millisecond*100)
}

func (s *DeploymentVersionSuite) checkVersionIsCurrent(ctx context.Context, tv *testvars.TestVars) {
	// Querying the Deployment
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := assert.New(t)
		resp, err := s.describeVersion(tv)
		if !a.NoError(err) {
			return
		}
		a.Equal(tv.DeploymentVersionString(), resp.GetWorkerDeploymentVersionInfo().GetVersion())
		a.Equal(tv.ExternalDeploymentVersion().GetDeploymentName(), resp.GetWorkerDeploymentVersionInfo().GetDeploymentVersion().GetDeploymentName())
		a.Equal(tv.ExternalDeploymentVersion().GetBuildId(), resp.GetWorkerDeploymentVersionInfo().GetDeploymentVersion().GetBuildId())

		a.NotNil(resp.GetWorkerDeploymentVersionInfo().GetCurrentSinceTime())
	}, time.Second*10, time.Millisecond*1000)
}

func (s *DeploymentVersionSuite) checkDescribeWorkflowAfterOverride(
	ctx context.Context,
	wf *commonpb.WorkflowExecution,
	expectedOverride *workflowpb.VersioningOverride,
) {
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		resp, err := s.FrontendClient().DescribeWorkflowExecution(ctx, &workflowservice.DescribeWorkflowExecutionRequest{
			Namespace: s.Namespace().String(),
			Execution: wf,
		})
		a.NoError(err)
		a.NotNil(resp)
		a.NotNil(resp.GetWorkflowExecutionInfo())
		actualOverride := resp.GetWorkflowExecutionInfo().GetVersioningInfo().GetVersioningOverride()
		a.Equal(expectedOverride.GetBehavior(), actualOverride.GetBehavior())           //nolint:staticcheck // SA1019: worker versioning v0.31
		a.Equal(expectedOverride.GetPinnedVersion(), actualOverride.GetPinnedVersion()) //nolint:staticcheck // SA1019: worker versioning v0.31
		a.Equal(expectedOverride.GetPinned().GetBehavior(), actualOverride.GetPinned().GetBehavior())
		a.Equal(expectedOverride.GetPinned().GetVersion().GetBuildId(), actualOverride.GetPinned().GetVersion().GetBuildId())
		a.Equal(expectedOverride.GetPinned().GetVersion().GetDeploymentName(), actualOverride.GetPinned().GetVersion().GetDeploymentName())
		a.Equal(expectedOverride.GetAutoUpgrade(), actualOverride.GetAutoUpgrade())

	}, 5*time.Second, 50*time.Millisecond)
}

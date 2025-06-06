package tests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dgryski/go-farm"
	"github.com/pborman/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	batchpb "go.temporal.io/api/batch/v1"
	commonpb "go.temporal.io/api/common/v1"
	deploymentpb "go.temporal.io/api/deployment/v1"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	sdkclient "go.temporal.io/sdk/client"
	"go.temporal.io/server/common/dynamicconfig"
	"go.temporal.io/server/common/payload"
	"go.temporal.io/server/common/testing/testvars"
	"go.temporal.io/server/common/tqid"
	"go.temporal.io/server/common/worker_versioning"
	deploymentwf "go.temporal.io/server/service/worker/deployment"
	"go.temporal.io/server/tests/testcore"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

/*

tests to write:

1. TestBasics to test basic deployment workflow start and use DescribeDeployment to query the deployment
2. Tests to register worker in a deployment and using DescribeDeployment for verification
*/

const (
	maxConcurrentBatchOps = 3
)

type (
	DeploymentSuite struct {
		testcore.FunctionalTestBase
	}
)

func TestDeploymentSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(DeploymentSuite))
}

func (s *DeploymentSuite) SetupSuite() {
	dynamicConfigOverrides := map[dynamicconfig.Key]any{
		dynamicconfig.EnableDeployments.Key():                          true,
		dynamicconfig.FrontendEnableWorkerVersioningDataAPIs.Key():     true,
		dynamicconfig.FrontendEnableWorkerVersioningWorkflowAPIs.Key(): true,
		dynamicconfig.FrontendEnableWorkerVersioningRuleAPIs.Key():     true,
		dynamicconfig.FrontendEnableExecuteMultiOperation.Key():        true,

		// Reachability
		dynamicconfig.ReachabilityCacheOpenWFsTTL.Key():   testReachabilityCacheOpenWFsTTL,
		dynamicconfig.ReachabilityCacheClosedWFsTTL.Key(): testReachabilityCacheClosedWFsTTL,

		// Make sure we don't hit the rate limiter in tests
		dynamicconfig.FrontendGlobalNamespaceNamespaceReplicationInducingAPIsRPS.Key():                1000,
		dynamicconfig.FrontendMaxNamespaceNamespaceReplicationInducingAPIsBurstRatioPerInstance.Key(): 1,
		dynamicconfig.FrontendNamespaceReplicationInducingAPIsRPS.Key():                               1000,

		// Reduce the chance of hitting max batch job limit in tests
		dynamicconfig.FrontendMaxConcurrentBatchOperationPerNamespace.Key(): maxConcurrentBatchOps,
	}

	s.FunctionalTestBase.SetupSuiteWithCluster(testcore.WithDynamicConfigOverrides(dynamicConfigOverrides))
}

// pollFromDeployment calls PollWorkflowTaskQueue to start deployment related workflows
func (s *DeploymentSuite) pollFromDeployment(ctx context.Context, taskQueue *taskqueuepb.TaskQueue,
	deployment *deploymentpb.Deployment) {
	_, _ = s.FrontendClient().PollWorkflowTaskQueue(ctx, &workflowservice.PollWorkflowTaskQueueRequest{
		Namespace: s.Namespace().String(),
		TaskQueue: taskQueue,
		Identity:  "random",
		DeploymentOptions: &deploymentpb.WorkerDeploymentOptions{
			BuildId:              deployment.BuildId,
			DeploymentName:       deployment.SeriesName,
			WorkerVersioningMode: enumspb.WORKER_VERSIONING_MODE_VERSIONED,
		},
	})
}

func (s *DeploymentSuite) pollActivityFromDeployment(ctx context.Context, taskQueue *taskqueuepb.TaskQueue,
	deployment *deploymentpb.Deployment) {
	_, _ = s.FrontendClient().PollActivityTaskQueue(ctx, &workflowservice.PollActivityTaskQueueRequest{
		Namespace: s.Namespace().String(),
		TaskQueue: taskQueue,
		Identity:  "random",
		DeploymentOptions: &deploymentpb.WorkerDeploymentOptions{
			BuildId:              deployment.BuildId,
			DeploymentName:       deployment.SeriesName,
			WorkerVersioningMode: enumspb.WORKER_VERSIONING_MODE_VERSIONED,
		},
	})
}

func (s *DeploymentSuite) TestDescribeDeployment_RegisterTaskQueue() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// presence of internally used delimiters (:) or escape
	// characters shouldn't break functionality
	seriesName := testcore.RandomizeStr("my-series|:|:")
	buildID := testcore.RandomizeStr("bgt:|")

	taskQueue := &taskqueuepb.TaskQueue{Name: "deployment-test", Kind: enumspb.TASK_QUEUE_KIND_NORMAL}
	workerDeployment := &deploymentpb.Deployment{
		SeriesName: seriesName,
		BuildId:    buildID,
	}
	numberOfDeployments := 1

	// Starting a deployment workflow
	go s.pollFromDeployment(ctx, taskQueue, workerDeployment)

	// Querying the Deployment
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)

		resp, err := s.FrontendClient().DescribeDeployment(ctx, &workflowservice.DescribeDeploymentRequest{
			Namespace:  s.Namespace().String(),
			Deployment: workerDeployment,
		})
		a.NoError(err)
		a.NotNil(resp.GetDeploymentInfo().GetDeployment())

		a.Equal(seriesName, resp.GetDeploymentInfo().GetDeployment().GetSeriesName())
		a.Equal(buildID, resp.GetDeploymentInfo().GetDeployment().GetBuildId())

		a.Equal(numberOfDeployments, len(resp.GetDeploymentInfo().GetTaskQueueInfos()))
		if len(resp.GetDeploymentInfo().GetTaskQueueInfos()) < numberOfDeployments {
			return
		}
		a.Equal(taskQueue.Name, resp.GetDeploymentInfo().GetTaskQueueInfos()[0].Name)
		a.Equal(false, resp.GetDeploymentInfo().GetIsCurrent())
		// todo (Shivam) - please add a check for current time
	}, time.Second*5, time.Millisecond*200)
}

func (s *DeploymentSuite) TestDescribeDeployment_RegisterTaskQueue_ConcurrentPollers() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()
	tv := testvars.New(s)
	d := tv.Deployment()

	root, err := tqid.PartitionFromProto(tv.TaskQueue(), s.Namespace().String(), enumspb.TASK_QUEUE_TYPE_WORKFLOW)
	s.NoError(err)
	// Making concurrent polls to 4 partitions, 3 polls to each
	for p := 0; p < 4; p++ {
		tq := &taskqueuepb.TaskQueue{Name: root.TaskQueue().NormalPartition(p).RpcName(), Kind: enumspb.TASK_QUEUE_KIND_NORMAL}
		for i := 0; i < 3; i++ {
			go s.pollFromDeployment(ctx, tq, d)
			go s.pollActivityFromDeployment(ctx, tq, d)
		}
	}

	// Querying the Deployment
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)

		resp, err := s.FrontendClient().DescribeDeployment(ctx, &workflowservice.DescribeDeploymentRequest{
			Namespace:  s.Namespace().String(),
			Deployment: d,
		})
		a.NoError(err)
		a.NotNil(resp.GetDeploymentInfo().GetDeployment())

		a.True(d.Equal(resp.GetDeploymentInfo().GetDeployment()))

		a.Equal(2, len(resp.GetDeploymentInfo().GetTaskQueueInfos()))
		a.Equal(tv.TaskQueue().GetName(), resp.GetDeploymentInfo().GetTaskQueueInfos()[0].Name)
		a.Equal(false, resp.GetDeploymentInfo().GetIsCurrent())
	}, time.Second*10, time.Millisecond*1000)
}

func (s *DeploymentSuite) TestGetCurrentDeployment_NoCurrentDeployment() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	seriesName := testcore.RandomizeStr("my-series")
	buildID := testcore.RandomizeStr("bgt")
	taskQueue := &taskqueuepb.TaskQueue{Name: "deployment-test", Kind: enumspb.TASK_QUEUE_KIND_NORMAL}
	workerDeployment := &deploymentpb.Deployment{
		SeriesName: seriesName,
		BuildId:    buildID,
	}

	workflowID := deploymentwf.GenerateDeploymentSeriesWorkflowID(seriesName)
	query := fmt.Sprintf("WorkflowId = '%s' AND TemporalNamespaceDivision IS NOT NULL", workflowID)
	notFoundErr := fmt.Sprintf("workflow not found for ID: %s", workflowID)

	// GetCurrentDeployment on a non-existing series returns an error
	resp, err := s.FrontendClient().GetCurrentDeployment(ctx, &workflowservice.GetCurrentDeploymentRequest{
		Namespace:  s.Namespace().String(),
		SeriesName: seriesName,
	})
	s.Error(err)
	s.Equal(err.Error(), notFoundErr)
	s.Nil(resp)

	// Starting a deployment workflow
	go s.pollFromDeployment(ctx, taskQueue, workerDeployment)

	// Verify the existence of a deployment series
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)

		resp, err := s.FrontendClient().CountWorkflowExecutions(ctx, &workflowservice.CountWorkflowExecutionsRequest{
			Namespace: s.Namespace().String(),
			Query:     query,
		})
		a.NoError(err)
		a.Equal(int64(1), resp.GetCount())
	}, time.Second*5, time.Millisecond*200)

	// Fetch series workflow's current deployment - will be nil since we haven't set it
	resp, err = s.FrontendClient().GetCurrentDeployment(ctx, &workflowservice.GetCurrentDeploymentRequest{
		Namespace:  s.Namespace().String(),
		SeriesName: seriesName,
	})
	s.NoError(err)
	s.Nil(resp.GetCurrentDeploymentInfo())
}

// addDeploymentsAndVerifyListDeployments does the following:
// verifyDeploymentListInfo checks the equality between two DeploymentListInfo objects
func (s *DeploymentSuite) verifyDeploymentListInfo(expectedDeploymentListInfo *deploymentpb.DeploymentListInfo, receivedDeploymentListInfo *deploymentpb.DeploymentListInfo) bool {
	maxDurationBetweenTimeStamps := 1 * time.Millisecond
	if expectedDeploymentListInfo.Deployment.SeriesName != receivedDeploymentListInfo.Deployment.SeriesName {
		return false
	}
	if expectedDeploymentListInfo.Deployment.BuildId != receivedDeploymentListInfo.Deployment.BuildId {
		return false
	}
	if expectedDeploymentListInfo.IsCurrent != receivedDeploymentListInfo.IsCurrent {
		return false
	}
	if expectedDeploymentListInfo.CreateTime.AsTime().Sub(receivedDeploymentListInfo.CreateTime.AsTime()) > maxDurationBetweenTimeStamps {
		return false
	}
	return true
}

func (s *DeploymentSuite) listDeploymentsAll(ctx context.Context, request *workflowservice.ListDeploymentsRequest) ([]*deploymentpb.DeploymentListInfo, error) {
	var resp *workflowservice.ListDeploymentsResponse
	var err error
	var deployments []*deploymentpb.DeploymentListInfo
	for resp == nil || len(resp.NextPageToken) > 0 {
		resp, err = s.FrontendClient().ListDeployments(ctx, request)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, resp.GetDeployments()...)
		request.NextPageToken = resp.NextPageToken
	}
	return deployments, nil
}

// verifyDeployments does the following:
// - makes a list deployments call with/without seriesFilter
// - validates the response with expectedDeployments
func (s *DeploymentSuite) verifyDeployments(ctx context.Context, request *workflowservice.ListDeploymentsRequest,
	expectedDeployments []*deploymentpb.DeploymentListInfo) {

	// list deployment call
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		actualDeployments, err := s.listDeploymentsAll(ctx, request)
		a.NoError(err)
		a.NotNil(actualDeployments)
		// check to stop eventuallyWithT from panicking since
		// it collects all possible errors
		if len(actualDeployments) < 1 {
			return
		}

		for _, expectedDeploymentListInfo := range expectedDeployments {

			deploymentListInfoValidated := false
			for _, receivedDeploymentListInfo := range actualDeployments {

				deploymentListInfoValidated = deploymentListInfoValidated ||
					s.verifyDeploymentListInfo(expectedDeploymentListInfo, receivedDeploymentListInfo)
			}
			a.True(deploymentListInfoValidated)
		}
	}, time.Second*5, time.Millisecond*200)
}

// startDeploymentsAndValidateList does the following:
// - starts deployment workflow(s)
// - calls verifyDeployments which lists + validates Deployments
func (s *DeploymentSuite) startDeploymentsAndValidateList(deploymentInfo []*deploymentpb.DeploymentListInfo, seriesFilter string, pageSize int32) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	taskQueue := &taskqueuepb.TaskQueue{Name: "deployment-test", Kind: enumspb.TASK_QUEUE_KIND_NORMAL}

	// Start deployment workflow(s)
	for _, listInfo := range deploymentInfo {
		go s.pollFromDeployment(ctx, taskQueue, listInfo.Deployment)
	}

	var expectedDeployments []*deploymentpb.DeploymentListInfo
	request := &workflowservice.ListDeploymentsRequest{
		Namespace: s.Namespace().String(),
		PageSize:  pageSize,
	}
	if seriesFilter != "" {
		request.SeriesName = seriesFilter

		// pass only those deployments for verification which have seriesName == seriesFilter
		for _, dInfo := range deploymentInfo {
			if dInfo.Deployment.SeriesName == seriesFilter {
				expectedDeployments = append(expectedDeployments, dInfo)
			}
		}
	} else {
		// pass all deployments for verification which have been started
		expectedDeployments = deploymentInfo
	}

	s.verifyDeployments(ctx, request, expectedDeployments)
}

func (s *DeploymentSuite) buildDeploymentInfo(numberOfDeployments int) []*deploymentpb.DeploymentListInfo {
	deploymentInfo := make([]*deploymentpb.DeploymentListInfo, 0)
	for i := 0; i < numberOfDeployments; i++ {
		seriesName := testcore.RandomizeStr("my-series")
		buildID := testcore.RandomizeStr("bgt")
		indDeployment := &deploymentpb.Deployment{
			SeriesName: seriesName,
			BuildId:    buildID,
		}
		deploymentListInfo := &deploymentpb.DeploymentListInfo{
			Deployment: indDeployment,
			IsCurrent:  false,
			CreateTime: timestamppb.Now(),
		}
		deploymentInfo = append(deploymentInfo, deploymentListInfo)
	}

	return deploymentInfo
}

func (s *DeploymentSuite) TestListDeployments_VerifySingleDeployment() {
	deploymentInfo := s.buildDeploymentInfo(1)
	s.startDeploymentsAndValidateList(deploymentInfo, "", 0)
}

func (s *DeploymentSuite) TestListDeployments_MultipleDeploymentsOnePage() {
	deploymentInfo := s.buildDeploymentInfo(5)
	s.startDeploymentsAndValidateList(deploymentInfo, "", 0)
}

func (s *DeploymentSuite) TestListDeployments_MultipleDeploymentsMultiplePages() {
	deploymentInfo := s.buildDeploymentInfo(5)
	s.startDeploymentsAndValidateList(deploymentInfo, "", 2)
}

func (s *DeploymentSuite) TestListDeployments_MultipleDeployments_WithSeriesFilter() {
	deploymentInfo := s.buildDeploymentInfo(2)
	seriesFilter := deploymentInfo[0].Deployment.SeriesName
	s.startDeploymentsAndValidateList(deploymentInfo, seriesFilter, 0)
}

// TODO Shivam - refactor the above test cases TestListDeployments_WithSeriesNameFilter + TestListDeployments_WithoutSeriesNameFilter
// Refactoring should be done in a way where we are validating the exact deployment (based on how many we create) - right now,
// the tests do validate the read API logic but are not the most assertive

// TODO Shivam - Add more getCurrentDeployment tests when SetCurrentDefaultBuildID API has been defined

func (s *DeploymentSuite) TestGetDeploymentReachability_OverrideUnversioned() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// presence of internally used delimiters (:) or escape
	// characters shouldn't break functionality
	seriesName := testcore.RandomizeStr("my-series|:|:")
	buildID := testcore.RandomizeStr("bgt:|")
	taskQueue := &taskqueuepb.TaskQueue{Name: "deployment-test", Kind: enumspb.TASK_QUEUE_KIND_NORMAL}
	workerDeployment := &deploymentpb.Deployment{
		SeriesName: seriesName,
		BuildId:    buildID,
	}

	s.createDeploymentAndWaitForExist(ctx, workerDeployment, taskQueue)

	// non-current deployment is unreachable
	s.checkDeploymentReachability(ctx, workerDeployment, enumspb.DEPLOYMENT_REACHABILITY_UNREACHABLE)

	// start an unversioned workflow, set pinned deployment override --> deployment should be reachable
	unversionedTQ := "unversioned-test-tq"
	run, err := s.SdkClient().ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{TaskQueue: unversionedTQ}, "wf")
	s.NoError(err)
	unversionedWFExec := &commonpb.WorkflowExecution{
		WorkflowId: run.GetID(),
		RunId:      run.GetRunID(),
	}

	// set override on our new unversioned workflow
	updateOpts := &workflowpb.WorkflowExecutionOptions{
		VersioningOverride: &workflowpb.VersioningOverride{
			Behavior:      enumspb.VERSIONING_BEHAVIOR_PINNED,
			PinnedVersion: worker_versioning.WorkerDeploymentVersionToString(worker_versioning.DeploymentVersionFromDeployment(workerDeployment)),
		},
	}
	updateResp, err := s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: updateOpts,
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), updateOpts))

	// describe workflow and check that the versioning info has the override
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, updateOpts.GetVersioningOverride())
	// check that the deployment is now reachable, since an open workflow is using it via override
	s.checkDeploymentReachability(ctx, workerDeployment, enumspb.DEPLOYMENT_REACHABILITY_REACHABLE)

	// TODO (carly): once sdk allows starting a deployment worker, start worker, complete workflow, and check for CLOSED_ONLY
	// TODO (carly): once SetCurrentDeployment is ready, check that a current deployment is reachable even with no workflows
	// TODO (carly): test starting a workflow execution on a current deployment, then getting reachability with no override
	// TODO (carly): check cache times (do I need to do this in functional when I have cache time tests in unit?)
}

func (s *DeploymentSuite) TestGetDeploymentReachability_NotFound() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// presence of internally used delimiters (:) or escape
	// characters shouldn't break functionality
	seriesName := testcore.RandomizeStr("my-series|:|:")
	buildID := testcore.RandomizeStr("bgt:|")
	resp, err := s.FrontendClient().GetDeploymentReachability(ctx, &workflowservice.GetDeploymentReachabilityRequest{
		Namespace: s.Namespace().String(),
		Deployment: &deploymentpb.Deployment{
			SeriesName: seriesName,
			BuildId:    buildID,
		},
	})
	var notFound *serviceerror.NotFound
	s.NotNil(err)
	s.True(errors.As(err, &notFound))
	s.Nil(resp)
}

func (s *DeploymentSuite) checkDescribeWorkflowAfterOverride(
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
		a.Equal(expectedOverride.GetBehavior(), resp.GetWorkflowExecutionInfo().GetVersioningInfo().GetVersioningOverride().GetBehavior())
		a.Equal(expectedOverride.GetPinnedVersion(), resp.GetWorkflowExecutionInfo().GetVersioningInfo().GetVersioningOverride().GetPinnedVersion())
	}, 5*time.Second, 50*time.Millisecond)
}

func (s *DeploymentSuite) checkDeploymentReachability(
	ctx context.Context,
	deploy *deploymentpb.Deployment,
	expectedReachability enumspb.DeploymentReachability,
) {
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		resp, err := s.FrontendClient().GetDeploymentReachability(ctx, &workflowservice.GetDeploymentReachabilityRequest{
			Namespace:  s.Namespace().String(),
			Deployment: deploy,
		})
		a.NoError(err)
		a.Equal(expectedReachability, resp.GetReachability())
	}, 5*time.Second, 50*time.Millisecond)
}

func (s *DeploymentSuite) createDeploymentAndWaitForExist(
	ctx context.Context,
	deployment *deploymentpb.Deployment,
	tq *taskqueuepb.TaskQueue,
) {
	// Start a deployment workflow
	go s.pollFromDeployment(ctx, tq, deployment)

	// Wait for the deployment to exist
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)

		resp, err := s.FrontendClient().DescribeDeployment(ctx, &workflowservice.DescribeDeploymentRequest{
			Namespace:  s.Namespace().String(),
			Deployment: deployment,
		})
		a.NoError(err)
		a.NotNil(resp.GetDeploymentInfo().GetDeployment())
	}, time.Second*5, time.Millisecond*100)
}

func (s *DeploymentSuite) TestUpdateWorkflowExecutionOptions_SetUnpinnedThenUnset() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	tv := testvars.New(s)
	// start an unversioned workflow
	run, err := s.SdkClient().ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{TaskQueue: tv.TaskQueue().Name}, "wf")
	s.NoError(err)
	unversionedWFExec := &commonpb.WorkflowExecution{
		WorkflowId: run.GetID(),
		RunId:      run.GetRunID(),
	}
	unpinnedOpts := &workflowpb.WorkflowExecutionOptions{
		VersioningOverride: &workflowpb.VersioningOverride{
			Behavior:   enumspb.VERSIONING_BEHAVIOR_AUTO_UPGRADE,
			Deployment: nil,
		},
	}

	// 1. Set unpinned override --> describe workflow shows the override
	updateResp, err := s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: unpinnedOpts,
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), unpinnedOpts))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, unpinnedOpts.GetVersioningOverride())

	// 2. Unset using empty update opts with mutation mask --> describe workflow shows no more override
	updateResp, err = s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: &workflowpb.WorkflowExecutionOptions{},
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), &workflowpb.WorkflowExecutionOptions{}))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, nil)
}

func (s *DeploymentSuite) TestUpdateWorkflowExecutionOptions_SetPinnedThenUnset() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// presence of internally used delimiters (:) or escape
	// characters shouldn't break functionality
	seriesName := testcore.RandomizeStr("my-series|:|:")
	buildID := testcore.RandomizeStr("bgt:|")
	workerDeployment := &deploymentpb.Deployment{
		SeriesName: seriesName,
		BuildId:    buildID,
	}

	// start an unversioned workflow
	unversionedTQ := "unversioned-test-tq"
	run, err := s.SdkClient().ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{TaskQueue: unversionedTQ}, "wf")
	s.NoError(err)
	unversionedWFExec := &commonpb.WorkflowExecution{
		WorkflowId: run.GetID(),
		RunId:      run.GetRunID(),
	}
	pinnedOpts := &workflowpb.WorkflowExecutionOptions{
		VersioningOverride: &workflowpb.VersioningOverride{
			Behavior:      enumspb.VERSIONING_BEHAVIOR_PINNED,
			PinnedVersion: worker_versioning.WorkerDeploymentVersionToString(worker_versioning.DeploymentVersionFromDeployment(workerDeployment)),
		},
	}
	noOpts := &workflowpb.WorkflowExecutionOptions{}

	// create deployment so that GetDeploymentReachability doesn't error
	s.createDeploymentAndWaitForExist(ctx, workerDeployment, &taskqueuepb.TaskQueue{Name: unversionedTQ, Kind: enumspb.TASK_QUEUE_KIND_NORMAL})

	// 1. Set pinned override on our new unversioned workflow --> describe workflow shows the override + deployment is reachable
	updateResp, err := s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: pinnedOpts,
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), pinnedOpts))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, pinnedOpts.GetVersioningOverride())
	s.checkDeploymentReachability(ctx, workerDeployment, enumspb.DEPLOYMENT_REACHABILITY_REACHABLE)

	// 2. Unset with empty update opts with mutation mask --> describe workflow shows no more override + deployment is unreachable
	updateResp, err = s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: &workflowpb.WorkflowExecutionOptions{},
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), noOpts))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, nil)
	s.checkDeploymentReachability(ctx, workerDeployment, enumspb.DEPLOYMENT_REACHABILITY_UNREACHABLE)
}

func (s *DeploymentSuite) TestUpdateWorkflowExecutionOptions_EmptyFields() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// presence of internally used delimiters (:) or escape
	// characters shouldn't break functionality
	seriesName := testcore.RandomizeStr("my-series|:|:")
	buildID := testcore.RandomizeStr("bgt:|")
	workerDeployment := &deploymentpb.Deployment{
		SeriesName: seriesName,
		BuildId:    buildID,
	}

	// start an unversioned workflow
	unversionedTQ := "unversioned-test-tq"
	run, err := s.SdkClient().ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{TaskQueue: unversionedTQ}, "wf")
	s.NoError(err)
	unversionedWFExec := &commonpb.WorkflowExecution{
		WorkflowId: run.GetID(),
		RunId:      run.GetRunID(),
	}
	pinnedOpts := &workflowpb.WorkflowExecutionOptions{
		VersioningOverride: &workflowpb.VersioningOverride{
			Behavior:   enumspb.VERSIONING_BEHAVIOR_PINNED,
			Deployment: workerDeployment,
		},
	}

	// 1. Pinned update with empty mask --> describe workflow shows no change
	updateResp, err := s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: pinnedOpts,
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), &workflowpb.WorkflowExecutionOptions{}))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, nil)
}

func (s *DeploymentSuite) TestUpdateWorkflowExecutionOptions_SetPinnedSetPinned() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	tv := testvars.New(s)
	tq := tv.TaskQueue()
	series := tv.DeploymentSeries()

	// start an unversioned workflow
	run, err := s.SdkClient().ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{TaskQueue: tq.GetName()}, "wf")
	s.NoError(err)
	unversionedWFExec := &commonpb.WorkflowExecution{
		WorkflowId: run.GetID(),
		RunId:      run.GetRunID(),
	}
	deployment1 := &deploymentpb.Deployment{
		SeriesName: series,
		BuildId:    tv.WithBuildIDNumber(1).BuildID(),
	}
	deployment2 := &deploymentpb.Deployment{
		SeriesName: series,
		BuildId:    tv.WithBuildIDNumber(2).BuildID(),
	}
	pinnedOpts1 := &workflowpb.WorkflowExecutionOptions{
		VersioningOverride: &workflowpb.VersioningOverride{
			Behavior:      enumspb.VERSIONING_BEHAVIOR_PINNED,
			PinnedVersion: worker_versioning.WorkerDeploymentVersionToString(worker_versioning.DeploymentVersionFromDeployment(deployment1)),
		},
	}
	pinnedOpts2 := &workflowpb.WorkflowExecutionOptions{
		VersioningOverride: &workflowpb.VersioningOverride{
			Behavior:      enumspb.VERSIONING_BEHAVIOR_PINNED,
			PinnedVersion: worker_versioning.WorkerDeploymentVersionToString(worker_versioning.DeploymentVersionFromDeployment(deployment2)),
		},
	}

	// create deployment so that GetDeploymentReachability doesn't error
	s.createDeploymentAndWaitForExist(ctx, deployment1, tq)
	s.createDeploymentAndWaitForExist(ctx, deployment2, tq)

	// 1. Set pinned override 1 --> describe workflow shows the override + deployment 1 is reachable
	updateResp, err := s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: pinnedOpts1,
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), pinnedOpts1))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, pinnedOpts1.GetVersioningOverride())
	s.checkDeploymentReachability(ctx, deployment1, enumspb.DEPLOYMENT_REACHABILITY_REACHABLE)
	s.checkDeploymentReachability(ctx, deployment2, enumspb.DEPLOYMENT_REACHABILITY_UNREACHABLE)

	// 3. Set pinned override 2 --> describe workflow shows the override + deployment 2 is reachable, 1 unreachable
	updateResp, err = s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: pinnedOpts2,
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), pinnedOpts2))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, pinnedOpts2.GetVersioningOverride())
	s.checkDeploymentReachability(ctx, deployment1, enumspb.DEPLOYMENT_REACHABILITY_UNREACHABLE)
	s.checkDeploymentReachability(ctx, deployment2, enumspb.DEPLOYMENT_REACHABILITY_REACHABLE)
}

func (s *DeploymentSuite) TestUpdateWorkflowExecutionOptions_SetUnpinnedSetUnpinned() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	tv := testvars.New(s)
	tq := tv.TaskQueue()

	// start an unversioned workflow
	run, err := s.SdkClient().ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{TaskQueue: tq.GetName()}, "wf")
	s.NoError(err)
	unversionedWFExec := &commonpb.WorkflowExecution{
		WorkflowId: run.GetID(),
		RunId:      run.GetRunID(),
	}
	unpinnedOpts := &workflowpb.WorkflowExecutionOptions{
		VersioningOverride: &workflowpb.VersioningOverride{
			Behavior:   enumspb.VERSIONING_BEHAVIOR_AUTO_UPGRADE,
			Deployment: nil,
		},
	}

	// 1. Set unpinned override --> describe workflow shows the override
	updateResp, err := s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: unpinnedOpts,
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), unpinnedOpts))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, unpinnedOpts.GetVersioningOverride())

	// 1. Set unpinned override --> describe workflow shows the override
	updateResp, err = s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: unpinnedOpts,
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), unpinnedOpts))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, unpinnedOpts.GetVersioningOverride())
}

func (s *DeploymentSuite) TestUpdateWorkflowExecutionOptions_SetUnpinnedSetPinned() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	tv := testvars.New(s)
	tq := tv.TaskQueue()
	series := tv.DeploymentSeries()

	// start an unversioned workflow
	unversionedTQ := "unversioned-test-tq"
	run, err := s.SdkClient().ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{TaskQueue: tq.GetName()}, "wf")
	s.NoError(err)
	unversionedWFExec := &commonpb.WorkflowExecution{
		WorkflowId: run.GetID(),
		RunId:      run.GetRunID(),
	}
	unpinnedOpts := &workflowpb.WorkflowExecutionOptions{
		VersioningOverride: &workflowpb.VersioningOverride{
			Behavior: enumspb.VERSIONING_BEHAVIOR_AUTO_UPGRADE,
		},
	}
	deployment1 := &deploymentpb.Deployment{
		SeriesName: series,
		BuildId:    tv.WithBuildIDNumber(1).BuildID(),
	}
	pinnedOpts1 := &workflowpb.WorkflowExecutionOptions{
		VersioningOverride: &workflowpb.VersioningOverride{
			Behavior:      enumspb.VERSIONING_BEHAVIOR_PINNED,
			PinnedVersion: worker_versioning.WorkerDeploymentVersionToString(worker_versioning.DeploymentVersionFromDeployment(deployment1)),
		},
	}

	// create deployment so that GetDeploymentReachability doesn't error
	s.createDeploymentAndWaitForExist(ctx, deployment1, &taskqueuepb.TaskQueue{Name: unversionedTQ, Kind: enumspb.TASK_QUEUE_KIND_NORMAL})

	// 1. Set unpinned override --> describe workflow shows the override + deployment1 is unreachable
	updateResp, err := s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: unpinnedOpts,
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), unpinnedOpts))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, unpinnedOpts.GetVersioningOverride())
	s.checkDeploymentReachability(ctx, deployment1, enumspb.DEPLOYMENT_REACHABILITY_UNREACHABLE)

	// 1. Set pinned override 1 --> describe workflow shows the override + deployment1 is reachable
	updateResp, err = s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: pinnedOpts1,
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), pinnedOpts1))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, pinnedOpts1.GetVersioningOverride())
	s.checkDeploymentReachability(ctx, deployment1, enumspb.DEPLOYMENT_REACHABILITY_REACHABLE)
}

func (s *DeploymentSuite) TestUpdateWorkflowExecutionOptions_SetPinnedSetUnpinned() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	tv := testvars.New(s)
	tq := tv.TaskQueue()
	series := tv.DeploymentSeries()

	// start an unversioned workflow
	run, err := s.SdkClient().ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{TaskQueue: tq.GetName()}, "wf")
	s.NoError(err)
	unversionedWFExec := &commonpb.WorkflowExecution{
		WorkflowId: run.GetID(),
		RunId:      run.GetRunID(),
	}
	unpinnedOpts := &workflowpb.WorkflowExecutionOptions{
		VersioningOverride: &workflowpb.VersioningOverride{
			Behavior: enumspb.VERSIONING_BEHAVIOR_AUTO_UPGRADE,
		},
	}
	deployment1 := &deploymentpb.Deployment{
		SeriesName: series,
		BuildId:    tv.WithBuildIDNumber(1).BuildID(),
	}
	pinnedOpts1 := &workflowpb.WorkflowExecutionOptions{
		VersioningOverride: &workflowpb.VersioningOverride{
			Behavior:      enumspb.VERSIONING_BEHAVIOR_PINNED,
			PinnedVersion: worker_versioning.WorkerDeploymentVersionToString(worker_versioning.DeploymentVersionFromDeployment(deployment1)),
		},
	}

	// create deployment so that GetDeploymentReachability doesn't error
	s.createDeploymentAndWaitForExist(ctx, deployment1, tq)

	// 1. Set pinned override 1 --> describe workflow shows the override + deployment1 is reachable
	updateResp, err := s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: pinnedOpts1,
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), pinnedOpts1))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, pinnedOpts1.GetVersioningOverride())
	s.checkDeploymentReachability(ctx, deployment1, enumspb.DEPLOYMENT_REACHABILITY_REACHABLE)

	// 1. Set unpinned override --> describe workflow shows the override + deployment1 is unreachable
	updateResp, err = s.FrontendClient().UpdateWorkflowExecutionOptions(ctx, &workflowservice.UpdateWorkflowExecutionOptionsRequest{
		Namespace:                s.Namespace().String(),
		WorkflowExecution:        unversionedWFExec,
		WorkflowExecutionOptions: unpinnedOpts,
		UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
	})
	s.NoError(err)
	s.True(proto.Equal(updateResp.GetWorkflowExecutionOptions(), unpinnedOpts))
	s.checkDescribeWorkflowAfterOverride(ctx, unversionedWFExec, unpinnedOpts.GetVersioningOverride())
	s.checkDeploymentReachability(ctx, deployment1, enumspb.DEPLOYMENT_REACHABILITY_UNREACHABLE)
}

func (s *DeploymentSuite) TestBatchUpdateWorkflowExecutionOptions_SetPinnedThenUnset() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	tv := testvars.New(s)
	tq := tv.TaskQueue()

	// presence of internally used delimiters (:) or escape
	// characters shouldn't break functionality
	seriesName := testcore.RandomizeStr("my-series|:|:")
	buildID := testcore.RandomizeStr("bgt:|")
	workerDeployment := &deploymentpb.Deployment{
		SeriesName: seriesName,
		BuildId:    buildID,
	}
	pinnedOpts := &workflowpb.WorkflowExecutionOptions{
		VersioningOverride: &workflowpb.VersioningOverride{
			Behavior:      enumspb.VERSIONING_BEHAVIOR_PINNED,
			PinnedVersion: worker_versioning.WorkerDeploymentVersionToString(worker_versioning.DeploymentVersionFromDeployment(workerDeployment)),
		},
	}

	// create deployment so that GetDeploymentReachability doesn't error
	s.createDeploymentAndWaitForExist(ctx, workerDeployment, tq)

	// start some unversioned workflows
	workflowType := "batch-test-type"
	workflows := make([]*commonpb.WorkflowExecution, 0)
	for i := 0; i < 5; i++ {
		run, err := s.SdkClient().ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{TaskQueue: tq.GetName()}, workflowType)
		s.NoError(err)
		workflows = append(workflows, &commonpb.WorkflowExecution{
			WorkflowId: run.GetID(),
			RunId:      run.GetRunID(),
		})
	}

	// start batch update-options operation
	batchJobId := uuid.New()
	err := s.startBatchJobWithinConcurrentJobLimit(ctx, &workflowservice.StartBatchOperationRequest{
		Namespace:  s.Namespace().String(),
		JobId:      batchJobId,
		Reason:     "test",
		Executions: workflows,
		Operation: &workflowservice.StartBatchOperationRequest_UpdateWorkflowOptionsOperation{
			UpdateWorkflowOptionsOperation: &batchpb.BatchOperationUpdateWorkflowExecutionOptions{
				Identity:                 uuid.New(),
				WorkflowExecutionOptions: pinnedOpts,
				UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
			},
		},
	})
	s.NoError(err)

	// wait til batch completes
	s.checkListAndWaitForBatchCompletion(ctx, batchJobId)

	// check all the workflows
	for _, wf := range workflows {
		s.checkDescribeWorkflowAfterOverride(ctx, wf, pinnedOpts.GetVersioningOverride())
	}

	// deployment should now be reachable
	s.checkDeploymentReachability(ctx, workerDeployment, enumspb.DEPLOYMENT_REACHABILITY_REACHABLE)

	// unset with empty update opts with mutation mask
	batchJobId = uuid.New()
	err = s.startBatchJobWithinConcurrentJobLimit(ctx, &workflowservice.StartBatchOperationRequest{
		Namespace:  s.Namespace().String(),
		JobId:      batchJobId,
		Reason:     "test",
		Executions: workflows,
		Operation: &workflowservice.StartBatchOperationRequest_UpdateWorkflowOptionsOperation{
			UpdateWorkflowOptionsOperation: &batchpb.BatchOperationUpdateWorkflowExecutionOptions{
				Identity:                 uuid.New(),
				WorkflowExecutionOptions: &workflowpb.WorkflowExecutionOptions{},
				UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"versioning_override"}},
			},
		},
	})
	s.NoError(err)

	// wait til batch completes
	s.checkListAndWaitForBatchCompletion(ctx, batchJobId)

	// check all the workflows
	for _, wf := range workflows {
		s.checkDescribeWorkflowAfterOverride(ctx, wf, nil)
	}

	// deployment should now be reachable
	s.checkDeploymentReachability(ctx, workerDeployment, enumspb.DEPLOYMENT_REACHABILITY_UNREACHABLE)
}

func (s *DeploymentSuite) startBatchJobWithinConcurrentJobLimit(ctx context.Context, req *workflowservice.StartBatchOperationRequest) error {
	var err error
	s.Eventually(func() bool {
		_, err = s.FrontendClient().StartBatchOperation(ctx, req)
		if err == nil {
			return true
		} else if strings.Contains(err.Error(), "Max concurrent batch operations is reached") {
			return false // retry
		}
		return true
	}, 5*time.Second, 500*time.Millisecond)
	return err
}

func (s *DeploymentSuite) checkListAndWaitForBatchCompletion(ctx context.Context, jobId string) {
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		listResp, err := s.FrontendClient().ListBatchOperations(ctx, &workflowservice.ListBatchOperationsRequest{
			Namespace: s.Namespace().String(),
		})
		a.NoError(err)
		a.Greater(len(listResp.GetOperationInfo()), 0)
		a.Equal(jobId, listResp.GetOperationInfo()[0].GetJobId())
	}, 10*time.Second, 50*time.Millisecond)

	for {
		descResp, err := s.FrontendClient().DescribeBatchOperation(ctx, &workflowservice.DescribeBatchOperationRequest{
			Namespace: s.Namespace().String(),
			JobId:     jobId,
		})
		s.NoError(err)
		if descResp.GetState() == enumspb.BATCH_OPERATION_STATE_FAILED {
			s.Fail("batch operation failed")
			return
		} else if descResp.GetState() == enumspb.BATCH_OPERATION_STATE_COMPLETED {
			return
		}
	}
}

func (s *DeploymentSuite) TestStartWorkflowExecution_WithPinnedOverride() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	deploymentA := &deploymentpb.Deployment{
		SeriesName: "seriesName",
		BuildId:    "A",
	}
	override := &workflowpb.VersioningOverride{
		Behavior:      enumspb.VERSIONING_BEHAVIOR_PINNED,
		PinnedVersion: worker_versioning.WorkerDeploymentVersionToString(worker_versioning.DeploymentVersionFromDeployment(deploymentA)),
	}

	// create deployment so that GetDeploymentReachability doesn't error
	s.createDeploymentAndWaitForExist(ctx, deploymentA, &taskqueuepb.TaskQueue{Name: "test-tq", Kind: enumspb.TASK_QUEUE_KIND_NORMAL})

	resp, err := s.FrontendClient().StartWorkflowExecution(ctx, &workflowservice.StartWorkflowExecutionRequest{
		Namespace:          s.Namespace().String(),
		WorkflowId:         "test-workflow-id1",
		WorkflowType:       &commonpb.WorkflowType{Name: "test-wf-type"},
		TaskQueue:          &taskqueuepb.TaskQueue{Name: "test-tq", Kind: enumspb.TASK_QUEUE_KIND_NORMAL},
		Identity:           "test-id1",
		RequestId:          uuid.New(),
		VersioningOverride: override,
	})

	s.NoError(err)
	s.True(resp.GetStarted())
	wf := &commonpb.WorkflowExecution{
		WorkflowId: "test-workflow-id1",
		RunId:      resp.GetRunId(),
	}
	s.checkDescribeWorkflowAfterOverride(ctx, wf, override)
	s.checkDeploymentReachability(ctx, deploymentA, enumspb.DEPLOYMENT_REACHABILITY_REACHABLE)
}

func (s *DeploymentSuite) TestStartWorkflowExecution_WithUnpinnedOverride() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	override := &workflowpb.VersioningOverride{
		Behavior:   enumspb.VERSIONING_BEHAVIOR_AUTO_UPGRADE,
		Deployment: nil,
	}

	resp, err := s.FrontendClient().StartWorkflowExecution(ctx, &workflowservice.StartWorkflowExecutionRequest{
		Namespace:          s.Namespace().String(),
		WorkflowId:         "test-workflow-id2",
		WorkflowType:       &commonpb.WorkflowType{Name: "test-wf-type"},
		TaskQueue:          &taskqueuepb.TaskQueue{Name: "test-tq", Kind: enumspb.TASK_QUEUE_KIND_NORMAL},
		Identity:           "test-id2",
		RequestId:          uuid.New(),
		VersioningOverride: override,
	})

	s.NoError(err)
	s.True(resp.GetStarted())

	wf := &commonpb.WorkflowExecution{
		WorkflowId: "test-workflow-id2",
		RunId:      resp.GetRunId(),
	}
	s.checkDescribeWorkflowAfterOverride(ctx, wf, override)
}

func (s *DeploymentSuite) TestSignalWithStartWorkflowExecution_WithPinnedOverride() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	deploymentA := &deploymentpb.Deployment{
		SeriesName: "seriesName",
		BuildId:    "A",
	}
	override := &workflowpb.VersioningOverride{
		Behavior:      enumspb.VERSIONING_BEHAVIOR_PINNED,
		PinnedVersion: worker_versioning.WorkerDeploymentVersionToString(worker_versioning.DeploymentVersionFromDeployment(deploymentA)),
	}

	// create deployment so that GetDeploymentReachability doesn't error
	s.createDeploymentAndWaitForExist(ctx, deploymentA, &taskqueuepb.TaskQueue{Name: "test-tq", Kind: enumspb.TASK_QUEUE_KIND_NORMAL})

	resp, err := s.FrontendClient().SignalWithStartWorkflowExecution(ctx, &workflowservice.SignalWithStartWorkflowExecutionRequest{
		Namespace:          s.Namespace().String(),
		WorkflowId:         "test-workflow-id3",
		WorkflowType:       &commonpb.WorkflowType{Name: "test-wf-type"},
		TaskQueue:          &taskqueuepb.TaskQueue{Name: "test-tq", Kind: enumspb.TASK_QUEUE_KIND_NORMAL},
		Identity:           "test-id3",
		RequestId:          uuid.New(),
		SignalName:         "test-signal3",
		SignalInput:        nil,
		VersioningOverride: override,
	})

	s.NoError(err)
	s.True(resp.GetStarted())

	wf := &commonpb.WorkflowExecution{
		WorkflowId: "test-workflow-id3",
		RunId:      resp.GetRunId(),
	}
	s.checkDescribeWorkflowAfterOverride(ctx, wf, override)
	s.checkDeploymentReachability(ctx, deploymentA, enumspb.DEPLOYMENT_REACHABILITY_REACHABLE)
}

func (s *DeploymentSuite) TestSignalWithStartWorkflowExecution_WithUnpinnedOverride() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	override := &workflowpb.VersioningOverride{
		Behavior:   enumspb.VERSIONING_BEHAVIOR_AUTO_UPGRADE,
		Deployment: nil,
	}

	resp, err := s.FrontendClient().SignalWithStartWorkflowExecution(ctx, &workflowservice.SignalWithStartWorkflowExecutionRequest{
		Namespace:          s.Namespace().String(),
		WorkflowId:         "test-workflow-id4",
		WorkflowType:       &commonpb.WorkflowType{Name: "test-wf-type"},
		TaskQueue:          &taskqueuepb.TaskQueue{Name: "test-tq", Kind: enumspb.TASK_QUEUE_KIND_NORMAL},
		Identity:           "test-id4",
		RequestId:          uuid.New(),
		SignalName:         "test-signal4",
		SignalInput:        nil,
		VersioningOverride: override,
	})

	s.NoError(err)
	s.True(resp.GetStarted())

	wf := &commonpb.WorkflowExecution{
		WorkflowId: "test-workflow-id4",
		RunId:      resp.GetRunId(),
	}
	s.checkDescribeWorkflowAfterOverride(ctx, wf, override)
}

func (s *DeploymentSuite) TestSetCurrent_BeforeAndAfterRegister() {
	tv := testvars.New(s)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	defer cancel()

	dep1 := &deploymentpb.Deployment{
		SeriesName: tv.DeploymentSeries(),
		BuildId:    tv.WithBuildIDNumber(1).BuildID(),
	}
	dep2 := &deploymentpb.Deployment{
		SeriesName: tv.DeploymentSeries(),
		BuildId:    tv.WithBuildIDNumber(2).BuildID(),
	}

	// set to 1
	res, err := s.FrontendClient().SetCurrentDeployment(ctx, &workflowservice.SetCurrentDeploymentRequest{
		Namespace:  s.Namespace().String(),
		Deployment: dep1,
		Identity:   "test",
	})
	s.NoError(err)
	s.Nil(res.PreviousDeploymentInfo)
	s.NotNil(res.CurrentDeploymentInfo)
	s.Equal(dep1.BuildId, res.CurrentDeploymentInfo.Deployment.BuildId)

	// describe 1 should say it's current (no delay)
	desc, err := s.FrontendClient().DescribeDeployment(ctx, &workflowservice.DescribeDeploymentRequest{
		Namespace:  s.Namespace().String(),
		Deployment: dep1,
	})
	s.NoError(err)
	s.True(desc.DeploymentInfo.IsCurrent)

	// get current should return 1 (no delay)
	cur, err := s.FrontendClient().GetCurrentDeployment(ctx, &workflowservice.GetCurrentDeploymentRequest{
		Namespace:  s.Namespace().String(),
		SeriesName: tv.DeploymentSeries(),
	})
	s.NoError(err)
	s.Equal(dep1.BuildId, cur.CurrentDeploymentInfo.Deployment.BuildId)

	// list should say it's current (with some delay)
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		list, err := s.FrontendClient().ListDeployments(ctx, &workflowservice.ListDeploymentsRequest{
			Namespace: s.Namespace().String(),
		})
		a.NoError(err)
		found, isCurrent1 := 0, false
		for _, d := range list.GetDeployments() {
			if d.Deployment.BuildId == dep1.BuildId {
				found++
				isCurrent1 = d.IsCurrent
			}
		}
		a.Equal(1, found)
		a.True(isCurrent1)
	}, time.Second*5, time.Millisecond*200)

	// now set to 2
	res, err = s.FrontendClient().SetCurrentDeployment(ctx, &workflowservice.SetCurrentDeploymentRequest{
		Namespace:  s.Namespace().String(),
		Deployment: dep2,
		Identity:   "test",
	})
	s.NoError(err)
	s.NotNil(res.PreviousDeploymentInfo)
	s.Equal(dep1.BuildId, res.PreviousDeploymentInfo.Deployment.BuildId)
	s.NotNil(res.CurrentDeploymentInfo)
	s.Equal(dep2.BuildId, res.CurrentDeploymentInfo.Deployment.BuildId)

	// describe 1 should say it's not current (no delay)
	desc, err = s.FrontendClient().DescribeDeployment(ctx, &workflowservice.DescribeDeploymentRequest{
		Namespace:  s.Namespace().String(),
		Deployment: dep1,
	})
	s.NoError(err)
	s.False(desc.DeploymentInfo.IsCurrent)

	// describe 2 should say it's not current (no delay)
	desc, err = s.FrontendClient().DescribeDeployment(ctx, &workflowservice.DescribeDeploymentRequest{
		Namespace:  s.Namespace().String(),
		Deployment: dep2,
	})
	s.NoError(err)
	s.True(desc.DeploymentInfo.IsCurrent)

	// get current should return 2 (no delay)
	cur, err = s.FrontendClient().GetCurrentDeployment(ctx, &workflowservice.GetCurrentDeploymentRequest{
		Namespace:  s.Namespace().String(),
		SeriesName: tv.DeploymentSeries(),
	})
	s.NoError(err)
	s.Equal(dep2.BuildId, cur.CurrentDeploymentInfo.Deployment.BuildId)

	// list should say 2 is current and 1 is not current (with some delay)
	s.EventuallyWithT(func(t *assert.CollectT) {
		a := require.New(t)
		list, err := s.FrontendClient().ListDeployments(ctx, &workflowservice.ListDeploymentsRequest{
			Namespace: s.Namespace().String(),
		})
		a.NoError(err)
		found, isCurrent1, isCurrent2 := 0, false, false
		for _, d := range list.GetDeployments() {
			if d.Deployment.BuildId == dep1.BuildId {
				found++
				isCurrent1 = d.IsCurrent
			} else if d.Deployment.BuildId == dep2.BuildId {
				found++
				isCurrent2 = d.IsCurrent
			}
		}
		a.Equal(2, found)
		a.False(isCurrent1)
		a.True(isCurrent2)
	}, time.Second*5, time.Millisecond*200)
}

func (s *DeploymentSuite) TestSetCurrent_UpdateMetadata() {
	tv := testvars.New(s)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	dep1 := &deploymentpb.Deployment{
		SeriesName: tv.DeploymentSeries(),
		BuildId:    tv.WithBuildIDNumber(1).BuildID(),
	}
	dep2 := &deploymentpb.Deployment{
		SeriesName: tv.DeploymentSeries(),
		BuildId:    tv.WithBuildIDNumber(2).BuildID(),
	}

	// set to 1 with some metadata
	_, err := s.FrontendClient().SetCurrentDeployment(ctx, &workflowservice.SetCurrentDeploymentRequest{
		Namespace:  s.Namespace().String(),
		Deployment: dep1,
		Identity:   "test",
		UpdateMetadata: &deploymentpb.UpdateDeploymentMetadata{
			UpsertEntries: map[string]*commonpb.Payload{
				"key1": payload.EncodeString("val1"),
				"key2": payload.EncodeString("val2"),
				"key3": payload.EncodeString("val3"),
			},
		},
	})
	s.NoError(err)

	// set to 2
	_, err = s.FrontendClient().SetCurrentDeployment(ctx, &workflowservice.SetCurrentDeploymentRequest{
		Namespace:  s.Namespace().String(),
		Deployment: dep2,
		Identity:   "test",
	})
	s.NoError(err)

	// set back to 1 with different metadata
	_, err = s.FrontendClient().SetCurrentDeployment(ctx, &workflowservice.SetCurrentDeploymentRequest{
		Namespace:  s.Namespace().String(),
		Deployment: dep1,
		Identity:   "test",
		UpdateMetadata: &deploymentpb.UpdateDeploymentMetadata{
			UpsertEntries: map[string]*commonpb.Payload{
				"key1": payload.EncodeString("new1"),
				"key4": payload.EncodeString("val4"),
			},
			RemoveEntries: []string{"key2"},
		},
	})
	s.NoError(err)

	cur, err := s.FrontendClient().GetCurrentDeployment(ctx, &workflowservice.GetCurrentDeploymentRequest{
		Namespace:  s.Namespace().String(),
		SeriesName: tv.DeploymentSeries(),
	})
	s.NoError(err)
	s.Equal(dep1.BuildId, cur.CurrentDeploymentInfo.Deployment.BuildId)
	s.Equal(`"new1"`, payload.ToString(cur.CurrentDeploymentInfo.Metadata["key1"]))
	s.Nil(cur.CurrentDeploymentInfo.Metadata["key2"])
	s.Equal(`"val3"`, payload.ToString(cur.CurrentDeploymentInfo.Metadata["key3"]))
	s.Equal(`"val4"`, payload.ToString(cur.CurrentDeploymentInfo.Metadata["key4"]))
}

// Name is used by testvars. We use a shorten test name in variables so that physical task queue IDs
// do not grow larger that DB column limit (currently as low as 272 chars).
func (s *DeploymentSuite) Name() string {
	fullName := s.T().Name()
	if len(fullName) <= 30 {
		return fullName
	}
	return fmt.Sprintf("%s-%08x",
		fullName[len(fullName)-21:],
		farm.Fingerprint32([]byte(fullName)),
	)
}

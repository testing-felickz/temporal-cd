package history

import (
	"context"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/server/common/log"
	"go.temporal.io/server/common/metrics"
	"go.temporal.io/server/service/history/consts"
	historyi "go.temporal.io/server/service/history/interfaces"
	"go.temporal.io/server/service/history/queues"
	wcache "go.temporal.io/server/service/history/workflow/cache"
)

type outboundQueueActiveTaskExecutor struct {
	stateMachineEnvironment
}

var _ queues.Executor = &outboundQueueActiveTaskExecutor{}

func newOutboundQueueActiveTaskExecutor(
	shardCtx historyi.ShardContext,
	workflowCache wcache.Cache,
	logger log.Logger,
	metricsHandler metrics.Handler,
) *outboundQueueActiveTaskExecutor {
	return &outboundQueueActiveTaskExecutor{
		stateMachineEnvironment: stateMachineEnvironment{
			shardContext: shardCtx,
			cache:        workflowCache,
			logger:       logger,
			metricsHandler: metricsHandler.WithTags(
				metrics.OperationTag(metrics.OperationOutboundQueueProcessorScope),
			),
		},
	}
}

func (e *outboundQueueActiveTaskExecutor) Execute(
	ctx context.Context,
	executable queues.Executable,
) queues.ExecuteResponse {
	task := executable.GetTask()
	namespaceTag, replicationState := getNamespaceTagAndReplicationStateByID(
		e.shardContext.GetNamespaceRegistry(),
		task.GetNamespaceID(),
	)
	taskType := queues.GetOutboundTaskTypeTagValue(task, true)
	respond := func(err error) queues.ExecuteResponse {
		metricsTags := []metrics.Tag{
			namespaceTag,
			metrics.TaskTypeTag(taskType),
			metrics.OperationTag(taskType),
		}
		return queues.ExecuteResponse{
			ExecutionMetricTags: metricsTags,
			ExecutedAsActive:    true,
			ExecutionErr:        err,
		}
	}

	ref, smt, err := StateMachineTask(e.shardContext.StateMachineRegistry(), task)
	if err != nil {
		return respond(err)
	}

	// We don't want to execute outbound tasks when handing over a namespace to avoid starting work that may not be
	// committed and cause duplicate requests.
	// We check namespace handover state **once** when processing is started. Outbound tasks may take up to 10
	// seconds (by default), but we avoid checking again later, before committing the result, to attempt to commit
	// results of inflight tasks and not lose the progress.
	if replicationState == enumspb.REPLICATION_STATE_HANDOVER {
		// TODO: Move this logic to queues.Executable when metrics tags don't need
		// to be returned from task executor. Also check the standby queue logic.
		return respond(consts.ErrNamespaceHandover)
	}

	if err := validateTaskByClock(e.shardContext, task); err != nil {
		return respond(err)
	}

	smRegistry := e.shardContext.StateMachineRegistry()
	err = smRegistry.ExecuteImmediateTask(ctx, e, ref, smt)
	return respond(err)
}

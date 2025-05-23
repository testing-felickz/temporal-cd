package matching

import (
	"context"
	"time"

	"go.temporal.io/api/serviceerror"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/server/api/matchingservice/v1"
	"go.temporal.io/server/common/headers"
	"go.temporal.io/server/common/log"
	"go.temporal.io/server/common/log/tag"
	"go.temporal.io/server/common/metrics"
	serviceerrors "go.temporal.io/server/common/serviceerror"
	"go.temporal.io/server/common/tqid"
	"google.golang.org/grpc"
)

var _ matchingservice.MatchingServiceClient = (*metricClient)(nil)

type metricClient struct {
	client          matchingservice.MatchingServiceClient
	metricsHandler  metrics.Handler
	logger          log.Logger
	throttledLogger log.Logger
}

// NewMetricClient creates a new instance of matchingservice.MatchingServiceClient that emits metrics
func NewMetricClient(
	client matchingservice.MatchingServiceClient,
	metricsHandler metrics.Handler,
	logger log.Logger,
	throttledLogger log.Logger,
) matchingservice.MatchingServiceClient {
	return &metricClient{
		client:          client,
		metricsHandler:  metricsHandler,
		logger:          logger,
		throttledLogger: throttledLogger,
	}
}

func (c *metricClient) AddActivityTask(
	ctx context.Context,
	request *matchingservice.AddActivityTaskRequest,
	opts ...grpc.CallOption,
) (_ *matchingservice.AddActivityTaskResponse, retError error) {

	scope, stopwatch := c.startMetricsRecording(ctx, metrics.MatchingClientAddActivityTaskScope)
	defer func() {
		c.finishMetricsRecording(scope, stopwatch, retError)
	}()

	c.emitForwardedSourceStats(
		scope,
		request.GetForwardInfo().GetSourcePartition(),
		request.TaskQueue,
	)

	return c.client.AddActivityTask(ctx, request, opts...)
}

func (c *metricClient) AddWorkflowTask(
	ctx context.Context,
	request *matchingservice.AddWorkflowTaskRequest,
	opts ...grpc.CallOption,
) (_ *matchingservice.AddWorkflowTaskResponse, retError error) {

	scope, stopwatch := c.startMetricsRecording(ctx, metrics.MatchingClientAddWorkflowTaskScope)
	defer func() {
		c.finishMetricsRecording(scope, stopwatch, retError)
	}()

	c.emitForwardedSourceStats(
		scope,
		request.GetForwardInfo().GetSourcePartition(),
		request.TaskQueue,
	)

	return c.client.AddWorkflowTask(ctx, request, opts...)
}

func (c *metricClient) PollActivityTaskQueue(
	ctx context.Context,
	request *matchingservice.PollActivityTaskQueueRequest,
	opts ...grpc.CallOption,
) (_ *matchingservice.PollActivityTaskQueueResponse, retError error) {

	scope, stopwatch := c.startMetricsRecording(ctx, metrics.MatchingClientPollActivityTaskQueueScope)
	defer func() {
		c.finishMetricsRecording(scope, stopwatch, retError)
	}()

	if request.PollRequest != nil {
		c.emitForwardedSourceStats(
			scope,
			request.GetForwardedSource(),
			request.PollRequest.TaskQueue,
		)
	}

	return c.client.PollActivityTaskQueue(ctx, request, opts...)
}

func (c *metricClient) PollWorkflowTaskQueue(
	ctx context.Context,
	request *matchingservice.PollWorkflowTaskQueueRequest,
	opts ...grpc.CallOption,
) (_ *matchingservice.PollWorkflowTaskQueueResponse, retError error) {

	scope, stopwatch := c.startMetricsRecording(ctx, metrics.MatchingClientPollWorkflowTaskQueueScope)
	defer func() {
		c.finishMetricsRecording(scope, stopwatch, retError)
	}()

	if request.PollRequest != nil {
		c.emitForwardedSourceStats(
			scope,
			request.GetForwardedSource(),
			request.PollRequest.TaskQueue,
		)
	}

	return c.client.PollWorkflowTaskQueue(ctx, request, opts...)
}

func (c *metricClient) QueryWorkflow(
	ctx context.Context,
	request *matchingservice.QueryWorkflowRequest,
	opts ...grpc.CallOption,
) (_ *matchingservice.QueryWorkflowResponse, retError error) {

	scope, stopwatch := c.startMetricsRecording(ctx, metrics.MatchingClientQueryWorkflowScope)
	defer func() {
		c.finishMetricsRecording(scope, stopwatch, retError)
	}()

	c.emitForwardedSourceStats(
		scope,
		request.GetForwardInfo().GetSourcePartition(),
		request.TaskQueue,
	)

	return c.client.QueryWorkflow(ctx, request, opts...)
}

func (c *metricClient) emitForwardedSourceStats(
	metricsHandler metrics.Handler,
	forwardedFrom string,
	taskQueue *taskqueuepb.TaskQueue,
) {
	if taskQueue == nil {
		return
	}

	switch {
	case forwardedFrom != "":
		metrics.MatchingClientForwardedCounter.With(metricsHandler).Record(1)
	default:
		// TODO: confirmed from metrics, it seems this error does happen at the moment...
		// it means some mangled name come here; need to check why
		_, err := tqid.NewTaskQueueFamily("", taskQueue.GetName())
		if err != nil {
			c.logger.Info("invalid tq name", tag.Error(err), tag.NewStringsTag("proto", []string{taskQueue.GetName()}))
			metrics.MatchingClientInvalidTaskQueueName.With(metricsHandler).Record(1)
		}
	}
}

func (c *metricClient) startMetricsRecording(
	ctx context.Context,
	operation string,
) (metrics.Handler, time.Time) {
	caller := headers.GetCallerInfo(ctx).CallerName
	handler := c.metricsHandler.WithTags(metrics.OperationTag(operation), metrics.NamespaceTag(caller), metrics.ServiceRoleTag(metrics.MatchingRoleTagValue))
	metrics.ClientRequests.With(handler).Record(1)
	return handler, time.Now().UTC()
}

func (c *metricClient) finishMetricsRecording(
	metricsHandler metrics.Handler,
	startTime time.Time,
	err error,
) {
	if err != nil {
		switch err.(type) {
		case *serviceerrors.StickyWorkerUnavailable,
			*serviceerror.Canceled,
			*serviceerror.DeadlineExceeded,
			*serviceerror.NotFound,
			*serviceerror.QueryFailed,
			*serviceerror.NamespaceNotFound,
			*serviceerror.NewerBuildExists,
			*serviceerror.WorkflowExecutionAlreadyStarted:
			// noop - not interest and too many logs
		default:
			c.throttledLogger.Info("matching client encountered error", tag.Error(err), tag.ServiceErrorType(err))
		}
		metrics.ClientFailures.With(metricsHandler).Record(1, metrics.ServiceErrorTypeTag(err))
	}
	metrics.ClientLatency.With(metricsHandler).Record(time.Since(startTime))
}

//go:generate mockgen -package $GOPACKAGE -source $GOFILE -destination history_iterator_mock.go

package archiver

import (
	"context"
	"encoding/json"
	"errors"

	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/api/serviceerror"
	archiverspb "go.temporal.io/server/api/archiver/v1"
	"go.temporal.io/server/common"
	"go.temporal.io/server/common/persistence"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	historyPageSize = 250
)

type (
	// HistoryIterator is used to get history batches
	HistoryIterator interface {
		Next(context.Context) (*archiverspb.HistoryBlob, error)
		HasNext() bool
		GetState() ([]byte, error)
	}

	historyIteratorState struct {
		NextEventID       int64
		FinishedIteration bool
	}

	historyIterator struct {
		historyIteratorState

		request               *ArchiveHistoryRequest
		executionManager      persistence.ExecutionManager
		sizeEstimator         SizeEstimator
		historyPageSize       int
		targetHistoryBlobSize int
	}
)

var (
	errIteratorDepleted = errors.New("iterator is depleted")
)

// NewHistoryIterator returns a new HistoryIterator
func NewHistoryIterator(
	request *ArchiveHistoryRequest,
	executionManager persistence.ExecutionManager,
	targetHistoryBlobSize int,
) HistoryIterator {
	return newHistoryIterator(request, executionManager, targetHistoryBlobSize)
}

// NewHistoryIteratorFromState returns a new HistoryIterator with specified state
func NewHistoryIteratorFromState(
	request *ArchiveHistoryRequest,
	executionManager persistence.ExecutionManager,
	targetHistoryBlobSize int,
	initialState []byte,
) (HistoryIterator, error) {
	it := newHistoryIterator(request, executionManager, targetHistoryBlobSize)
	if initialState == nil {
		return it, nil
	}
	if err := it.reset(initialState); err != nil {
		return nil, err
	}
	return it, nil
}

func newHistoryIterator(
	request *ArchiveHistoryRequest,
	executionManager persistence.ExecutionManager,
	targetHistoryBlobSize int,
) *historyIterator {
	return &historyIterator{
		historyIteratorState: historyIteratorState{
			NextEventID:       common.FirstEventID,
			FinishedIteration: false,
		},
		request:               request,
		executionManager:      executionManager,
		historyPageSize:       historyPageSize,
		targetHistoryBlobSize: targetHistoryBlobSize,
		sizeEstimator:         NewJSONSizeEstimator(),
	}
}

func (i *historyIterator) Next(
	ctx context.Context,
) (*archiverspb.HistoryBlob, error) {
	if !i.HasNext() {
		return nil, errIteratorDepleted
	}

	historyBatches, newIterState, err := i.readHistoryBatches(ctx, i.NextEventID)
	if err != nil {
		return nil, err
	}

	i.historyIteratorState = newIterState
	firstEvent := historyBatches[0].Events[0]
	lastBatch := historyBatches[len(historyBatches)-1]
	lastEvent := lastBatch.Events[len(lastBatch.Events)-1]
	eventCount := int64(0)
	for _, batch := range historyBatches {
		eventCount += int64(len(batch.Events))
	}
	header := &archiverspb.HistoryBlobHeader{
		Namespace:            i.request.Namespace,
		NamespaceId:          i.request.NamespaceID,
		WorkflowId:           i.request.WorkflowID,
		RunId:                i.request.RunID,
		IsLast:               i.FinishedIteration,
		FirstFailoverVersion: firstEvent.Version,
		LastFailoverVersion:  lastEvent.Version,
		FirstEventId:         firstEvent.EventId,
		LastEventId:          lastEvent.EventId,
		EventCount:           eventCount,
	}

	return &archiverspb.HistoryBlob{
		Header: header,
		Body:   historyBatches,
	}, nil
}

// HasNext returns true if there are more items to iterate over.
func (i *historyIterator) HasNext() bool {
	return !i.FinishedIteration
}

// GetState returns the encoded iterator state
func (i *historyIterator) GetState() ([]byte, error) {
	return json.Marshal(i.historyIteratorState)
}

func (i *historyIterator) readHistoryBatches(
	ctx context.Context,
	firstEventID int64,
) ([]*historypb.History, historyIteratorState, error) {
	size := 0
	targetSize := i.targetHistoryBlobSize
	var historyBatches []*historypb.History
	newIterState := historyIteratorState{}
	for size < targetSize {
		currHistoryBatches, err := i.readHistory(ctx, firstEventID)
		if _, isNotFound := err.(*serviceerror.NotFound); isNotFound && firstEventID != common.FirstEventID {
			newIterState.FinishedIteration = true
			return historyBatches, newIterState, nil
		}
		if err != nil {
			return nil, newIterState, err
		}
		for idx, batch := range currHistoryBatches {
			historyBatchSize, err := i.sizeEstimator.EstimateSize(batch)
			if err != nil {
				return nil, newIterState, err
			}
			size += historyBatchSize
			historyBatches = append(historyBatches, batch)
			firstEventID = batch.Events[len(batch.Events)-1].EventId + 1

			// In case targetSize is satisfied before reaching the end of current set of batches, return immediately.
			// Otherwise, we need to look ahead to see if there's more history batches.
			if size >= targetSize && idx != len(currHistoryBatches)-1 {
				newIterState.FinishedIteration = false
				newIterState.NextEventID = firstEventID
				return historyBatches, newIterState, nil
			}
		}
	}

	// If you are here, it means the target size is met after adding the last batch of read history.
	// We need to check if there's more history batches.
	_, err := i.readHistory(ctx, firstEventID)
	if _, isNotFound := err.(*serviceerror.NotFound); isNotFound && firstEventID != common.FirstEventID {
		newIterState.FinishedIteration = true
		return historyBatches, newIterState, nil
	}
	if err != nil {
		return nil, newIterState, err
	}
	newIterState.FinishedIteration = false
	newIterState.NextEventID = firstEventID
	return historyBatches, newIterState, nil
}

func (i *historyIterator) readHistory(ctx context.Context, firstEventID int64) ([]*historypb.History, error) {
	req := &persistence.ReadHistoryBranchRequest{
		BranchToken: i.request.BranchToken,
		MinEventID:  firstEventID,
		MaxEventID:  common.EndEventID,
		PageSize:    i.historyPageSize,
		ShardID:     i.request.ShardID,
	}
	historyBatches, _, _, err := persistence.ReadFullPageEventsByBatch(ctx, i.executionManager, req)
	return historyBatches, err
}

// reset resets iterator to a certain state given its encoded representation
// if it returns an error, the operation will have no effect on the iterator
func (i *historyIterator) reset(stateToken []byte) error {
	var iteratorState historyIteratorState
	if err := json.Unmarshal(stateToken, &iteratorState); err != nil {
		return err
	}
	i.historyIteratorState = iteratorState
	return nil
}

type (
	// SizeEstimator is used to estimate the size of any object
	SizeEstimator interface {
		EstimateSize(v interface{}) (int, error)
	}

	jsonSizeEstimator struct {
	}
)

func (e *jsonSizeEstimator) EstimateSize(v interface{}) (int, error) {
	// protojson must be used for proto structs.
	if protoMessage, ok := v.(proto.Message); ok {
		bs, err := protojson.Marshal(protoMessage)
		return len(bs), err
	}

	data, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

// NewJSONSizeEstimator returns a new SizeEstimator which uses json encoding to estimate size
func NewJSONSizeEstimator() SizeEstimator {
	return &jsonSizeEstimator{}
}

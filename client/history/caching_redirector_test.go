package history

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/server/api/historyservice/v1"
	"go.temporal.io/server/api/historyservicemock/v1"
	"go.temporal.io/server/common/convert"
	"go.temporal.io/server/common/dynamicconfig"
	"go.temporal.io/server/common/log"
	"go.temporal.io/server/common/membership"
	serviceerrors "go.temporal.io/server/common/serviceerror"
	"go.uber.org/mock/gomock"
)

type (
	cachingRedirectorSuite struct {
		suite.Suite
		*require.Assertions

		controller  *gomock.Controller
		connections *MockconnectionPool
		logger      log.Logger
		resolver    *membership.MockServiceResolver
	}
)

func TestCachingRedirectorSuite(t *testing.T) {
	s := new(cachingRedirectorSuite)
	suite.Run(t, s)
}

func (s *cachingRedirectorSuite) SetupTest() {
	s.Assertions = require.New(s.T())
	s.controller = gomock.NewController(s.T())

	s.connections = NewMockconnectionPool(s.controller)
	s.logger = log.NewNoopLogger()
	s.resolver = membership.NewMockServiceResolver(s.controller)
	s.resolver.EXPECT().AddListener(cachingRedirectorListener, gomock.Any()).Return(nil).AnyTimes()
	s.resolver.EXPECT().RemoveListener(cachingRedirectorListener).Return(nil).AnyTimes()
}

func (s *cachingRedirectorSuite) TearDownTest() {
	s.controller.Finish()
}

func (s *cachingRedirectorSuite) newCachingDirector(staleTTL time.Duration) *cachingRedirector {
	return newCachingRedirector(
		s.connections,
		s.resolver,
		s.logger,
		dynamicconfig.GetDurationPropertyFn(staleTTL),
	)
}

func (s *cachingRedirectorSuite) TestShardCheck() {
	r := s.newCachingDirector(0)
	defer r.stop()

	invalErr := &serviceerror.InvalidArgument{}
	err := r.execute(
		context.Background(),
		-1,
		func(_ context.Context, _ historyservice.HistoryServiceClient) error {
			panic("notreached")
		})
	s.ErrorAs(err, &invalErr)

	_, err = r.clientForShardID(-1)
	s.ErrorAs(err, &invalErr)
}

func cacheRetainingTest(s *cachingRedirectorSuite, opErr error, verify func(error)) {
	testAddr := rpcAddress("testaddr")
	shardID := int32(1)

	s.resolver.EXPECT().
		Lookup(convert.Int32ToString(shardID)).
		Return(membership.NewHostInfoFromAddress(string(testAddr)), nil).
		Times(1)

	mockClient := historyservicemock.NewMockHistoryServiceClient(s.controller)
	clientConn := clientConnection{
		historyClient: mockClient,
	}
	s.connections.EXPECT().
		getOrCreateClientConn(testAddr).
		Return(clientConn)
	s.connections.EXPECT().
		resetConnectBackoff(clientConn)

	clientOp := func(ctx context.Context, client historyservice.HistoryServiceClient) error {
		if client != mockClient {
			return errors.New("wrong client")
		}
		return opErr
	}
	r := newCachingRedirector(s.connections, s.resolver, s.logger, dynamicconfig.GetDurationPropertyFn(0))
	defer r.stop()

	for i := 0; i < 3; i++ {
		err := r.execute(
			context.Background(),
			shardID,
			clientOp,
		)
		verify(err)
	}
}

func (s *cachingRedirectorSuite) TestExecuteShardSuccess() {
	cacheRetainingTest(s, nil, func(err error) {
		s.NoError(err)
	})
}

func (s *cachingRedirectorSuite) TestExecuteCacheRetainingError() {
	notFound := serviceerror.NewNotFound("notfound")
	cacheRetainingTest(s, notFound, func(err error) {
		s.Error(err)
		s.Equal(notFound, err)
	})
}

func hostDownErrorTest(s *cachingRedirectorSuite, clientOp clientOperation, verify func(err error)) {
	testAddr := rpcAddress("testaddr")
	shardID := int32(1)

	s.resolver.EXPECT().
		Lookup(convert.Int32ToString(shardID)).
		Return(membership.NewHostInfoFromAddress(string(testAddr)), nil).
		Times(1)

	mockClient := historyservicemock.NewMockHistoryServiceClient(s.controller)
	clientConn := clientConnection{
		historyClient: mockClient,
	}
	s.connections.EXPECT().
		getOrCreateClientConn(testAddr).
		Return(clientConn).
		Times(1)
	s.connections.EXPECT().
		resetConnectBackoff(clientConn).
		Times(1)

	r := s.newCachingDirector(0)
	defer r.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := r.execute(
		ctx,
		shardID,
		clientOp,
	)
	verify(err)
}

func (s *cachingRedirectorSuite) TestDeadlineExceededError() {
	hostDownErrorTest(s,
		func(ctx context.Context, client historyservice.HistoryServiceClient) error {
			<-ctx.Done()
			return ctx.Err()
		},
		func(err error) {
			s.ErrorIs(err, context.DeadlineExceeded)
		})
}

func (s *cachingRedirectorSuite) TestUnavailableError() {
	hostDownErrorTest(s,
		func(ctx context.Context, client historyservice.HistoryServiceClient) error {
			return serviceerror.NewUnavailable("unavail")
		},
		func(err error) {
			unavail := &serviceerror.Unavailable{}
			s.ErrorAs(err, &unavail)
		})
}

func (s *cachingRedirectorSuite) TestShardOwnershipLostErrors() {
	testAddr1 := rpcAddress("testaddr1")
	testAddr2 := rpcAddress("testaddr2")
	shardID := int32(1)

	mockClient1 := historyservicemock.NewMockHistoryServiceClient(s.controller)
	mockClient2 := historyservicemock.NewMockHistoryServiceClient(s.controller)

	r := s.newCachingDirector(0)
	defer r.stop()
	opCalls := 1
	doExecute := func() error {
		return r.execute(
			context.Background(),
			shardID,
			func(ctx context.Context, client historyservice.HistoryServiceClient) error {
				switch opCalls {
				case 1:
					if client != mockClient1 {
						return errors.New("wrong client")
					}
					opCalls++
					return serviceerrors.NewShardOwnershipLost(string(testAddr1), "current")
				case 2:
					if client != mockClient1 {
						return errors.New("wrong client")
					}
					opCalls++
					return serviceerrors.NewShardOwnershipLost("", "current")
				case 3:
					if client != mockClient1 {
						return errors.New("wrong client")
					}
					opCalls++
					return serviceerrors.NewShardOwnershipLost(string(testAddr2), "current")
				case 4:
					if client != mockClient2 {
						return errors.New("wrong client")
					}
					opCalls++
					return nil
				case 5:
					if client != mockClient2 {
						return errors.New("wrong client")
					}
					opCalls++
					return nil
				}
				return errors.New("too many op calls")
			},
		)
	}

	// opCall 1: return SOL, but with same owner as current.
	s.resolver.EXPECT().
		Lookup(convert.Int32ToString(shardID)).
		Return(membership.NewHostInfoFromAddress(string(testAddr1)), nil).
		Times(1)

	clientConn1 := clientConnection{
		historyClient: mockClient1,
	}
	s.connections.EXPECT().
		getOrCreateClientConn(testAddr1).
		Return(clientConn1).
		Times(1)
	s.connections.EXPECT().
		resetConnectBackoff(clientConn1).
		Times(1)

	err := doExecute()
	s.Error(err)
	solErr := &serviceerrors.ShardOwnershipLost{}
	s.ErrorAs(err, &solErr)
	s.Equal(string(testAddr1), solErr.OwnerHost)

	// opCall 2: return SOL, but with empty new owner hint.
	s.resolver.EXPECT().
		Lookup(convert.Int32ToString(shardID)).
		Return(membership.NewHostInfoFromAddress(string(testAddr1)), nil).
		Times(1)

	s.connections.EXPECT().
		getOrCreateClientConn(testAddr1).
		Return(clientConn1).
		Times(1)
	s.connections.EXPECT().
		resetConnectBackoff(clientConn1).
		Times(1)

	err = doExecute()
	s.Error(err)
	solErr = &serviceerrors.ShardOwnershipLost{}
	s.ErrorAs(err, &solErr)
	s.Empty(solErr.OwnerHost)
	s.Equal(3, opCalls)

	// opCall 3 & 4: return SOL with new owner hint.
	s.resolver.EXPECT().
		Lookup(convert.Int32ToString(shardID)).
		Return(membership.NewHostInfoFromAddress(string(testAddr1)), nil).
		Times(1)

	s.connections.EXPECT().
		getOrCreateClientConn(testAddr1).
		Return(clientConn1).
		Times(1)
	s.connections.EXPECT().
		resetConnectBackoff(clientConn1).
		Times(1)

	clientConn2 := clientConnection{
		historyClient: mockClient2,
	}
	s.connections.EXPECT().
		getOrCreateClientConn(testAddr2).
		Return(clientConn2).
		Times(1)
	s.connections.EXPECT().
		resetConnectBackoff(clientConn2).
		Times(1)

	err = doExecute()
	s.NoError(err)
	s.Equal(5, opCalls)

	// OpCall 5: should use cached lookup & connection, so no additional mocks.
	err = doExecute()
	s.NoError(err)
}

func (s *cachingRedirectorSuite) TestClientForTargetByShard() {
	testAddr := rpcAddress("testaddr")
	shardID := int32(1)

	s.resolver.EXPECT().
		Lookup(convert.Int32ToString(shardID)).
		Return(membership.NewHostInfoFromAddress(string(testAddr)), nil).
		Times(1)

	mockClient := historyservicemock.NewMockHistoryServiceClient(s.controller)
	clientConn := clientConnection{
		historyClient: mockClient,
	}
	s.connections.EXPECT().
		getOrCreateClientConn(testAddr).
		Return(clientConn)
	s.connections.EXPECT().
		resetConnectBackoff(clientConn).
		Times(1)

	r := s.newCachingDirector(0)
	defer r.stop()
	cli, err := r.clientForShardID(shardID)
	s.NoError(err)
	s.Equal(mockClient, cli)

	// No additional mocks; lookup should have been cached
	cli, err = r.clientForShardID(shardID)
	s.NoError(err)
	s.Equal(mockClient, cli)
}

func (s *cachingRedirectorSuite) TestStaleTTL() {
	testAddr1 := rpcAddress("testaddr1")
	shardID := int32(1)
	mockClient1 := historyservicemock.NewMockHistoryServiceClient(s.controller)
	clientConn1 := clientConnection{
		historyClient: mockClient1,
	}

	staleTTL := 500 * time.Millisecond
	r := s.newCachingDirector(staleTTL)
	defer r.stop()

	// Trigger the creation of a cache entry for the shard.
	s.resolver.EXPECT().
		Lookup(convert.Int32ToString(shardID)).
		Return(membership.NewHostInfoFromAddress(string(testAddr1)), nil).
		Times(1)

	s.connections.EXPECT().
		getOrCreateClientConn(testAddr1).
		Return(clientConn1).
		Times(1)
	s.connections.EXPECT().
		resetConnectBackoff(clientConn1).
		Times(1)

	cli, err := r.clientForShardID(shardID)
	s.NoError(err)
	s.Equal(mockClient1, cli)

	// Now simulate a membership update that changes the shard owner.
	mockClient2 := historyservicemock.NewMockHistoryServiceClient(s.controller)
	clientConn2 := clientConnection{
		historyClient: mockClient2,
	}
	testAddr2 := rpcAddress("testaddr2")
	s.resolver.EXPECT().
		Lookup(convert.Int32ToString(shardID)).
		Return(membership.NewHostInfoFromAddress(string(testAddr2)), nil).
		Times(1)

	// Simulate the update, should see the entry marked as stale.
	r.membershipUpdateCh <- &membership.ChangedEvent{}
	s.Eventually(func() bool {
		r.mu.RLock()
		defer r.mu.RUnlock()
		entry := r.mu.cache[shardID]
		return !entry.staleAt.IsZero()
	}, 4*staleTTL, staleTTL)

	s.resolver.EXPECT().
		Lookup(convert.Int32ToString(shardID)).
		Return(membership.NewHostInfoFromAddress(string(testAddr2)), nil).
		Times(1)
	s.connections.EXPECT().
		getOrCreateClientConn(testAddr2).
		Return(clientConn2).
		Times(1)
	s.connections.EXPECT().
		resetConnectBackoff(clientConn2).
		Times(1)

	cli, err = r.clientForShardID(shardID)
	s.NoError(err)
	s.Equal(mockClient2, cli)
}

package langserver

import (
	"context"
	"testing"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/require"
)

func TestCancel(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	c := NewCancel()
	id1 := jsonrpc2.ID{Num: 1}
	id2 := jsonrpc2.ID{Num: 2}
	id3 := jsonrpc2.ID{Num: 3}
	ctx1, cancel1 := c.WithCancel(context.Background(), id1)
	ctx2, cancel2 := c.WithCancel(context.Background(), id2)
	ctx3, cancel3 := c.WithCancel(context.Background(), id3)

	require.NoError(ctx1.Err())
	require.NoError(ctx2.Err())
	require.NoError(ctx3.Err())

	cancel1()
	require.Error(ctx1.Err())
	require.NoError(ctx2.Err())
	require.NoError(ctx3.Err())

	c.Cancel(id2)
	require.Error(ctx2.Err())
	require.NoError(ctx3.Err())

	// we always need to call cancel from a WithCancel, even if it is
	// already cancelled. Calling to ensure no panic/etc
	cancel2()

	cancel3()
	require.Error(ctx3.Err())

	// If we try to cancel something that has already been cancelled, it
	// should just be a noop.
	c.Cancel(id3)
}

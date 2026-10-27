package conduit

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalForwardTrafficAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forward, err := newLocalForward(ctx, "127.0.0.1", "0")
	require.NoError(t, err)
	defer forward.Close()
	upstream, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", forward.targetPort))
	require.NoError(t, err)
	defer upstream.Close()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := upstream.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		body, err := io.ReadAll(conn)
		if err == nil {
			_, err = conn.Write(append([]byte("reply:"), body...))
		}
		serverDone <- err
	}()
	client, err := net.DialTimeout("tcp", forward.listener.Addr().String(), time.Second)
	require.NoError(t, err)
	defer client.Close()
	require.NoError(t, client.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = client.Write([]byte("hello"))
	require.NoError(t, err)
	require.NoError(t, client.(*net.TCPConn).CloseWrite())
	reply, err := io.ReadAll(client)
	require.NoError(t, err)
	require.Equal(t, "reply:hello", string(reply))
	require.NoError(t, <-serverDone)
	cancel()
	select {
	case <-forward.done:
	case <-time.After(time.Second):
		t.Fatal("forward did not stop")
	}
}

func TestLocalForwardClosesActiveConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forward, err := newLocalForward(ctx, "127.0.0.1", "0")
	require.NoError(t, err)
	defer forward.Close()
	upstream, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", forward.targetPort))
	require.NoError(t, err)
	defer upstream.Close()
	client, err := net.Dial("tcp", forward.listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()
	require.NoError(t, upstream.(*net.TCPListener).SetDeadline(time.Now().Add(5*time.Second)))
	conn, err := upstream.Accept()
	require.NoError(t, err)
	defer conn.Close()
	cancel()
	select {
	case <-forward.done:
	case <-time.After(time.Second):
		t.Fatal("active forward did not stop")
	}
	require.NoError(t, client.SetReadDeadline(time.Now().Add(time.Second)))
	_, err = client.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
}

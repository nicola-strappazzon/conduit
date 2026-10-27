package conduit

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// localForward exposes the plugin's loopback listener on the requested IP.
type localForward struct {
	listener   net.Listener
	targetPort string
	cancel     context.CancelFunc
	done       chan struct{}
}

func newLocalForward(parent context.Context, address, port string) (*localForward, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(address, port))
	if err != nil {
		return nil, fmt.Errorf("opening local listener: %w", err)
	}
	// The plugin chooses its own listener; reserve a different loopback port
	// and release it immediately before starting the session.
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("selecting SSM local port: %w", err)
	}
	_, targetPort, _ := net.SplitHostPort(probe.Addr().String())
	probe.Close()
	ctx, cancel := context.WithCancel(parent)
	f := &localForward{listener: listener, targetPort: targetPort, cancel: cancel, done: make(chan struct{})}
	go f.serve(ctx)
	return f, nil
}

func (f *localForward) Close() {
	f.cancel()
	f.listener.Close()
	<-f.done
}

func (f *localForward) serve(ctx context.Context) {
	var connections sync.WaitGroup
	defer close(f.done)
	defer connections.Wait()
	stop := context.AfterFunc(ctx, func() { f.listener.Close() })
	defer stop()
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			return
		}
		connections.Add(1)
		go func() {
			defer connections.Done()
			defer conn.Close()
			closeClient := context.AfterFunc(ctx, func() { conn.Close() })
			defer closeClient()
			upstream, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort("localhost", f.targetPort))
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("connecting to local SSM tunnel: %v", err)
				}
				return
			}
			defer upstream.Close()
			closeUpstream := context.AfterFunc(ctx, func() { upstream.Close() })
			defer closeUpstream()
			var copies sync.WaitGroup
			copyStream := func(dst, src net.Conn) {
				defer copies.Done()
				_, _ = io.Copy(dst, src)
				if tcp, ok := dst.(*net.TCPConn); ok {
					_ = tcp.CloseWrite()
				}
			}
			copies.Add(2)
			go copyStream(upstream, conn)
			go copyStream(conn, upstream)
			copies.Wait()
		}()
	}
}

package websocket

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/gorilla/websocket"
	"net/http"
	"sync"
	"time"
)

// MessageHandler is the function type for receiving messages on the connection.
type MessageHandler func(message []byte) error

// ConnectHandler is the function type for an action to be performed
// each time a new connection is established or reestablished.
type ConnectHandler func(w Client) error

// Client is the interface for sending and closing the websocket client.
type Client interface {
	Send(message []byte) error
	Close() error
}

// client is the internal implementation of Client.
type client struct {
	reconnectDelay time.Duration
	ctx            context.Context
	cancel         context.CancelFunc
	conn           *websocket.Conn
	token          string
	handler        MessageHandler
	url            string
	l              logr.Logger
	done           chan bool
	m              *sync.Mutex
	buf            chan []byte
	closed         bool
	closeMutex     *sync.Mutex

	// onConnect is called any time the client successfully connects or reconnects.
	onConnect ConnectHandler
}

// NewClient creates a websocket client and starts the connection loop.
// The onConnect handler is optional; if provided, it will be called
// whenever a connection is established or reestablished.
func NewClient(
	ctx context.Context,
	l logr.Logger,
	url string,
	token string,
	reconnectDelay time.Duration,
	onMessage MessageHandler,
	onConnect ConnectHandler,
) (Client, error) {
	if onMessage == nil {
		return nil, errors.New("onMessage is nil")
	}
	ctx, cancel := context.WithCancel(ctx)
	c := &client{
		reconnectDelay: reconnectDelay,
		ctx:            ctx,
		cancel:         cancel,
		token:          token,
		handler:        onMessage,
		l:              l,
		url:            url,
		done:           make(chan bool),
		buf:            make(chan []byte, 1000),
		m:              &sync.Mutex{},
		closed:         false,
		closeMutex:     &sync.Mutex{},
		onConnect:      onConnect,
	}
	go c.run()
	return c, nil
}

// Send writes a message to the websocket connection if it is established.
// If no connection exists yet, the message is buffered until one is available.
func (c *client) Send(message []byte) error {
	c.m.Lock()
	defer c.m.Unlock()
	if c.conn == nil {
		if len(c.buf) == cap(c.buf) {
			return errors.New("buffer full, client is not connected, dropping message")
		}
		c.buf <- message
		return nil
	}
	return c.conn.WriteMessage(websocket.TextMessage, message)
}

// Close stops the client connection loop and cleans up resources.
func (c *client) Close() error {
	c.closeMutex.Lock()
	defer c.closeMutex.Unlock()

	if c.closed {
		return errors.New("client already closed")
	}
	c.closed = true
	c.cancel()
	<-c.done
	return nil
}

// run manages the lifecycle of the websocket client, reconnecting
// automatically with a fixed delay.
func (c *client) run() {
	for {
		select {
		case <-c.ctx.Done():
			c.signalDone()
			return
		default:
		}

		err := c.connect()
		if err != nil {
			c.l.Error(err, fmt.Sprintf("Connection terminated: Retrying in %s...", c.reconnectDelay))
		}

		select {
		case <-c.ctx.Done():
			c.signalDone()
			return
		case <-time.After(c.reconnectDelay):
			continue
		}
	}
}

// connect attempts to establish the websocket connection, reads messages,
// and handles disconnections. It will return when the connection is lost.
func (c *client) connect() error {
	signalConnectionDone := sync.Once{}
	connectionDone := make(chan error)

	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	conn, _, err := websocket.DefaultDialer.Dial(c.url, header)
	if err != nil {
		return err
	}
	// If we exit before the function ends, make sure to close the connection.
	defer func() { _ = conn.Close() }()

	c.m.Lock()
	c.conn = conn
	c.drainBuffer()
	c.m.Unlock()

	// Call onConnect right after the connection is established.
	if c.onConnect != nil {
		if err := c.onConnect(c); err != nil {
			return err
		}
	}

	conn.SetCloseHandler(func(code int, text string) error {
		signalConnectionDone.Do(func() {
			connectionDone <- errors.New(text)
		})
		return nil
	})

	// Goroutine to receive messages and forward them to the handler.
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				signalConnectionDone.Do(func() {
					connectionDone <- err
				})
				return
			}
			if err = c.handler(message); err != nil {
				signalConnectionDone.Do(func() {
					connectionDone <- err
				})
				return
			}
		}
	}()

	// Wait for either a disconnection or context cancellation.
	select {
	case <-c.ctx.Done():
		_ = conn.Close()
		return nil
	case err := <-connectionDone:
		c.m.Lock()
		c.conn = nil
		c.m.Unlock()
		return err
	}
}

// drainBuffer empties any messages accumulated while disconnected
// and writes them to the newly established connection.
func (c *client) drainBuffer() {
	for len(c.buf) > 0 {
		message := <-c.buf
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			c.l.Error(err, "Failed to send message to websocket")
			return
		}
	}
}

// signalDone signals that the run loop has exited.
func (c *client) signalDone() {
	c.done <- true
	close(c.done)
}

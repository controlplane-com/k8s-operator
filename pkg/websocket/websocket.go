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

type Handler func(message []byte) error

type Client interface {
	Send(message []byte) error
	Close() error
}

type client struct {
	reconnectDelay time.Duration
	ctx            context.Context
	cancel         context.CancelFunc
	conn           *websocket.Conn
	token          string
	handler        Handler
	url            string
	l              logr.Logger
	done           chan bool
	m              *sync.Mutex
	buf            chan []byte
	closed         bool
	closeMutex     *sync.Mutex
}

func NewClient(ctx context.Context, l logr.Logger, url string, token string, reconnectDelay time.Duration, handler Handler) (Client, error) {
	if handler == nil {
		return nil, errors.New("handler is nil")
	}
	ctx, cancel := context.WithCancel(ctx)
	c := &client{
		reconnectDelay: reconnectDelay,
		ctx:            ctx,
		cancel:         cancel,
		token:          token,
		handler:        handler,
		l:              l,
		url:            url,
		done:           make(chan bool),
		buf:            make(chan []byte, 1000),
		m:              &sync.Mutex{},
		closed:         false,
		closeMutex:     &sync.Mutex{},
	}
	go c.run()
	return c, nil
}

func (c *client) Send(message []byte) error {
	c.m.Lock()
	defer c.m.Unlock()
	if c.conn == nil {
		if len(c.buf) == 1000 {
			return errors.New("buffer full, client is not connected, dropping message")
		}
		c.buf <- message
		return nil
	}
	return c.conn.WriteMessage(websocket.TextMessage, message)
}

func (c *client) drainBuffer() {
	for len(c.buf) > 0 {
		message := <-c.buf
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			c.l.Error(err, "Failed to send message to websocket")
			return
		}
	}
}

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

func (c *client) signalDone() {
	c.done <- true
	close(c.done)
}

func (c *client) run() {
	for {
		// Check if the context is done before attempting a connection
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

func (c *client) connect() error {
	signalConnectionDone := sync.Once{}
	connectionDone := make(chan error)
	// Establish a connection
	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	conn, _, err := websocket.DefaultDialer.Dial(c.url, header)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	c.m.Lock()
	c.conn = conn
	c.drainBuffer()
	c.m.Unlock()

	conn.SetCloseHandler(func(code int, text string) error {
		signalConnectionDone.Do(func() {
			connectionDone <- errors.New(text)
		})
		return nil
	})

	// Listen for messages or handle disconnections
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

	// Block until disconnection occurs, or context is cancelled
	select {
	case <-c.ctx.Done():
		_ = conn.Close() // Close the connection in case of context cancellation
		return nil
	case err := <-connectionDone:
		c.m.Lock()
		c.conn = nil
		c.m.Unlock()
		return err
	}
}

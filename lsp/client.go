package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	mu      sync.Mutex
	nextID  int
	pending map[int]chan json.RawMessage

	// OnDiagnostics is called when the server publishes diagnostics.
	OnDiagnostics func(params PublishDiagnosticsParams)

	closed bool
}

func NewClient(command string, args ...string) (*Client, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil // discard stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		nextID:  1,
		pending: make(map[int]chan json.RawMessage),
	}

	go c.readLoop()
	return c, nil
}

func (c *Client) readLoop() {
	for !c.closed {
		// Read Content-Length header
		header, err := c.stdout.ReadString('\n')
		if err != nil {
			return
		}
		header = strings.TrimSpace(header)
		if !strings.HasPrefix(header, "Content-Length:") {
			continue
		}
		lengthStr := strings.TrimSpace(strings.TrimPrefix(header, "Content-Length:"))
		length, err := strconv.Atoi(lengthStr)
		if err != nil {
			continue
		}

		// Read blank line separator
		c.stdout.ReadString('\n')

		// Read body
		body := make([]byte, length)
		_, err = io.ReadFull(c.stdout, body)
		if err != nil {
			return
		}

		var msg Response
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}

		if msg.ID != nil {
			// Response to a request
			c.mu.Lock()
			ch, ok := c.pending[*msg.ID]
			if ok {
				delete(c.pending, *msg.ID)
			}
			c.mu.Unlock()
			if ok {
				ch <- msg.Result
			}
		} else if msg.Method != "" {
			// Server notification
			c.handleNotification(msg.Method, msg.Params)
		}
	}
}

func (c *Client) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "textDocument/publishDiagnostics":
		if c.OnDiagnostics != nil {
			var p PublishDiagnosticsParams
			if err := json.Unmarshal(params, &p); err == nil {
				c.OnDiagnostics(p)
			}
		}
	}
}

func (c *Client) sendRequest(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	ch := make(chan json.RawMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := c.send(req); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	result := <-ch
	return result, nil
}

func (c *Client) sendNotification(method string, params interface{}) error {
	msg := struct {
		JSONRPC string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.send(msg)
}

func (c *Client) send(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err = c.stdin.Write([]byte(header))
	if err != nil {
		return err
	}
	_, err = c.stdin.Write(data)
	return err
}

func (c *Client) Close() {
	c.closed = true
	c.sendNotification("shutdown", nil)
	c.sendNotification("exit", nil)
	c.stdin.Close()
	c.cmd.Wait()
}

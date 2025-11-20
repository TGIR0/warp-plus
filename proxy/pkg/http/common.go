package http

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/http"
	"sync"
)

// copyBuffer is a helper function to copy data between two net.Conn objects.
// func copyBuffer(dst, src net.Conn, buf []byte) (int64, error) {
// 	return io.CopyBuffer(dst, src, buf)
// }

type responseWriter struct {
	conn    net.Conn
	headers http.Header
	status  int
	written bool
}

func NewHTTPResponseWriter(conn net.Conn) http.ResponseWriter {
	return &responseWriter{
		conn:    conn,
		headers: http.Header{},
		status:  http.StatusOK,
	}
}

func (rw *responseWriter) Header() http.Header {
	return rw.headers
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	if rw.written {
		return
	}
	rw.status = statusCode
	rw.written = true

	statusText := http.StatusText(statusCode)
	if statusText == "" {
		statusText = fmt.Sprintf("status code %d", statusCode)
	}
	_, _ = fmt.Fprintf(rw.conn, "HTTP/1.1 %d %s\r\n", statusCode, statusText)
	_ = rw.headers.Write(rw.conn)
	_, _ = rw.conn.Write([]byte("\r\n"))
}

func (rw *responseWriter) Write(data []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.conn.Write(data)
}

type BufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func NewBufferedConn(c net.Conn, r *bufio.Reader) *BufferedConn {
	return &BufferedConn{Conn: c, r: r}
}

func (b *BufferedConn) Read(p []byte) (int, error) {
	return b.r.Read(p)
}

type customConn struct {
	net.Conn
	reader      *bufio.Reader
	req         *http.Request
	initialData []byte
	once        sync.Once
}

func (c *customConn) Read(p []byte) (n int, err error) {
	c.once.Do(func() {
		buf := &bytes.Buffer{}
		err = c.req.Write(buf)
		if err != nil {
			n = 0
			return
		}
		c.initialData = buf.Bytes()
	})

	if len(c.initialData) > 0 {
		copy(p, c.initialData)
		n = len(p)
		c.initialData = nil
		return
	}

	if c.reader != nil {
		return c.reader.Read(p)
	}
	return c.Conn.Read(p)
}

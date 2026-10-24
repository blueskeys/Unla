package core

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amoylab/unla/internal/common/config"
	"github.com/amoylab/unla/internal/mcp/session"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHTTPBodyBudget(t *testing.T) {
	t.Run("exact limit", func(t *testing.T) {
		data, err := readHTTPBody(context.Background(), strings.NewReader(strings.Repeat("x", maxHTTPBodyBytes)))
		require.NoError(t, err)
		require.Len(t, data, maxHTTPBodyBytes)
	})
	t.Run("wire limit", func(t *testing.T) {
		r := strings.NewReader(strings.Repeat("x", maxHTTPBodyBytes*2))
		_, err := readDecodedResponseBody(&http.Response{Header: http.Header{}, Body: io.NopCloser(r)})
		require.ErrorContains(t, err, "4 MiB")
		require.Equal(t, maxHTTPBodyBytes-1, r.Len())
	})
	t.Run("gzip expansion", func(t *testing.T) {
		var compressed bytes.Buffer
		w := gzip.NewWriter(&compressed)
		_, err := w.Write(bytes.Repeat([]byte("x"), maxHTTPBodyBytes+1))
		require.NoError(t, err)
		require.NoError(t, w.Close())
		_, err = readDecodedResponseBody(&http.Response{Header: http.Header{"Content-Encoding": {"gzip"}}, Body: io.NopCloser(&compressed)})
		require.ErrorContains(t, err, "4 MiB")
	})
	t.Run("encoding layers", func(t *testing.T) {
		_, err := readDecodedResponseBody(&http.Response{Header: http.Header{"Content-Encoding": {"gzip,gzip,gzip,gzip"}}, Body: io.NopCloser(strings.NewReader(""))})
		require.ErrorContains(t, err, "layers")
	})
	t.Run("canceled decode", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := readHTTPBody(ctx, strings.NewReader("ok"))
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestHTTPDecoderLimits(t *testing.T) {
	for _, encoding := range []string{"gzip", "br", "zstd", "deflate", "raw-deflate"} {
		t.Run(encoding, func(t *testing.T) {
			var compressed bytes.Buffer
			var writer io.WriteCloser
			switch encoding {
			case "gzip":
				writer = gzip.NewWriter(&compressed)
			case "br":
				writer = brotli.NewWriter(&compressed)
			case "zstd":
				w, err := zstd.NewWriter(&compressed)
				require.NoError(t, err)
				writer = w
			case "deflate":
				writer = zlib.NewWriter(&compressed)
			case "raw-deflate":
				w, err := flate.NewWriter(&compressed, flate.DefaultCompression)
				require.NoError(t, err)
				writer = w
			}
			_, err := writer.Write(bytes.Repeat([]byte("x"), maxHTTPBodyBytes+1))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			if encoding == "raw-deflate" {
				encoding = "deflate"
			}
			_, err = decodeBodyBytesWithContext(context.Background(), compressed.Bytes(), encoding)
			require.ErrorContains(t, err, "4 MiB")
		})
	}
	t.Run("oversized intermediate layer", func(t *testing.T) {
		// The inner gzip bytes are oversized, even before any inner decoding.
		var outer bytes.Buffer
		w := gzip.NewWriter(&outer)
		_, err := w.Write(bytes.Repeat([]byte("x"), maxHTTPBodyBytes+1))
		require.NoError(t, err)
		require.NoError(t, w.Close())
		_, err = readDecodedResponseBody(&http.Response{Header: http.Header{"Content-Encoding": {"gzip,gzip"}}, Body: io.NopCloser(&outer)})
		require.ErrorContains(t, err, "4 MiB")
	})
}

func TestHTTPToolDeadline(t *testing.T) {
	for _, phase := range []string{"headers", "body"} {
		t.Run(phase, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if phase == "body" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer upstream.Close()
			s := &Server{logger: zap.NewNop(), toolRespHandler: CreateResponseHandlerChain()}
			s.internalNetACL, _ = parseInternalNetworkAllowlist([]string{"127.0.0.0/8"})
			tool := &config.ToolConfig{Name: "slow", Method: "GET", Endpoint: upstream.URL}
			client, err := s.createHTTPClient(tool, "")
			require.NoError(t, err)
			require.Equal(t, 30*time.Second, client.Timeout)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/test/mcp", nil).WithContext(ctx)
			conn := &fakeConnExec{meta: &session.Meta{Request: &session.RequestInfo{}}}
			start := time.Now()
			_, err = s.executeHTTPTool(c, conn, tool, map[string]any{}, nil)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Less(t, time.Since(start), 2*time.Second)
		})
	}
}

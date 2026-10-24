package core

import (
	"context"
	"fmt"
	"io"
	"time"
)

const (
	httpToolTimeout          = 30 * time.Second
	maxHTTPBodyBytes         = 4 << 20
	maxDelegatedResultBytes  = 1 << 20
	maxContentEncodingLayers = 3
)

type budgetReader struct {
	ctx context.Context
	r   io.Reader
}

func (r budgetReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > 32<<10 {
		p = p[:32<<10]
	}
	return r.r.Read(p)
}

func readHTTPBody(ctx context.Context, r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(budgetReader{ctx: ctx, r: r}, maxHTTPBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxHTTPBodyBytes {
		return nil, fmt.Errorf("HTTP body exceeds 4 MiB limit")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}

// SPDX-License-Identifier: Apache-2.0

package httpapi_test

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/push"
)

func TestPushClientAgainstEngine(t *testing.T) {
	srv := httptest.NewServer(newAPI(t, setup{}))
	defer srv.Close()
	c := push.New(srv.URL, tokenA)
	if err := c.RegisterManifest(ctx, sampleManifest(t)); err != nil {
		t.Fatal(err)
	}
	b, err := adapter.DecodeBatch(bytes.NewReader(sampleBatch(t)))
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Push(ctx, b)
	if err != nil || len(res) != 1 || res[0].Created != b.RecordCount() || res[0].SnapshotID == "" {
		t.Fatalf("first push = %+v, %v", res, err)
	}
	if again, err := c.Push(ctx, b); err != nil || !again[0].NoChanges {
		t.Fatalf("second push = %+v, %v", again, err)
	}

	c.MaxRecords = 2
	b.Batch.Mode = adapter.ModeIncremental
	parts, err := c.Push(ctx, b)
	if err != nil || len(parts) != (b.RecordCount()+1)/2 {
		t.Fatalf("split push = %d parts, %v", len(parts), err)
	}
	for _, p := range parts {
		if !p.NoChanges || p.RejectedRecords != 0 {
			t.Fatalf("part = %+v", p)
		}
	}
	b.Batch.Mode = adapter.ModeFull
	if _, err := c.Push(ctx, b); !errors.Is(err, push.ErrFullBatchTooLarge) {
		t.Fatalf("oversized full push = %v", err)
	}

	var ae *push.APIError
	if err := push.New(srv.URL, "wrong-token").RegisterManifest(ctx, sampleManifest(t)); !errors.As(err, &ae) || ae.Status != 401 || ae.Code != "unauthorized" {
		t.Fatalf("bad token = %v", err)
	}
}

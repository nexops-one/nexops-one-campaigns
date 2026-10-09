// SPDX-License-Identifier: Apache-2.0

package console

import (
	"crypto/sha256"
	"sync"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/importer"
)

const (
	pendingTTL        = 30 * time.Minute
	pendingPerSession = 4
)

// pendingImport is an uploaded file validated by a dry run and waiting for
// its commit. It lives in process memory only.
type pendingImport struct {
	id      string
	session string // session hash
	input   importer.Input
	sum     [32]byte
	created time.Time
}

// pendingImports keeps validated uploads so that the commit imports exactly
// the bytes that were validated, without a second upload.
type pendingImports struct {
	mu    sync.Mutex
	now   func() time.Time
	items map[string]*pendingImport
}

func newPendingImports(now func() time.Time) *pendingImports {
	return &pendingImports{now: now, items: map[string]*pendingImport{}}
}

// put stores an upload for a session, dropping expired entries and the
// session's oldest when it already holds pendingPerSession.
func (p *pendingImports) put(session string, in importer.Input) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	var oldest *pendingImport
	n := 0
	for id, it := range p.items {
		if now.Sub(it.created) >= pendingTTL {
			delete(p.items, id)
			continue
		}
		if it.session == session {
			n++
			if oldest == nil || it.created.Before(oldest.created) {
				oldest = it
			}
		}
	}
	if n >= pendingPerSession && oldest != nil {
		delete(p.items, oldest.id)
	}
	id := randomToken()[:22]
	p.items[id] = &pendingImport{id: id, session: session, input: in, sum: sha256.Sum256(in.Data), created: now}
	return id
}

// take removes and returns a session's pending upload; false when it is
// unknown, expired or belongs to another session.
func (p *pendingImports) take(session, id string) (importer.Input, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	it, ok := p.items[id]
	if !ok || it.session != session {
		return importer.Input{}, false
	}
	delete(p.items, id)
	if p.now().Sub(it.created) >= pendingTTL || sha256.Sum256(it.input.Data) != it.sum {
		return importer.Input{}, false
	}
	return it.input, true
}

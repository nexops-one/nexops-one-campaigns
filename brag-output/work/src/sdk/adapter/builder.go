// SPDX-License-Identifier: Apache-2.0

package adapter

// Builder assembles a Batch for a manifest.
type Builder struct{ batch Batch }

// NewBuilder starts a batch whose source is the manifest's adapter.
func NewBuilder(m Manifest, system string) *Builder {
	return &Builder{batch: Batch{
		SchemaVersion: m.SchemaVersion,
		Source:        Source{System: system, Adapter: m.Name, AdapterVersion: m.Version},
		Entities:      map[string][]Record{},
	}}
}

func (b *Builder) info() *BatchInfo {
	if b.batch.Batch == nil {
		b.batch.Batch = &BatchInfo{}
	}
	return b.batch.Batch
}

// Mode sets the batch mode.
func (b *Builder) Mode(m Mode) *Builder { b.info().Mode = m; return b }

// BatchID sets the batch identifier.
func (b *Builder) BatchID(id string) *Builder { b.info().BatchID = id; return b }

// Add appends a record of entity.
func (b *Builder) Add(entity string, rec Record) *Builder {
	b.batch.Entities[entity] = append(b.batch.Entities[entity], rec)
	return b
}

// Build returns the batch.
func (b *Builder) Build() Batch { return b.batch }

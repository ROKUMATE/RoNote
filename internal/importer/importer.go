// Package importer runs the import pipeline: fetch a page, or reuse the
// stored copy, extract it, and keep it as the owner's document. The API
// enqueues imports through Service and the worker process runs them with
// Worker.Handle, as jobs of kind Kind (docs/adr/0005-import-pipeline.md,
// docs/adr/0009-jobs-port.md).
package importer

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/jobs"
)

const (
	// Kind is the kind of an import's job.
	Kind = "import"
	// maxAttempts counts every try of a job, retries included.
	maxAttempts = 3
	// maxRetryAfter caps how long a server's Retry-After may delay a retry.
	maxRetryAfter = 10 * time.Minute
)

// JobOptions say how import jobs run: three attempts, four at a time, a
// minute each. The API declares them too, to enqueue imports.
var JobOptions = jobs.Options{MaxAttempts: maxAttempts, Concurrency: 4, Timeout: 60 * time.Second}

// Args are an import job's payload. The owner travels with the import so
// every query the job runs is scoped to them.
type Args struct {
	ImportID uuid.UUID `json:"import_id"`
	OwnerID  uuid.UUID `json:"owner_id"`
}

// job is the job that runs an import. Its key is the import's, so one import
// never has two jobs.
func job(args Args) (jobs.Job, error) {
	payload, err := json.Marshal(args)
	return jobs.Job{Kind: Kind, Key: args.ImportID.String(), Payload: payload}, err
}

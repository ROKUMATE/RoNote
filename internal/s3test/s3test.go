// Package s3test starts a throwaway SeaweedFS for integration tests: an
// S3-compatible store with one bucket. Like pgtest, it always removes what
// it starts. Only tests may link it.
package s3test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/rokumate/ronote/internal/blob/s3blob"
)

// Image is the dev stack's SeaweedFS; TestImageMatchesCompose keeps the two
// in sync once the dev stack runs it.
const Image = "chrislusf/seaweedfs:4.53@sha256:ccad23e18639c8b98cee17e7100b95830a06077b56d8e5b006d6dcf8ec5e94a0"

// label marks containers started here so scripts/container.sh can spot leaks.
const label = "ronote.s3test"

// Credentials and bucket of every store started here.
const (
	accessKey = "ronote"
	secretKey = "ronote-test-secret" //nolint:gosec // the key of a throwaway test container
	Bucket    = "ronote"
)

const config = `{"identities":[{"name":"ronote","credentials":[{"accessKey":"` + accessKey + `","secretKey":"` + secretKey + `"}],"actions":["Admin","Read","Write","List","Tagging"]}]}`

// New starts SeaweedFS for one test, creates the bucket, and removes the
// container when the test ends, even if it fails.
func New(t testing.TB) s3blob.Config {
	t.Helper()
	cfg, stop, err := start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("s3test: %v", err)
		}
	})
	return cfg
}

// start runs SeaweedFS and creates the bucket. It returns the store's config
// and a func that removes the container. On error nothing is left running.
func start(ctx context.Context) (s3blob.Config, func() error, error) {
	ctr, err := testcontainers.Run(ctx, Image,
		testcontainers.WithCmd("server", "-s3", "-s3.config=/etc/seaweedfs/s3.json", "-dir=/data"),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader: strings.NewReader(config), ContainerFilePath: "/etc/seaweedfs/s3.json", FileMode: 0o644,
		}),
		testcontainers.WithExposedPorts("8333/tcp"),
		// S3 answers an anonymous request with 403 once it is up.
		testcontainers.WithWaitStrategy(wait.ForHTTP("/").WithPort("8333/tcp").
			WithStatusCodeMatcher(func(int) bool { return true }).WithStartupTimeout(2*time.Minute)),
		testcontainers.WithLabels(map[string]string{label: "true"}),
	)
	stop := func() error { return testcontainers.TerminateContainer(ctr) }
	if err != nil {
		err = fmt.Errorf("start seaweedfs (is a container runtime up? on macOS run podman machine start): %w", err)
		return s3blob.Config{}, nil, errors.Join(err, stop())
	}
	endpoint, err := ctr.PortEndpoint(ctx, "8333/tcp", "http")
	if err == nil {
		err = createBucket(ctx, ctr)
	}
	if err != nil {
		return s3blob.Config{}, nil, errors.Join(err, stop())
	}
	return s3blob.Config{Endpoint: endpoint, Region: "us-east-1", Bucket: Bucket, AccessKey: accessKey, SecretKey: secretKey}, stop, nil
}

// createBucket asks weed shell for the bucket, retrying while the master
// elects itself leader.
func createBucket(ctx context.Context, ctr *testcontainers.DockerContainer) error {
	cmd := []string{"sh", "-c", "echo 's3.bucket.create -name " + Bucket + "' | weed shell"}
	var last string
	for range 30 {
		code, out, err := ctr.Exec(ctx, cmd, tcexec.Multiplexed())
		if err != nil {
			return fmt.Errorf("create bucket: %w", err)
		}
		text, _ := io.ReadAll(out)
		last = string(text)
		if code == 0 && strings.Contains(last, "created bucket") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("create bucket: weed shell said %q", last)
}

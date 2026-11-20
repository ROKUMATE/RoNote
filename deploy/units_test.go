// Package deploy holds the production units, Caddy config and images
// (docs/adr/0014-production-topology.md); this test keeps the units to the
// ADR's rules.
package deploy

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// unit is a Quadlet file's keys, by section, each with every value given.
type unit map[string]map[string][]string

func readUnit(t *testing.T, path string) unit {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // the test's own files
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	u := unit{}
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			section = strings.Trim(line, "[]")
			if u[section] == nil {
				u[section] = map[string][]string{}
			}
		default:
			k, v, ok := strings.Cut(line, "=")
			if !ok || section == "" {
				t.Fatalf("%s: %q is not key=value in a section", path, line)
			}
			u[section][k] = append(u[section][k], v)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return u
}

func (u unit) container(key string) []string { return u["Container"][key] }

func glob(t *testing.T, pattern string) []string {
	t.Helper()
	paths, err := filepath.Glob(pattern)
	if err != nil || len(paths) == 0 {
		t.Fatalf("no files match %s (%v)", pattern, err)
	}
	return paths
}

// ciUnits are CI's units and drop-ins (deploy/quadlet/ci) matching pattern,
// held to the same rules.
func ciUnits(pattern string) []string {
	paths, _ := filepath.Glob(filepath.Join("quadlet", "ci", pattern))
	return paths
}

// Every container, the CI ones included, runs read-only, unprivileged and
// limited, and every service is health-checked; a one-shot, such as the
// migrations, only has to finish.
func TestEveryContainerIsHardened(t *testing.T) {
	t.Parallel()
	for _, path := range append(glob(t, "quadlet/*.container"), ciUnits("*.container")...) {
		u := readUnit(t, path)
		for key, want := range map[string]string{"ReadOnly": "true", "NoNewPrivileges": "true", "DropCapability": "ALL"} {
			if got := u.container(key); !slices.Equal(got, []string{want}) {
				t.Errorf("%s: %s = %q, want %s", path, key, got, want)
			}
		}
		args := strings.Join(u.container("PodmanArgs"), " ")
		if !strings.Contains(args, "--memory=") || !strings.Contains(args, "--cpus=") {
			t.Errorf("%s: PodmanArgs = %q, want --memory and --cpus", path, args)
		}
		if slices.Equal(u["Service"]["Type"], []string{"oneshot"}) {
			continue
		}
		health := u.container("HealthCmd")
		fake := strings.HasPrefix(path, filepath.Join("quadlet", "ci"))
		if len(health) != 1 || (health[0] == "none" && !fake) {
			t.Errorf("%s: HealthCmd = %q, want a check (only CI's distroless fakes may have none)", path, health)
		}
		if !slices.Equal(u["Service"]["Restart"], []string{"always"}) {
			t.Errorf("%s: Restart = %q, want always", path, u["Service"]["Restart"])
		}
	}
}

// Only Caddy publishes ports, CI's drop-ins included.
func TestOnlyCaddyPublishesPorts(t *testing.T) {
	t.Parallel()
	paths := append(glob(t, "quadlet/*.container"), ciUnits("*.container")...)
	paths = append(paths, ciUnits("*.d/*.conf")...)
	for _, path := range paths {
		published := readUnit(t, path).container("PublishPort")
		if filepath.Base(path) == "ronote-caddy.container" {
			if !slices.Equal(published, []string{"80:8080", "443:8443", "443:8443/udp"}) {
				t.Errorf("%s: PublishPort = %q, want 80 and 443 alone", path, published)
			}
			continue
		}
		if len(published) > 0 {
			t.Errorf("%s publishes %q; only Caddy may", path, published)
		}
	}
	for _, path := range glob(t, "quadlet/*.network") {
		u := readUnit(t, path)
		internal := slices.Equal(u["Network"]["Internal"], []string{"true"})
		if want := strings.Contains(path, "data") || strings.Contains(path, "converters"); internal != want {
			t.Errorf("%s: Internal = %v, want %v", path, internal, want)
		}
	}
}

// The units run the images the dev stack and the tests pin.
func TestImagesMatchCompose(t *testing.T) {
	t.Parallel()
	compose, err := os.ReadFile("compose.dev.yml")
	if err != nil {
		t.Fatal(err)
	}
	pinned := regexp.MustCompile(`(?m)^\s+image: (\S+)$`).FindAllStringSubmatch(string(compose), -1)
	images := map[string]bool{}
	for _, m := range pinned {
		images[m[1]] = true
	}
	for name, prefix := range map[string]string{
		"ronote-postgres.container":  "docker.io/",
		"ronote-seaweedfs.container": "docker.io/",
		"ronote-docling.container":   "",
		"ronote-pdftext.container":   "",
	} {
		image := readUnit(t, filepath.Join("quadlet", name)).container("Image")
		if len(image) != 1 || !images[strings.TrimPrefix(image[0], prefix)] {
			t.Errorf("%s: Image = %q, not an image compose.dev.yml pins", name, image)
		}
	}
	for _, path := range glob(t, "quadlet/*.container") {
		for _, image := range readUnit(t, path).container("Image") {
			if !strings.Contains(image, "@sha256:") && !strings.HasSuffix(image, ":@RONOTE_VERSION@") {
				t.Errorf("%s: Image %q is pinned neither by digest nor to the release", path, image)
			}
		}
	}
}

// The api and the worker load one configuration, so they need the same
// secrets.
func TestTheAPIAndTheWorkerGetTheSameSecrets(t *testing.T) {
	t.Parallel()
	api := readUnit(t, "quadlet/ronote-api.container").container("Secret")
	worker := readUnit(t, "quadlet/ronote-worker.container").container("Secret")
	if !slices.Equal(api, worker) || len(api) == 0 {
		t.Errorf("api secrets %q, worker secrets %q; want the same", api, worker)
	}
}

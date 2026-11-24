// Package deploy holds the production units, Caddy config and images
// (docs/adr/0014-production-topology.md); this test keeps the units to the
// ADR's rules.
package deploy

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/chat"
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

// ciUnits are CI's units and drop-ins, in deploy/quadlet/ci and its layers,
// ci-fakes and real, matching pattern, held to the same rules.
func ciUnits(pattern string) []string {
	var paths []string
	for _, dir := range []string{"ci", "ci-fakes", "real"} {
		more, _ := filepath.Glob(filepath.Join("quadlet", dir, pattern))
		paths = append(paths, more...)
	}
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
		"ronote-postgres.container":     "docker.io/",
		"ci/ronote-seaweedfs.container": "docker.io/",
		"ronote-docling.container":      "",
		"ronote-pdftext.container":      "",
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

// as-ronote.sh runs podman as ronote, which must read its own config even
// when sudo keeps the caller's HOME and XDG dirs, as GitHub's runner does.
func TestAsRonoteGivesRonoteItsOwnHome(t *testing.T) {
	t.Parallel()
	bin := t.TempDir()
	fakes := map[string]string{
		"id":     "echo 1001",
		"getent": "echo ronote:x:1001:1001::/home/ronote:/usr/sbin/nologin",
		// A sudo that keeps the environment: it drops -u ronote and runs the rest.
		"sudo": `shift 2; exec "$@"`,
	}
	for name, body := range fakes {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil { //nolint:gosec // a test's fake commands
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(t.Context(), "sh", "as-ronote.sh", "sh", "-c", `printf '%s %s' "$HOME" "${XDG_CONFIG_HOME-unset}"`)
	cmd.Env = []string{
		"PATH=" + bin + ":/usr/bin:/bin",
		"HOME=/home/runner",
		"XDG_CONFIG_HOME=/home/runner/.config",
	}
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, got)
	}
	if want := "/home/ronote unset"; string(got) != want {
		t.Errorf("as ronote, HOME and XDG_CONFIG_HOME are %q; want %q", got, want)
	}
}

// Production gives every unit fifteen minutes to start (an image pull and a
// health check on a small host); only CI's drop-in shortens it.
func TestProductionUnitsHaveFifteenMinutesToStart(t *testing.T) {
	t.Parallel()
	for _, path := range glob(t, "quadlet/*.container") {
		if got := readUnit(t, path)["Service"]["TimeoutStartSec"]; !slices.Equal(got, []string{"900"}) {
			t.Errorf("%s: TimeoutStartSec = %q, want 900", path, got)
		}
	}
}

// A saved image keeps no digest reference, so in CI a unit pinning a third-
// party image would pull it. Its drop-in names the local tag make
// prod-config gives it, from the unit's own pin, which stays.
func TestCIThirdPartyImagesAreTheUnitsOwn(t *testing.T) {
	t.Parallel()
	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^CI_THIRD_PARTY = (.*)$`).FindSubmatch(makefile)
	if m == nil {
		t.Fatal("no CI_THIRD_PARTY in the Makefile")
	}
	want := strings.Fields(string(m[1]))
	var got []string
	for _, path := range ciUnits("*.container.d/image.conf") {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(filepath.Dir(path)), "ronote-"), ".container.d")
		got = append(got, name)
		if image := readUnit(t, path).container("Image"); !slices.Equal(image, []string{"localhost/ronote-ci/" + name + ":pinned"}) {
			t.Errorf("%s: Image = %q, want localhost/ronote-ci/%s:pinned", path, image, name)
		}
		unit := filepath.Join("quadlet", "ronote-"+name+".container")
		if _, err := os.Stat(unit); err != nil {
			unit = filepath.Join("quadlet", "ci", "ronote-"+name+".container")
		}
		if pin := readUnit(t, unit).container("Image"); len(pin) != 1 || !strings.Contains(pin[0], "@sha256:") {
			t.Errorf("ronote-%s.container: Image = %q, want a digest pin", name, pin)
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("drop-ins give local names to %q, the Makefile tags %q", got, want)
	}
}

// Quadlet 5.7 drops the closing quote of a quoted HealthCmd when it writes
// the unit, so systemd hands podman a command with a quote left open, which
// sh then refuses ("unterminated quoted string"). A JSON array is not split
// that way, and a plain string needs no quote: podman runs it with sh -c.
func TestHealthChecksNeedNoQuotingQuadletMangles(t *testing.T) {
	t.Parallel()
	paths := append(glob(t, "quadlet/*.container"), ciUnits("*.container")...)
	paths = append(paths, ciUnits("*.d/*.conf")...)
	for _, path := range paths {
		for _, cmd := range readUnit(t, path).container("HealthCmd") {
			if strings.Contains(cmd, `"`) && !strings.HasPrefix(cmd, "[") {
				t.Errorf("%s: HealthCmd = %s; write it as a JSON array, or without double quotes", path, cmd)
			}
		}
	}
}

// The units' memory is ADR 0014's budget for the 12 GB host, which leaves
// it at least 2 GB with every long-running unit at its limit, and no unit
// asks for more than the host's 2 CPUs.
func TestUnitsFitTheHost(t *testing.T) {
	t.Parallel()
	budget := map[string]int{
		"ronote-caddy": 128, "ronote-api": 512, "ronote-worker": 512, "ronote-postgres": 2048,
		"ronote-ollama": 2048, "ronote-docling": 4096, "ronote-pdftext": 512,
	}
	limit := regexp.MustCompile(`--memory=(\d+)([mg]) --cpus=([\d.]+)`)
	total := 0
	for _, path := range glob(t, "quadlet/*.container") {
		u := readUnit(t, path)
		m := limit.FindStringSubmatch(strings.Join(u.container("PodmanArgs"), " "))
		if m == nil {
			t.Fatalf("%s: no --memory and --cpus in PodmanArgs", path)
		}
		mb, _ := strconv.Atoi(m[1])
		if m[2] == "g" {
			mb *= 1024
		}
		if cpus, _ := strconv.ParseFloat(m[3], 64); cpus > 2 {
			t.Errorf("%s: --cpus=%s, more than the host's 2", path, m[3])
		}
		if slices.Equal(u["Service"]["Type"], []string{"oneshot"}) {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(path), ".container")
		if want, ok := budget[name]; !ok || mb != want {
			t.Errorf("%s: --memory is %d MB, ADR 0014's budget is %d MB", path, mb, want)
		}
		total += mb
	}
	if total > 10*1024 {
		t.Errorf("the long-running units take %d MB, leaving the 12 GB host under 2 GB", total)
	}
}

// Production's blobs are in Oracle's Object Storage (ADR 0015): no
// production unit runs or waits for SeaweedFS, which only CI's drop-ins add.
func TestProductionRunsNoSeaweedFS(t *testing.T) {
	t.Parallel()
	for _, path := range append(glob(t, "quadlet/*.container"), glob(t, "quadlet/*.volume")...) {
		text, err := os.ReadFile(path) //nolint:gosec // the test's own files
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(text), "seaweedfs") || strings.Contains(string(text), "ronote-blobs") {
			t.Errorf("%s names SeaweedFS or its volume; only deploy/quadlet/ci may", path)
		}
	}
}

// The backups' images are pinned by digest, and CI runs each by the local
// tag make prod-config gives it (CI_BACKUP_IMAGES), as it does the units'.
func TestBackupImagesArePinned(t *testing.T) {
	t.Parallel()
	read := func(path string) map[string]string {
		data, err := os.ReadFile(path) //nolint:gosec // the test's own files
		if err != nil {
			t.Fatal(err)
		}
		images := map[string]string{}
		for line := range strings.Lines(string(data)) {
			if name, ref, ok := strings.Cut(strings.TrimSpace(line), "="); ok && !strings.HasPrefix(name, "#") {
				images[name] = ref
			}
		}
		return images
	}
	pinned, ci := read("backup/images"), read("quadlet/ci/config/backup.images")
	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^CI_BACKUP_IMAGES = (.*)$`).FindSubmatch(makefile)
	if m == nil {
		t.Fatal("no CI_BACKUP_IMAGES in the Makefile")
	}
	names := strings.Fields(string(m[1]))
	if len(names) != len(pinned) || len(ci) != len(pinned) {
		t.Errorf("backup/images names %d images, CI's %d, the Makefile %q", len(pinned), len(ci), names)
	}
	for _, name := range names {
		if !strings.Contains(pinned[name], "@sha256:") {
			t.Errorf("backup/images: %s = %q, want a digest pin", name, pinned[name])
		}
		if want := "localhost/ronote-ci/" + name + ":pinned"; ci[name] != want {
			t.Errorf("CI's backup.images: %s = %q, want %s", name, ci[name], want)
		}
	}
}

// The watcher alerts on the line the api logs when chat reaches its daily
// spend cap; the two must name it alike.
func TestTheWatcherMatchesTheSpendCapLine(t *testing.T) {
	t.Parallel()
	watch, err := os.ReadFile("alerts/watch.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(watch), "grep -q '"+chat.SpendCapReached+"'") {
		t.Errorf("alerts/watch.sh doesn't match %q, which the api logs", chat.SpendCapReached)
	}
}

// The real images' layer must outrank the shared drop-in: Quadlet applies
// drop-ins by file name, whichever directory they are in, so a file that
// sorts before ronote-.container.d/start.conf loses its Pull and its start
// time to start.conf's (make units-check finds that in Quadlet's own output).
func TestTheRealLayerSortsAfterTheSharedDropIn(t *testing.T) {
	t.Parallel()
	paths := glob(t, "quadlet/real/ronote-*.container.d/*.conf")
	if len(paths) != 3 {
		t.Errorf("the real layer has %d drop-ins, want ollama's, docling's and pdftext's", len(paths))
	}
	for _, path := range paths {
		if name := filepath.Base(path); name <= "start.conf" {
			t.Errorf("%s sorts before start.conf, which would win", path)
		}
		u := readUnit(t, path)
		if got := u.container("Pull"); !slices.Equal(got, []string{"missing"}) {
			t.Errorf("%s: Pull = %q, want missing", path, got)
		}
		if got := u["Service"]["TimeoutStartSec"]; !slices.Equal(got, []string{"1500"}) {
			t.Errorf("%s: TimeoutStartSec = %q, want 1500", path, got)
		}
	}
}

// Ollama embeds with one model, one request at a time, and keeps it loaded:
// its memory is in the budget (ADR 0014), and a cold load would make the
// first search after an idle spell wait. The context fits the largest
// embedding input (search's maxEmbedChars, under 2048 tokens) and the query
// prefix, rounded up to a power of two: 4096, a KV cache of 448 MiB.
func TestOllamaIsPinnedToOneModelAndItsContext(t *testing.T) {
	t.Parallel()
	env := map[string]string{}
	for _, line := range readUnit(t, "quadlet/ronote-ollama.container").container("Environment") {
		for _, kv := range strings.Fields(line) {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
	}
	want := map[string]string{
		"OLLAMA_CONTEXT_LENGTH":    "4096",
		"OLLAMA_NUM_PARALLEL":      "1",
		"OLLAMA_MAX_LOADED_MODELS": "1",
		"OLLAMA_KEEP_ALIVE":        "-1",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("ronote-ollama.container: %s = %q, want %q", k, env[k], v)
		}
	}
}

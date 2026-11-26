#!/bin/sh
# What the real docling and Ollama images need on this host
# (docs/adr/0015-operations.md, .github/workflows/arm-real.yml). Both
# subcommands write Markdown for the job summary to stdout, the reason for a
# failure to stderr, and exit 1 on a failure.
#
#   sh scripts/real-images.sh arm64     each pinned image is a multi-arch index
#                                       listing linux/amd64 and linux/arm64
#   sudo sh scripts/real-images.sh sample FILE [SECONDS]
#                                       appends each of ronote's containers'
#                                       memory to FILE every SECONDS (1),
#                                       until killed; with 0, once
#   sudo sh scripts/real-images.sh memory FILE
#                                       each container's peak memory, split
#                                       into anon and file, against its
#                                       limit, from FILE's samples, then
#                                       podman system df; stops the sampler
#                                       in FILE.pid. A container the kernel
#                                       killed for memory, or a unit that
#                                       restarted, fails it
#   sudo sh scripts/real-images.sh ollama
#                                       the settings both Ollamas logged as
#                                       they started and loaded the model
#   sudo sh scripts/real-images.sh curve PAGES FILE [PAGES FILE...]
#                                       docling's time, anon and swap for
#                                       each PDF, converted once; a timeout
#                                       skips the larger, another failure
#                                       fails it
set -u

here=$(cd "$(dirname "$0")" && pwd)
failed=false

say() {
	printf '%s\n' "$*"
}

fail() {
	failed=true
	printf 'real-images: %s\n' "$*" >&2
}

# as_user runs a command as ronote, with its own systemd and podman; a test
# stands in for it with RONOTE_AS_RONOTE.
as_user() {
	sh "${RONOTE_AS_RONOTE:-$here/../deploy/as-ronote.sh}" "$@"
}

# The images the units and the backups pin by digest.
pinned() {
	{
		sed -n 's/^Image=//p' "$here"/../deploy/quadlet/*.container "$here"/../deploy/quadlet/ci/*.container
		sed -n 's/^[a-z]*=//p' "$here/../deploy/backup/images"
	} | grep '@sha256:' | sort -u
}

# by_digest REF: REF without its tag. podman refuses a reference with both a
# tag and a digest ("currently not supported"), and the digest is what a unit
# runs by.
by_digest() {
	name=${1%%@*}
	case ${name##*/} in
	*:*) name=${name%:*} ;;
	esac
	printf '%s@%s' "$name" "${1#*@}"
}

# arm64: each pin is the multi-arch index, listing linux/amd64 and
# linux/arm64. A pin that is one architecture's manifest would run that
# image on the other, and one podman can't read is no verdict on its builds,
# so each says what it is.
arm64() {
	say "### Pinned images: multi-arch indexes with linux/arm64"
	say ""
	say "| image | index | linux/amd64 | linux/arm64 |"
	say "|---|---|---|---|"
	errors=$(mktemp)
	trap 'rm -f "$errors"' EXIT
	broken=false
	refs=$(pinned)
	for ref in $refs; do
		if ! manifest=$(podman manifest inspect "$(by_digest "$ref")" 2>"$errors"); then
			say "| \`$ref\` | **error** | - | - |"
			fail "can't inspect $ref: $(cat "$errors")"
			continue
		fi
		flat=$(printf '%s' "$manifest" | tr -d ' \t\r\n')
		case $flat in
		*'"manifests":['*) ;;
		*)
			say "| \`$ref\` | **single** | - | - |"
			fail "$ref is a single-arch manifest, not a multi-arch index: production on arm64 would pull the wrong image"
			broken=true
			continue
			;;
		esac
		platforms=$(printf '%s' "$flat" | grep -o '"platform":{[^}]*}' | grep '"os":"linux"')
		row="| \`$ref\` | yes |"
		for arch in amd64 arm64; do
			if printf '%s\n' "$platforms" | grep -q "\"architecture\":\"$arch\""; then
				row="$row yes |"
			else
				row="$row **no** |"
				fail "no linux/$arch entry in the index of $ref"
				broken=true
			fi
		done
		say "$row"
	done
	if [ "$broken" = true ]; then
		say ""
		say "A pin that is not an index listing both platforms can't run on both the amd64 hosts ADR 0015 allows and Oracle's arm64 A1. Options: pin the image's index digest (the one of its tag); pick a release that has both builds; or build it for the missing architecture and pin that."
	fi
}

mib() {
	echo $(($1 / 1048576))
}

# value FILE KEY: KEY's number in a cgroup file of KEY VALUE lines; 0 if none.
value() {
	awk -v k="$2" '$1 == k { v = $2 } END { print v + 0 }' "$1" 2>/dev/null || echo 0
}

# bytes FILE: a cgroup file's one number; 0 for none or "max".
bytes() {
	read -r n <"$1" 2>/dev/null || n=0
	case $n in '' | *[!0-9]*) n=0 ;; esac
	echo "$n"
}

# record KEY DIR WEIGHED: one sample line, KEY PEAK ANON FILE LIMIT
# OOM-KILLS SWAP CPU-WEIGHT, from the cgroup DIR; swap is its peak, or what
# it holds now where the kernel keeps no peak; the CPU weight is WEIGHED's,
# the cgroup that competes for the CPUs with its siblings.
record() {
	peak=$(bytes "$2/memory.peak")
	[ "$peak" -gt 0 ] || peak=$(bytes "$2/memory.current")
	swap=$(bytes "$2/memory.swap.peak")
	now=$(bytes "$2/memory.swap.current")
	[ "$now" -le "$swap" ] || swap=$now
	printf '%s %s %s %s %s %s %s %s\n' "$1" "$peak" "$(value "$2/memory.stat" anon)" "$(value "$2/memory.stat" file)" \
		"$(bytes "$2/memory.max")" "$(value "$2/memory.events" oom_kill)" "$swap" "$(bytes "$3/cpu.weight")"
}

# sample_once FILE: each unit's container, by its own cgroup under its
# service's (Quadlet's libpod-payload-*, where its limit is: the service's
# also holds podman's own start), with the service's CPU weight, and each
# container a script ran (the backups and the drill, in scopes of their own
# in ronote's user.slice), with that slice's, named once by its container's
# name if that is ronote's, else by its image.
sample_once() {
	out=$1
	base=$(user_cgroup)
	[ -d "$base" ] || return 0
	find "$base" -type d -name 'ronote-*.service' | while read -r dir; do
		for payload in "$dir"/libpod-payload-*; do
			[ ! -d "$payload" ] || record "$(basename "$dir" .service)" "$payload" "$dir" >>"$out"
		done
	done
	find "$base" -type d -name 'libpod-*.scope' | while read -r dir; do
		id=$(basename "$dir" .scope)
		id=${id#libpod-}
		case $id in conmon-*) continue ;; esac
		key=$(awk -v id="$id" '$1 == id { print $2 }' "$out.names" 2>/dev/null)
		if [ -z "$key" ]; then
			info=$(as_user podman inspect --format '{{.Name}} {{.ImageName}}' "$id" 2>/dev/null) || continue
			case $info in
			ronote-*) key=${info%% *} ;;
			*) key=${info#* }; key=${key%%@*}; key=${key##*/}; key=${key%%:*} ;;
			esac
			printf '%s %s\n' "$id" "$key" >>"$out.names"
		fi
		record "$key" "$dir" "$(dirname "$dir")" >>"$out"
	done
}

sample() {
	file=${1:?the samples file}
	every=${2:-1}
	while :; do
		sample_once "$file"
		[ "$every" -gt 0 ] || return 0
		sleep "$every"
	done
}

# threads NAME: the CPU threads container NAME computes on, as it says: each
# Ollama's llama-server logs them as it loads the model, and docling's torch
# is asked; "-" for the rest, or when it can't tell.
threads() {
	n=
	case $1 in
	ronote-ollama | ronote-ollama-query)
		n=$(as_user journalctl --user -b --no-pager -o cat -u "$1.service" 2>/dev/null |
			sed -n 's/.*system_info: n_threads = \([0-9]*\).*/\1/p' | tail -n 1)
		;;
	ronote-docling)
		n=$(as_user podman exec ronote-docling python -c 'import torch; print(torch.get_num_threads())' 2>/dev/null)
		;;
	esac
	echo "${n:--}"
}

memory() {
	file=${1:?the samples file}
	if [ -f "$file.pid" ]; then
		kill "$(cat "$file.pid")" 2>/dev/null || true
		rm -f "$file.pid"
	fi
	say "### Peak memory of each container"
	say ""
	say "ronote's units ran on CPUs $(cat "$(dirname "$(user_cgroup)")/cpuset.cpus.effective" 2>/dev/null || echo unknown) (its slice's cpuset.cpus.effective)."
	say ""
	say "Peak is the kernel's high-water mark for the container's own cgroup; anon (what it allocated) and file (page cache, such as model files read) are the largest of the samples taken every second, as the kernel keeps no split of the peak; swap is the most it had swapped out. CPU weight is cpu.weight of the cgroup that competes for the CPUs: a unit's service (the container's parent), or ronote's user.slice for a script's container. Threads are what each Ollama's llama-server last logged (system_info's n_threads), and torch's in docling."
	say ""
	say "| container | peak | anon | file | swap | limit | peak of limit | anon of limit | CPU weight | threads | state |"
	say "|---|---|---|---|---|---|---|---|---|---|---|"
	[ -s "$file" ] || fail "no samples in $file: was the sampler running?"
	rows=$(awk '
		{ k = $1; seen[k] = 1
		  if ($2 > peak[k]) peak[k] = $2; if ($3 > anon[k]) anon[k] = $3; if ($4 > file[k]) file[k] = $4
		  if ($5 > limit[k]) limit[k] = $5; if ($6 > oom[k]) oom[k] = $6; if ($7 > swap[k]) swap[k] = $7; weight[k] = $8 }
		END { for (k in seen) print k, peak[k] + 0, anon[k] + 0, file[k] + 0, limit[k] + 0, oom[k] + 0, swap[k] + 0, weight[k] + 0 }
	' "$file" 2>/dev/null | sort)
	units=$(as_user systemctl --user list-units --all --plain --no-legend 'ronote-*.service' | awk '{ print $1 }')
	printf '%s\n' "$rows" | {
		while read -r name peak anon file limit oom swap weight; do
			[ -n "$name" ] || continue
			case $name in *-network | *-volume) continue ;; esac
			restarts=0
			if printf '%s\n' "$units" | grep -qx "$name.service"; then
				restarts=$(as_user systemctl --user show -p NRestarts --value "$name.service")
			fi
			state=ok
			of_limit=-
			anon_of_limit=-
			if [ "$limit" -gt 0 ]; then
				of_limit="$((peak * 100 / limit))%"
				anon_of_limit="$((anon * 100 / limit))%"
			fi
			if [ "$oom" -gt 0 ]; then
				state="**OOM-killed**"
				fail "$name was killed for memory at its $(mib "$limit") MiB limit"
			elif [ "${restarts:-0}" -gt 0 ]; then
				state="**restarted $restarts times**"
				fail "$name restarted $restarts times: it did not stay within its $(mib "$limit") MiB limit, or failed some other way (see its journal)"
			elif [ "$swap" -gt 0 ]; then
				state="**swapped**"
			elif [ "$limit" -gt 0 ] && [ $((anon * 100 / limit)) -ge 80 ]; then
				state="anon near its limit"
			fi
			say "| $name | $(mib "$peak") MiB | $(mib "$anon") MiB | $(mib "$file") MiB | $(mib "$swap") MiB | $(mib "$limit") MiB | $of_limit | $anon_of_limit | $weight | $(threads "$name") | $state |"
		done
		[ "$failed" = false ]
	} || failed=true
	if [ "$failed" = true ]; then
		say ""
		say "Docling is the unit to doubt: it is given 4 GiB of the 12 GB host (ADR 0014), the most of any. Options, none applied here: raise its limit to 5-6 GiB, paid for from the host's 2.3 GB of headroom; or cap what it converts at once (\`DOCLING_SERVE_MAX_NUM_PAGES\`, fewer workers). Always Free's A1 allowance is 2 OCPUs and 12 GB, all of it this host's already: more than that is a paid shape."
	fi
	say ""
	say "### ronote's images, containers and volumes"
	say ""
	say '```'
	as_user podman system df
	say '```'
}

# docling's own document timeout (its unit's DOCLING_SERVE_MAX_DOCUMENT_TIMEOUT).
docling_timeout=540

# user_cgroup: ronote's systemd user manager's cgroup, where its units are.
user_cgroup() {
	uid=$(id -u ronote 2>/dev/null)
	echo "${RONOTE_USER_CGROUP:-/sys/fs/cgroup/user.slice/user-$uid.slice/user@$uid.service}"
}

# curve PAGES FILE...: each PDF converted once, smallest first, on the one
# running docling, by the worker's own client in its container with no job
# and so no retries (worker convert-check), timed against docling's
# document timeout, with the most docling's own cgroup held in anon and
# swap, sampled every second meanwhile. A timeout is a result: the larger
# are skipped, as they would only run into it too.
curve() {
	say "### Docling by page count"
	say ""
	say "Each PDF converted once, smallest first, by the worker's own client with no job and no retries (\`worker convert-check\`), on the one docling, against its ${docling_timeout} s document timeout; anon and swap are the most docling's container held, sampled every second while it converted."
	say ""
	say "| pages | time | of ${docling_timeout} s | anon | of limit | swap | outcome |"
	say "|---|---|---|---|---|---|---|"
	dir=$(find "$(user_cgroup)" -type d -path '*/ronote-docling.service/libpod-payload-*' 2>/dev/null | head -n 1)
	if [ -z "$dir" ]; then
		fail "no docling container to measure under $(user_cgroup)"
		return
	fi
	limit=$(bytes "$dir/memory.max")
	held=$(mktemp)
	skipped=
	while [ $# -ge 2 ]; do
		pages=$1
		pdf=$2
		shift 2
		if [ -n "$skipped" ]; then
			say "| $pages | - | - | - | - | - | skipped: $skipped pages ran into the timeout |"
			continue
		fi
		: >"$held"
		(
			while :; do
				printf '%s %s\n' "$(value "$dir/memory.stat" anon)" "$(bytes "$dir/memory.swap.current")" >>"$held"
				sleep 1
			done
		) &
		watcher=$!
		started=$(date +%s)
		out=$(as_user podman exec -i ronote-worker /ronote convert-check <"$pdf" 2>&1)
		took=$(($(date +%s) - started))
		kill "$watcher" 2>/dev/null
		wait "$watcher" 2>/dev/null
		printf '%s %s\n' "$(value "$dir/memory.stat" anon)" "$(bytes "$dir/memory.swap.current")" >>"$held"
		anon=$(awk '$1 > m { m = $1 } END { print m + 0 }' "$held")
		swap=$(awk '$2 > m { m = $2 } END { print m + 0 }' "$held")
		case $out in
		*"convert-check: ok"*) outcome=ok ;;
		*pdf_conversion_timeout*)
			outcome="**timed out**"
			skipped=$pages
			;;
		*)
			outcome="**failed**"
			fail "docling failed on $pages pages: $out"
			;;
		esac
		of_limit=-
		[ "$limit" -eq 0 ] || of_limit="$((anon * 100 / limit))%"
		say "| $pages | $took s | $((took * 100 / docling_timeout))% | $(mib "$anon") MiB | $of_limit | $(mib "$swap") MiB | $outcome |"
	done
	rm -f "$held"
}

# ollama: what Ollama logged as it started (its settings) and loaded the
# model (its context, its parallel sequences, its caches).
ollama() {
	for unit in ronote-ollama ronote-ollama-query; do
		say "### $unit's settings, as it logged them"
		say ""
		say '```'
		as_user journalctl --user -b --no-pager -o cat -u "$unit.service" |
			grep -E 'server config|OLLAMA_|system_info|n_threads|ctx|parallel|n_seq|kv|KV|keep|loaded|offload|memory|starting runner|load_tensors|context|level=(WARN|ERROR)' |
			cut -c1-1000 | head -n 80
		say '```'
		say ""
	done
}

case ${1:-} in
arm64) arm64 ;;
sample) sample "${2:-}" "${3:-1}" ;;
memory) memory "${2:-}" ;;
ollama) ollama ;;
curve)
	shift
	curve "$@"
	;;
*)
	printf 'usage: sh scripts/real-images.sh arm64|sample FILE [SECONDS]|memory FILE|ollama|curve PAGES FILE...\n' >&2
	exit 2
	;;
esac
[ "$failed" = false ]

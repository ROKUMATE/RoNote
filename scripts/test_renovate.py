"""Checks renovate.json (docs/adr/0015-operations.md): its policy, and that
every commit message it would write passes the commit check in strict mode
(docs/adr/0003-commit-convention.md).

It finds every dependency as Renovate's managers would here: Go modules,
npm packages, actions, the workflows' runners, Node's version, the images
of the Containerfiles, compose and the workflows, and those of the config's
own regex manager. Each gets the message Renovate would write: its group's,
by the config's packageRules, or its own. It runs each through
.githooks/commit-msg with COMMIT_MSG_STRICT=1, as scripts/check-commits.sh
does. It renders the variables the config uses
with the dependency's current version, so a new version longer than that
is the one case it can't see; Renovate's first pull request, checked by CI,
is the proof of its real output. make hooks-test runs it:

    python3 scripts/test_renovate.py
"""

import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
CONFIG = json.loads((REPO / "renovate.json").read_text())


def js_regex(pattern: str) -> str:
    """A JavaScript regex as Python's re reads it: named groups renamed."""
    return pattern.replace("(?<", "(?P<")


def files(pattern: str) -> list[Path]:
    """The repo's tracked files a managerFilePatterns entry ("/re/") matches."""
    tracked = subprocess.run(["git", "ls-files"], cwd=REPO, capture_output=True, text=True, check=True)
    compiled = re.compile(js_regex(pattern.strip("/")))
    return [REPO / f for f in tracked.stdout.split() if compiled.search(f)]


class Dep:
    """A dependency as a packageRule sees it."""

    def __init__(self, manager: str, datasource: str, dep_type: str, name: str, version: str):
        self.manager, self.datasource, self.dep_type, self.name, self.version = manager, datasource, dep_type, name, version


def regex_manager_deps() -> list[Dep]:
    deps = []
    for manager in CONFIG["customManagers"]:
        for pattern in manager["managerFilePatterns"]:
            for path in files(pattern):
                for match in manager["matchStrings"]:
                    for m in re.finditer(js_regex(match), path.read_text()):
                        deps.append(Dep("custom.regex", manager["datasourceTemplate"], "", m["depName"], m["currentValue"]))
    return deps


IMAGE = re.compile(r"([a-z0-9][a-z0-9.-]*(?:/[a-z0-9._-]+)+):([A-Za-z0-9][A-Za-z0-9._-]*)(?:@sha256:[a-f0-9]{64})?")


WORKFLOWS = r"/^\.github/workflows/.+\.ya?ml$/"


def image_deps() -> list[Dep]:
    """Images in Containerfiles (FROM), compose and the workflows (image:)."""
    deps = []
    for manager, pattern in (("dockerfile", r"/(^|/)Containerfile[^/]*$/"), ("docker-compose", r"/compose[^/]*\.ya?ml$/"), ("github-actions", WORKFLOWS)):
        for path in files(pattern):
            for line in path.read_text().splitlines():
                m = IMAGE.search(line) if re.match(r"\s*(FROM|image:)\s", line) else None
                if m:
                    deps.append(Dep(manager, "docker", "", m[1], m[2]))
    return deps


def action_deps() -> list[Dep]:
    deps = []
    for path in files(WORKFLOWS):
        text = path.read_text()
        for m in re.finditer(r"uses:\s*([\w.-]+/[\w./-]+)@[0-9a-f]{40}\s*#\s*(\S+)", text):
            deps.append(Dep("github-actions", "github-tags", "action", m[1], m[2]))
        for m in re.finditer(r"(ubuntu)-(\d+\.\d+)(?:-arm)?", text):
            deps.append(Dep("github-actions", "github-runners", "github-runner", m[1], m[2]))
    return deps


def go_deps() -> list[Dep]:
    text = (REPO / "go.mod").read_text()
    found = re.findall(r"^\s+([\w./-]+)\s+(v\S+)", text, re.M) + re.findall(r"^require\s+([\w./-]+)\s+(v\S+)", text, re.M)
    return [Dep("gomod", "go", "require", name, version) for name, version in found]


def npm_deps() -> list[Dep]:
    deps = []
    for path in files(r"/(^|/)package\.json$/"):
        data = json.loads(path.read_text())
        for kind in ("dependencies", "devDependencies"):
            deps += [Dep("npm", "npm", kind, name, version) for name, version in data.get(kind, {}).items()]
    return deps


def node_deps() -> list[Dep]:
    return [Dep("nvm", "node-version", "", "node", (REPO / ".nvmrc").read_text().strip())]


def all_deps() -> list[Dep]:
    return regex_manager_deps() + image_deps() + action_deps() + go_deps() + npm_deps() + node_deps()


def group_of(dep: Dep) -> str | None:
    """The group a dependency's update goes in: the last packageRule that
    matches it and names one wins, as in Renovate."""
    group = None
    for rule in CONFIG["packageRules"]:
        if "groupName" not in rule:
            continue
        if all(
            value in rule.get(key, [value])
            for key, value in (("matchManagers", dep.manager), ("matchDatasources", dep.datasource), ("matchDepTypes", dep.dep_type))
        ):
            group = rule["groupName"]
    return group


def render(template: str, values: dict[str, str]) -> str:
    out = template
    for name, value in values.items():
        out = out.replace("{{{" + name + "}}}", value).replace("{{" + name + "}}", value)
    return out


def message(topic: str, extra: str, values: dict[str, str]) -> str:
    parts = [CONFIG["commitMessagePrefix"], CONFIG["commitMessageAction"], render(topic, values), render(extra, values), CONFIG["commitMessageSuffix"]]
    return " ".join(p for p in parts if p)


def group_names() -> list[str]:
    return [rule["groupName"] for rule in CONFIG["packageRules"] if "groupName" in rule]


def strict_check(msg: str) -> tuple[int, str]:
    """The commit hook's verdict in strict mode, and the message as left."""
    with tempfile.NamedTemporaryFile("w", suffix=".msg", delete=False) as f:
        f.write(msg + "\n")
    try:
        result = subprocess.run(
            ["sh", str(REPO / ".githooks" / "commit-msg"), f.name],
            env={**os.environ, "COMMIT_MSG_STRICT": "1"},
            capture_output=True,
            text=True,
        )
        return result.returncode, Path(f.name).read_text()
    finally:
        os.unlink(f.name)


class RenovateConfig(unittest.TestCase):
    def test_policy(self):
        self.assertIs(CONFIG["automerge"], False)
        self.assertIs(CONFIG["pinDigests"], True)
        self.assertEqual(CONFIG["minimumReleaseAge"], "14 days")
        self.assertEqual(CONFIG["schedule"], ["before 6am on monday"])
        # Dependabot's security alerts stay on, in the repo's settings;
        # Renovate opens no security pull requests of its own.
        self.assertEqual(CONFIG["vulnerabilityAlerts"], {"enabled": False})
        self.assertEqual(CONFIG["semanticCommits"], "disabled")
        self.assertIsNone(CONFIG["commitBody"])

    def test_a_few_pull_requests_at_a_time(self):
        self.assertLessEqual(CONFIG["prConcurrentLimit"], 3)
        self.assertEqual(CONFIG["lockFileMaintenance"], {"enabled": False})

    def test_every_update_type_keeps_the_form(self):
        # Renovate's defaults say Pin, Roll back or Replace for these.
        for kind in ("pin", "pinDigest", "rollback", "replacement"):
            self.assertEqual(CONFIG[kind]["commitMessageAction"], "Bump", kind)
        for kind in ("pinDigest", "digest"):
            self.assertEqual((CONFIG[kind]["commitMessageTopic"], CONFIG[kind]["commitMessageExtra"]), ("{{depName}}", "to {{newVersion}}"), kind)
        # Pin updates go in Renovate's own "Pin Dependencies" group, whose
        # topic is "dependencies".
        status, _ = strict_check(message(CONFIG["group"]["commitMessageTopic"], CONFIG["group"]["commitMessageExtra"], {"groupName": "dependencies"}))
        self.assertEqual(status, 0)

    def test_the_models_are_never_bumped(self):
        # A new embedding model invalidates every vector and the eval's
        # baseline; the chat model is chosen by hand. No manager reads them.
        names = {dep.name for dep in all_deps()}
        self.assertFalse({n for n in names if "qwen3-embedding" in n or n.startswith("claude-")}, names)

    def test_postgres_keeps_its_major(self):
        rule = next(r for r in CONFIG["packageRules"] if "pgvector/pgvector" in r.get("matchPackageNames", []))
        allowed = re.compile(rule["allowedVersions"].strip("/"))
        tags = {dep.version for dep in all_deps() if dep.name.endswith("pgvector/pgvector")}
        self.assertTrue(tags)
        for tag in tags:
            self.assertRegex(tag, allowed, "the pinned tag is within the allowed versions")
        self.assertIsNone(allowed.search("0.9.0-pg19-trixie"), "a Postgres major is not")

    def test_what_to_run_again_is_said(self):
        notes = {name: " ".join(r.get("prBodyNotes", [])) for r in CONFIG["packageRules"] for name in r.get("matchPackageNames", [])}
        self.assertIn("make pdf-record", notes["quay.io/docling-project/docling-serve-cpu"])
        self.assertIn("arm-real.yml", notes["docker.io/ollama/ollama"])
        self.assertIn("make web-e2e", notes["pyodide"])
        self.assertIn("make river-migration", notes["/^github\\.com/riverqueue/river/"])

    def test_every_image_go_module_npm_package_and_action_is_grouped(self):
        for dep in all_deps():
            if dep.datasource in ("docker", "go", "npm") or dep.dep_type == "action":
                self.assertIsNotNone(group_of(dep), f"{dep.manager} {dep.name}")

    def test_every_image_is_in_one_group(self):
        images = [r for r in CONFIG["packageRules"] if r.get("matchDatasources") == ["docker"]]
        self.assertEqual(len(images), 1, "images in one pull request: TestImagesMatchCompose")

    def test_the_regex_manager_finds_every_pinned_image(self):
        found = {dep.name for dep in regex_manager_deps()}
        for want in ("docker.io/ollama/ollama", "quay.io/docling-project/docling-serve-cpu", "docker.io/restic/restic", "pgvector/pgvector", "chrislusf/seaweedfs"):
            self.assertIn(want, found)
        self.assertFalse(any(name.startswith(("ghcr.io/rokumate/", "localhost/")) for name in found), found)

    def test_every_message_passes_the_commit_check(self):
        deps = all_deps()
        self.assertGreater(len(deps), 30, "the managers found too little to be checking anything")
        group = CONFIG["group"]
        messages = set()
        for dep in deps:
            name = group_of(dep)
            if name:
                messages.add(message(group["commitMessageTopic"], group["commitMessageExtra"], {"groupName": name}))
            else:
                messages.add(message(CONFIG["commitMessageTopic"], CONFIG["commitMessageExtra"], {"depName": dep.name, "newVersion": dep.version}))
        self.assertEqual(
            {m for m in messages if m.startswith("Build: Bump") and " to " not in m},
            {message(group["commitMessageTopic"], group["commitMessageExtra"], {"groupName": n}) for n in group_names()},
            "every group has a pull request here",
        )
        for msg in sorted(messages, key=len, reverse=True):
            with self.subTest(msg=msg):
                status, left = strict_check(msg)
                self.assertEqual(status, 0, f"the commit check refuses {msg!r} ({len(msg)} characters)")
                self.assertEqual(left, msg + "\n", "strict mode never rewrites")

    def test_the_forms_are_as_the_convention_says(self):
        self.assertEqual(
            message(CONFIG["commitMessageTopic"], CONFIG["commitMessageExtra"], {"depName": "quay.io/docling-project/docling-serve-cpu", "newVersion": "v1.39.0"}),
            "Build: Bump quay.io/docling-project/docling-serve-cpu to v1.39.0",
        )
        self.assertEqual(message(CONFIG["group"]["commitMessageTopic"], CONFIG["group"]["commitMessageExtra"], {"groupName": "container images"}), "Build: Bump container images")


if __name__ == "__main__":
    unittest.main()

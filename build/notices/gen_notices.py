#!/usr/bin/env python3
"""Regenerate THIRD_PARTY_NOTICES.txt from the Go modules compiled into the
app (Windows, macOS and Linux builds) and the npm packages bundled into the
frontend. Run from the repository root:  python3 build/notices/gen_notices.py
"""
import json, os, subprocess, sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
LICENSE_NAMES = ["LICENSE", "LICENSE.md", "LICENSE.txt", "LICENCE", "LICENCE.txt", "COPYING", "LICENSE-MIT"]
NOTICE_NAMES = ["NOTICE", "NOTICE.md", "NOTICE.txt"]


def read_files(d, names):
    """Read files whose names match (case-insensitively) one of names."""
    wanted = [n.lower() for n in names]
    out = []
    try:
        entries = sorted(os.listdir(d), key=lambda e: wanted.index(e.lower()) if e.lower() in wanted else 99)
    except OSError:
        return out
    for e in entries:
        if e.lower() in wanted and os.path.isfile(os.path.join(d, e)):
            with open(os.path.join(d, e), encoding="utf-8", errors="replace") as f:
                out.append(f.read().strip())
    return out


def go_modules():
    mods = {}
    for goos in ("windows", "darwin", "linux"):
        env = dict(os.environ, GOOS=goos, GOARCH="arm64" if goos == "darwin" else "amd64", CGO_ENABLED="1")
        res = subprocess.run(
            ["go", "list", "-deps", "-tags", "production", "-f",
             "{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}", "."],
            cwd=ROOT, env=env, capture_output=True, text=True, check=True)
        for line in res.stdout.splitlines():
            if line.strip():
                path, ver, d = line.split("\t")
                mods[path] = (ver, d)
    return mods


def npm_packages():
    res = subprocess.run(["npm", "ls", "--omit=dev", "--all", "--json", "--long"],
                         cwd=os.path.join(ROOT, "frontend"), capture_output=True, text=True)
    data = json.loads(res.stdout)
    pkgs = {}

    def walk(deps):
        for name, info in (deps or {}).items():
            if info.get("path") and name not in pkgs:
                pkgs[name] = (info.get("version", ""), info["path"])
            walk(info.get("dependencies"))

    walk(data.get("dependencies"))
    return pkgs


def section(name, ver, d, kind):
    lic = read_files(d, LICENSE_NAMES)
    if not lic:
        # some packages only declare the license in package.json
        pj = os.path.join(d, "package.json")
        if os.path.isfile(pj):
            with open(pj, encoding="utf-8") as f:
                lic = ["License: " + str(json.load(f).get("license", "unknown")) + " (no license file shipped)"]
    if not lic:
        print(f"warning: no license text for {name}", file=sys.stderr)
        lic = ["(license file not found)"]
    body = "\n\n".join(lic + read_files(d, NOTICE_NAMES))
    return f"{'=' * 78}\n{name} {ver} ({kind})\n{'=' * 78}\n\n{body}\n\n"


def main():
    out = [
        "Pinion — third-party notices\n\n"
        "Pinion is distributed under the MIT License (see LICENSE). It includes the\n"
        "following third-party software, each under its own license reproduced below.\n\n"
    ]
    for name, (ver, d) in sorted(go_modules().items()):
        out.append(section(name, ver, d, "Go module"))
    for name, (ver, d) in sorted(npm_packages().items()):
        out.append(section(name, ver, d, "npm package"))
    with open(os.path.join(ROOT, "THIRD_PARTY_NOTICES.txt"), "w", encoding="utf-8") as f:
        f.write("".join(out))


if __name__ == "__main__":
    main()

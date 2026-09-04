#!/usr/bin/env python3
"""Build and publish an easy-qfnu date-tagged GitHub Release."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

TARGETS = (
    ("linux", "amd64", ""),
    ("linux", "arm64", ""),
    ("darwin", "amd64", ""),
    ("darwin", "arm64", ""),
    ("windows", "amd64", ".exe"),
)
GO_TOOLCHAIN = "go1.27.1"
MIN_GO_VERSION = (1, 27, 0)
GARBLE_VERSION = "v0.17.0"
GO_VERSION_RE = re.compile(r"(?<![A-Za-z])go(?P<version>[0-9]+(?:[.][0-9]+){1,2})(?![0-9])")
VERSION_RE = re.compile(r"^v[0-9]{4}[.][0-9]{2}[.][0-9]{2}[.][0-9]{2}$")
MODULE_RE = re.compile(r"^module[ \t]+([^ \t\r\n]+)$", re.MULTILINE)
COMMIT_RE = re.compile(r"^(?P<type>[a-z]+)(?:[(](?P<scope>[^)]*)[)])?(?:!)?:[ \t]*(?P<subject>.+)$", re.IGNORECASE)
# Keep recognizing legacy HHmm tags while migrating historical Releases.
DATE_TAG_RE = re.compile(r"^v[0-9]{4}[.][0-9]{2}[.][0-9]{2}[.](?:[0-9]{2}|[0-9]{4})$")

FEATURE_TYPES = {"feat", "feature"}
FIX_TYPES = {"fix", "bugfix", "perf"}


class ReleaseError(RuntimeError):
    """A release precondition or publication step failed."""


def default_cli_repo() -> Path:
    """Locate the Go repository that contains this local skill."""
    for parent in Path(__file__).resolve().parents:
        if (parent / ".git").exists() and (parent / "go.mod").is_file():
            return parent
    return Path.cwd()


def command_text(
    command: list[str],
    cwd: Path | None = None,
    env: dict[str, str] | None = None,
) -> str:
    result = subprocess.run(command, cwd=cwd, env=env, text=True, capture_output=True, check=False)
    if result.returncode != 0:
        details = (result.stderr or result.stdout).strip()
        raise ReleaseError(f"命令失败: {' '.join(command)}\n{details}")
    return result.stdout.strip()


def command(command: list[str], cwd: Path | None = None, env: dict[str, str] | None = None) -> None:
    result = subprocess.run(command, cwd=cwd, env=env, check=False)
    if result.returncode != 0:
        raise ReleaseError(f"命令失败（退出码 {result.returncode}）: {' '.join(command)}")


def parse_go_version(output: str) -> tuple[int, ...] | None:
    match = GO_VERSION_RE.search(output)
    if not match:
        return None
    return tuple(int(part) for part in match.group("version").split("."))


def go_candidates() -> list[str]:
    configured = os.environ.get("EASY_QFNU_GO")
    if configured:
        return [configured]

    candidates = [GO_TOOLCHAIN]
    go_path = shutil.which("go")
    if go_path:
        result = subprocess.run(
            [go_path, "env", "GOPATH"],
            text=True,
            capture_output=True,
            check=False,
        )
        if result.returncode == 0:
            for root in result.stdout.strip().split(os.pathsep):
                if root:
                    candidates.append(str(Path(root) / "bin" / GO_TOOLCHAIN))
    go_executable = "go.exe" if os.name == "nt" else "go"
    candidates.append(str(Path.home() / "sdk" / GO_TOOLCHAIN / "bin" / go_executable))
    candidates.append("go")
    return list(dict.fromkeys(candidates))


def resolve_go() -> tuple[str, tuple[int, ...]]:
    errors = []
    for candidate in go_candidates():
        executable = shutil.which(candidate)
        if not executable:
            continue
        result = subprocess.run([executable, "version"], text=True, capture_output=True, check=False)
        if result.returncode != 0:
            errors.append(f"{candidate}: 无法读取版本")
            continue
        version = parse_go_version(result.stdout)
        if version is None:
            errors.append(f"{candidate}: 版本格式无法识别")
            continue
        if version[:2] != (1, 26) or version < MIN_GO_VERSION:
            errors.append(f"{candidate}: 需要 Go 1.26.2+，实际为 go{'.'.join(map(str, version))}")
            continue

        goroot_result = subprocess.run([executable, "env", "GOROOT"], text=True, capture_output=True, check=False)
        gomodcache_result = subprocess.run([executable, "env", "GOMODCACHE"], text=True, capture_output=True, check=False)
        if goroot_result.returncode != 0 or gomodcache_result.returncode != 0:
            errors.append(f"{candidate}: 无法读取 GOROOT/GOMODCACHE")
            continue
        goroot = goroot_result.stdout.strip()
        gomodcache = gomodcache_result.stdout.strip()
        if goroot and gomodcache:
            try:
                Path(goroot).resolve().relative_to(Path(gomodcache).resolve())
            except ValueError:
                pass
            else:
                errors.append(f"{candidate}: GOROOT 位于 GOMODCACHE，不能供 garble 修改链接器")
                continue
        return executable, version

    details = f"（{'; '.join(errors)}）" if errors else ""
    raise ReleaseError(
        f"发布构建需要独立的 {GO_TOOLCHAIN} 工具链；"
        "Go 自动下载到 GOMODCACHE 的工具链不能供 garble 修改链接器。"
        f"请安装 {GO_TOOLCHAIN}，或设置 EASY_QFNU_GO 指向独立 go 可执行文件。{details}"
    )


def toolchain_env(go_command: str) -> dict[str, str]:
    env = os.environ.copy()
    env["GOTOOLCHAIN"] = "local"
    env.pop("GOROOT", None)
    goroot = command_text([go_command, "env", "GOROOT"], env=env)
    env["PATH"] = str(Path(goroot) / "bin") + os.pathsep + env.get("PATH", "")
    return env


def install_garble(
    go_command: str,
    repo: Path,
    tool_dir: Path,
    base_env: dict[str, str],
) -> Path:
    tool_dir.mkdir(parents=True, exist_ok=True)
    env = base_env.copy()
    env.update({"GOBIN": str(tool_dir), "CGO_ENABLED": "0"})
    for variable in (
        "GOOS",
        "GOARCH",
        "GOARM",
        "GOAMD64",
        "GOARM64",
        "GO386",
        "GOMIPS",
        "GOMIPS64",
    ):
        env.pop(variable, None)
    command([go_command, "install", f"mvdan.cc/garble@{GARBLE_VERSION}"], cwd=repo, env=env)
    executable = tool_dir / ("garble.exe" if os.name == "nt" else "garble")
    if not executable.is_file():
        raise ReleaseError(f"garble {GARBLE_VERSION} 安装后未找到可执行文件")
    return executable


def release_exists(public_repo: str, version: str) -> bool:
    result = subprocess.run(
        ["gh", "release", "view", version, "--repo", public_repo],
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode == 0:
        return True
    error = (result.stderr or result.stdout).lower()
    if "release not found" in error or "not found" in error:
        return False
    raise ReleaseError(f"无法检查公共 Release {version}: {(result.stderr or result.stdout).strip()}")


def previous_release_tag(public_repo: str, current_version: str) -> str | None:
    """Return the latest published tag before the release being prepared."""
    result = subprocess.run(
        [
            "gh",
            "release",
            "list",
            "--repo",
            public_repo,
            "--limit",
            "100",
            "--json",
            "tagName,publishedAt,isDraft,isPrerelease",
        ],
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        raise ReleaseError(f"无法读取历史 Release: {(result.stderr or result.stdout).strip()}")
    try:
        releases = json.loads(result.stdout or "[]")
    except json.JSONDecodeError as exc:
        raise ReleaseError("gh 返回的 Release 列表不是有效 JSON") from exc
    published = [
        release
        for release in releases
        if release.get("tagName")
        and DATE_TAG_RE.fullmatch(release["tagName"])
        and release.get("tagName") != current_version
        and not release.get("isDraft")
        and not release.get("isPrerelease")
        and release.get("publishedAt")
    ]
    published.sort(key=lambda release: release["publishedAt"], reverse=True)
    return published[0]["tagName"] if published else None


def default_skill_repo(cli_repo: Path) -> Path | None:
    configured = os.environ.get("EASY_QFNU_SKILL_REPO")
    candidates = [Path(configured).expanduser()] if configured else []
    candidates.append(cli_repo.parent / "easy-qfnu-skill")
    for candidate in candidates:
        if (candidate / ".git").exists():
            return candidate.resolve()
    return None


def git_subjects(repo: Path | None, previous_tag: str | None) -> list[str]:
    """Read commit subjects since the previous release, newest first."""
    if repo is None:
        return []
    if previous_tag:
        reference = f"{previous_tag}..HEAD"
        available = subprocess.run(
            ["git", "rev-parse", "--verify", previous_tag],
            cwd=repo,
            text=True,
            capture_output=True,
            check=False,
        ).returncode == 0
        if not available:
            return []
    else:
        reference = "HEAD"
    result = subprocess.run(
        ["git", "log", "--format=%s", "--no-merges", reference],
        cwd=repo,
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        return []
    return [line.strip() for line in result.stdout.splitlines() if line.strip()]


def classify_subject(subject: str) -> tuple[str, str]:
    match = COMMIT_RE.match(subject)
    if not match:
        return "maintenance", subject
    commit_type = match.group("type").lower()
    scope = (match.group("scope") or "").lower()
    if scope in {"release", "ci", "docs", "test", "chore", "build"}:
        return "maintenance", match.group("subject").strip()
    if commit_type in FEATURE_TYPES:
        return "features", match.group("subject").strip()
    if commit_type in FIX_TYPES:
        return "fixes", match.group("subject").strip()
    return "maintenance", match.group("subject").strip()


def public_subject(subject: str) -> str:
    """Remove implementation-only details before putting a subject in public notes."""
    subject = subject.replace("github.com/w1ndys/easy-qfnu-cli", "源码仓库")
    subject = subject.replace("easy-qfnu-cli", "源码仓库")
    return subject.rstrip("。．")


def release_notes(
    version: str,
    previous_tag: str | None,
    cli_repo: Path,
    skill_repo: Path | None,
    notes_file: Path | None,
) -> str:
    """Render the fixed, user-facing Release note layout."""
    if notes_file:
        notes = notes_file.read_text(encoding="utf-8").strip()
        if not notes:
            raise ReleaseError(f"Release 文案文件为空: {notes_file}")
        return notes + "\n"

    subjects = git_subjects(cli_repo, previous_tag)
    subjects += git_subjects(skill_repo, previous_tag)
    grouped: dict[str, list[str]] = {"features": [], "fixes": [], "maintenance": []}
    seen: set[str] = set()
    for raw_subject in subjects:
        match = COMMIT_RE.match(raw_subject)
        scope = (match.group("scope") or "").lower() if match else ""
        if scope in {"release", "ci", "test", "chore"}:
            continue
        category, subject = classify_subject(raw_subject)
        subject = public_subject(subject)
        if not subject or subject in seen or raw_subject.lower().startswith("initial commit"):
            continue
        seen.add(subject)
        grouped[category].append(subject)

    def bullets(category: str, empty: str) -> list[str]:
        values = grouped[category]
        return [f"- {value}" for value in values] or [f"- {empty}"]

    change_range = f"`{previous_tag}` → `{version}`" if previous_tag else "首次日期版本"
    lines = [
        "## 🚀 发布说明",
        f"本版本变更范围：{change_range}。",
        "",
        "## ✨ 功能更新",
        *bullets("features", "本版本无新增功能。"),
        "",
        "## 🐛 修复问题",
        *bullets("fixes", "本版本无问题修复。"),
        "",
        "## 🔧 改进与维护",
        *bullets("maintenance", "本版本无额外改进。"),
        "",
        "## 📦 安装",
        "- 下载本页面中与你的操作系统和 CPU 架构对应的 `easy-qfnu` 二进制。",
        "- 使用 `checksums.txt` 校验文件完整性，再通过 skill 目录中的安装脚本放入 `bin/`；无需加入 PATH。",
        "",
        "## 🔐 版本信息",
        "| 项目 | 版本 |",
        "| --- | --- |",
        f"| Release | `{version}` |",
        f"| CLI | `{version}` |",
        f"| skill | `{version}` |",
        "",
        "`manifest.json` 使用 Release 标签作为 Release、CLI 和 skill 的统一版本，并记录各平台产物的 SHA-256；skill 使用前请先更新到最新版本。",
    ]
    return "\n".join(lines) + "\n"


def remote_tag_exists(repo: Path, version: str) -> bool:
    result = subprocess.run(
        ["git", "ls-remote", "--exit-code", "--tags", "origin", f"refs/tags/{version}"],
        cwd=repo,
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode == 0:
        return True
    if result.returncode == 2:
        return False
    raise ReleaseError(f"无法检查源码仓库远端标签: {(result.stderr or result.stdout).strip()}")


def parse_module(repo: Path) -> str:
    go_mod = repo / "go.mod"
    if not go_mod.is_file():
        raise ReleaseError(f"缺少 Go 模块文件: {go_mod}")
    match = MODULE_RE.search(go_mod.read_text(encoding="utf-8"))
    if not match:
        raise ReleaseError("go.mod 中没有有效的 module 声明")
    return match.group(1)


def validate_repo(repo: Path) -> str:
    if not (repo / ".git").exists():
        raise ReleaseError(f"不是 Git 仓库: {repo}")
    if not (repo / "cmd" / "easy-qfnu").exists():
        raise ReleaseError(f"找不到 CLI 入口: {repo / 'cmd' / 'easy-qfnu'}")
    if command_text(["git", "status", "--porcelain"], cwd=repo):
        raise ReleaseError("源码仓库有未提交修改；请先提交或清理工作树")
    return parse_module(repo)


def asset_names() -> list[str]:
    """Deterministic release asset file names, checksums and manifest included."""
    return [
        f"easy-qfnu-{goos}-{goarch}{suffix}" for goos, goarch, suffix in TARGETS
    ] + ["checksums.txt", "manifest.json"]


def build_cache_dir(version: str) -> Path:
    """Persistent per-version artifact cache, reused between dry-run and publish."""
    root = os.environ.get("EASY_QFNU_CACHE_DIR")
    base = Path(root).expanduser() if root else Path.home() / ".cache" / "easy-qfnu-release"
    return base / version


def source_commit(repo: Path) -> str:
    return command_text(["git", "rev-parse", "HEAD"], cwd=repo)


def write_build_info(cache_dir: Path, repo: Path, version: str, go_executable: str) -> None:
    info = {
        "version": version,
        "source_commit": source_commit(repo),
        "toolchain": os.path.realpath(go_executable),
        "garble": GARBLE_VERSION,
    }
    (cache_dir / "build-info.json").write_text(
        json.dumps(info, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


def build_cache_fresh(cache_dir: Path, repo: Path, version: str, go_executable: str) -> bool:
    """True when cached assets match the current version, source commit and toolchain."""
    info_file = cache_dir / "build-info.json"
    if not info_file.is_file():
        return False
    try:
        info = json.loads(info_file.read_text(encoding="utf-8"))
    except (ValueError, OSError):
        return False
    if (
        info.get("version") != version
        or info.get("source_commit") != source_commit(repo)
        or info.get("toolchain") != os.path.realpath(go_executable)
        or info.get("garble") != GARBLE_VERSION
    ):
        return False
    return all((cache_dir / name).is_file() for name in asset_names())


def build(
    repo: Path,
    module: str,
    version: str,
    output_dir: Path,
    go_command: str,
) -> list[Path]:
    env = toolchain_env(go_command)
    env["GARBLE_CACHE"] = str(output_dir / ".garble-cache")
    command([go_command, "test", "./..."], cwd=repo, env=env)
    garble = install_garble(go_command, repo, output_dir / ".release-tools", env)
    binaries: list[Path] = []
    for goos, goarch, suffix in TARGETS:
        name = f"easy-qfnu-{goos}-{goarch}{suffix}"
        output = output_dir / name
        target_env = env.copy()
        target_env.update({"GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0"})
        command(
            [
                str(garble),
                "-seed=random",
                "-tiny",
                "-literals",
                "build",
                "-trimpath",
                f"-ldflags=-s -w -X {module}/internal/qfnu.version={version}",
                "-o",
                str(output),
                "./cmd/easy-qfnu",
            ],
            cwd=repo,
            env=target_env,
        )
        binaries.append(output)
    checksums = output_dir / "checksums.txt"
    lines = []
    for binary in sorted(binaries, key=lambda item: item.name):
        digest = hashlib.sha256(binary.read_bytes()).hexdigest()
        lines.append(f"{digest}  {binary.name}")
    checksums.write_text("\n".join(lines) + "\n", encoding="utf-8")
    manifest = output_dir / "manifest.json"
    manifest.write_text(
        json.dumps(
            {
                "release_version": version,
                "cli_version": version,
                "skill_version": version,
                "assets": {
                    binary.name: {
                        "sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                    }
                    for binary in sorted(binaries, key=lambda item: item.name)
                },
            },
            ensure_ascii=False,
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )
    return binaries + [checksums, manifest]


def publish(
    repo: Path,
    version: str,
    public_repo: str,
    assets: list[Path],
    notes: str,
    replace: bool,
    public_only: bool,
) -> None:
    existing_release = release_exists(public_repo, version)
    local_tag = False
    remote_tag = False
    if not public_only:
        local_tag = subprocess.run(
            ["git", "rev-parse", "--verify", f"refs/tags/{version}"],
            cwd=repo,
            text=True,
            capture_output=True,
            check=False,
        ).returncode == 0
        remote_tag = remote_tag_exists(repo, version)
    if (local_tag or remote_tag or existing_release) and not replace:
        raise ReleaseError(f"版本 {version} 已存在标签或 Release；如需覆盖请明确使用 --replace")

    if not public_only:
        tag_args = ["git", "tag", "-a", version, "-m", f"release(cli): 🚀 发布 easy-qfnu {version}"]
        if local_tag:
            tag_args.insert(2, "-f")
        command(tag_args, cwd=repo)
        push_args = ["git", "push", "origin", version]
        if remote_tag:
            push_args.insert(2, "--force")
        command(push_args, cwd=repo)

    asset_args = [str(asset) for asset in assets]
    title = version
    if existing_release:
        command(["gh", "release", "upload", version, "--repo", public_repo, "--clobber", *asset_args])
        command(["gh", "release", "edit", version, "--repo", public_repo, "--title", title, "--notes", notes])
    else:
        command(
            [
                "gh",
                "release",
                "create",
                version,
                "--repo",
                public_repo,
                "--target",
                "main",
                "--title",
                title,
                "--notes",
                notes,
                *asset_args,
            ]
        )

    expected = {asset.name for asset in assets}
    actual = set(command_text(["gh", "release", "view", version, "--repo", public_repo, "--json", "assets", "--jq", ".assets[].name"]).splitlines())
    missing = expected - actual
    if missing:
        raise ReleaseError(f"Release 已发布但缺少资产: {', '.join(sorted(missing))}")


def parse_args() -> argparse.Namespace:
    today = dt.datetime.now().astimezone().strftime("v%Y.%m.%d.%H")
    parser = argparse.ArgumentParser(description="Build and publish easy-qfnu date releases")
    parser.add_argument("--repo", type=Path, default=None, help="local CLI repository")
    parser.add_argument("--public-repo", default=None, help="public release repository")
    parser.add_argument("--version", default=today, help=f"date tag (default: {today})")
    parser.add_argument("--notes-file", type=Path, default=None, help="可选的 Release 文案文件")
    parser.add_argument("--publish", action="store_true", help="create/push tag and upload release")
    parser.add_argument("--replace", action="store_true", help="replace an existing same-hour tag/release")
    parser.add_argument("--public-only", action="store_true", help="only update the public Release; do not create or push a source tag")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if not VERSION_RE.fullmatch(args.version):
        raise ReleaseError("版本必须使用 vYYYY.MM.DD.HH 格式，例如 v2026.08.30.14")
    default_repo = default_cli_repo()
    repo = (args.repo or Path(os.environ.get("EASY_QFNU_CLI_REPO", default_repo))).expanduser().resolve()
    public_repo = args.public_repo or os.environ.get("EASY_QFNU_PUBLIC_REPO", "w1ndys/easy-qfnu-skill")
    if args.public_only and not args.publish:
        raise ReleaseError("--public-only 只能与 --publish 一起使用")
    module = validate_repo(repo)
    go_command, go_version = resolve_go()
    command(["gh", "auth", "status"])
    if args.replace and not args.publish:
        raise ReleaseError("--replace 只能与 --publish 一起使用")
    if args.notes_file and not args.notes_file.is_file():
        raise ReleaseError(f"找不到 Release 文案文件: {args.notes_file}")

    previous_tag = previous_release_tag(public_repo, args.version)
    skill_repo = default_skill_repo(repo)
    notes = release_notes(
        args.version,
        previous_tag,
        repo,
        skill_repo,
        args.notes_file,
    )
    print(f"源码仓库: {repo}")
    print(f"目标 Release: {public_repo}")
    print(f"上一个 Release: {previous_tag or '无（首次日期版本）'}")
    print(f"Release/CLI/skill 统一版本: {args.version}")
    print(f"构建工具链: go{'.'.join(map(str, go_version))} ({go_command})")
    print(f"混淆工具: garble {GARBLE_VERSION}")
    print("Release 文案:")
    print(notes, end="")
    print("模式: publish" if args.publish else "模式: dry-run")

    cache_dir = build_cache_dir(args.version)
    cache_dir.mkdir(parents=True, exist_ok=True)
    if build_cache_fresh(cache_dir, repo, args.version, go_command):
        assets = [cache_dir / name for name in asset_names()]
        print("构建产物（复用缓存，未重新交叉编译）:")
    else:
        assets = build(repo, module, args.version, cache_dir, go_command)
        write_build_info(cache_dir, repo, args.version, go_command)
        print("构建产物:")
    for asset in assets:
        print(f"  {asset.name} ({asset.stat().st_size} bytes)")
    if args.publish:
        publish(repo, args.version, public_repo, assets, notes, args.replace, args.public_only)
        print(f"发布完成: https://github.com/{public_repo}/releases/tag/{args.version}")
    else:
        print("dry-run 完成；构建产物已缓存，确认发布时将直接复用，无需重新交叉编译。")
    return 0

if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ReleaseError as exc:
        print(f"错误: {exc}", file=sys.stderr)
        raise SystemExit(1)

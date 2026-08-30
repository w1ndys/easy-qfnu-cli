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
import tempfile
from pathlib import Path


TARGETS = (
    ("linux", "amd64", ""),
    ("linux", "arm64", ""),
    ("darwin", "amd64", ""),
    ("darwin", "arm64", ""),
    ("windows", "amd64", ".exe"),
)
VERSION_RE = re.compile(r"^v(?:\d{4}\.\d{2}\.\d{2}\.\d{4}|\d+\.\d+\.\d+)$")
MODULE_RE = re.compile(r"^module\s+(\S+)$", re.MULTILINE)


class ReleaseError(RuntimeError):
    """A release precondition or publication step failed."""


def default_cli_repo() -> Path:
    """Locate the Go repository that contains this local skill."""
    for parent in Path(__file__).resolve().parents:
        if (parent / ".git").exists() and (parent / "go.mod").is_file():
            return parent
    return Path.cwd()


def command_text(command: list[str], cwd: Path | None = None) -> str:
    result = subprocess.run(command, cwd=cwd, text=True, capture_output=True)
    if result.returncode != 0:
        details = (result.stderr or result.stdout).strip()
        raise ReleaseError(f"命令失败: {' '.join(command)}\n{details}")
    return result.stdout.strip()


def command(command: list[str], cwd: Path | None = None, env: dict[str, str] | None = None) -> None:
    result = subprocess.run(command, cwd=cwd, env=env)
    if result.returncode != 0:
        raise ReleaseError(f"命令失败（退出码 {result.returncode}）: {' '.join(command)}")


def release_exists(public_repo: str, version: str) -> bool:
    result = subprocess.run(
        ["gh", "release", "view", version, "--repo", public_repo],
        text=True,
        capture_output=True,
    )
    if result.returncode == 0:
        return True
    error = (result.stderr or result.stdout).lower()
    if "release not found" in error or "not found" in error:
        return False
    raise ReleaseError(f"无法检查公共 Release {version}: {(result.stderr or result.stdout).strip()}")


def remote_tag_exists(repo: Path, version: str) -> bool:
    result = subprocess.run(
        ["git", "ls-remote", "--exit-code", "--tags", "origin", f"refs/tags/{version}"],
        cwd=repo,
        text=True,
        capture_output=True,
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


def build(repo: Path, module: str, cli_version: str, skill_version: str, output_dir: Path) -> list[Path]:
    command(["go", "test", "./..."], cwd=repo)
    binaries: list[Path] = []
    for goos, goarch, suffix in TARGETS:
        name = f"easy-qfnu-{goos}-{goarch}{suffix}"
        output = output_dir / name
        env = os.environ.copy()
        env.update({"GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0"})
        command(
            [
                "go",
                "build",
                "-trimpath",
                f"-ldflags=-s -w -X {module}/internal/qfnu.version={cli_version}",
                "-o",
                str(output),
                "./cmd/easy-qfnu",
            ],
            cwd=repo,
            env=env,
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
                "release_version": cli_version,
                "cli_version": cli_version,
                "skill_version": skill_version,
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
    cli_version: str,
    skill_version: str,
    public_repo: str,
    assets: list[Path],
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
    title = f"easy-qfnu {version} (CLI {cli_version}, skill {skill_version})"
    notes = (
        f"CLI 版本: `{cli_version}`\n"
        f"skill 版本: `{skill_version}`\n\n"
        "本 Release 的 `manifest.json` 同时记录 Release、CLI 和 skill 版本。"
    )
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
    today = dt.datetime.now().astimezone().strftime("v%Y.%m.%d.%H%M")
    parser = argparse.ArgumentParser(description="Build and publish easy-qfnu date releases")
    parser.add_argument("--repo", type=Path, default=None, help="local CLI repository")
    parser.add_argument("--public-repo", default=None, help="public release repository")
    parser.add_argument("--version", default=today, help=f"date tag (default: {today})")
    parser.add_argument("--skill-version", default=None, help="skill version (default: same as --version)")
    parser.add_argument("--publish", action="store_true", help="create/push tag and upload release")
    parser.add_argument("--replace", action="store_true", help="replace an existing same-day tag/release")
    parser.add_argument("--public-only", action="store_true", help="only update the public Release; do not create or push a source tag")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if not VERSION_RE.fullmatch(args.version):
        raise ReleaseError("版本必须使用 vYYYY.MM.DD.HHmm 或 vX.Y.Z 格式，例如 v2026.08.30.1430")
    default_repo = default_cli_repo()
    repo = (args.repo or Path(os.environ.get("EASY_QFNU_CLI_REPO", default_repo))).expanduser().resolve()
    public_repo = args.public_repo or os.environ.get("EASY_QFNU_PUBLIC_REPO", "w1ndys/easy-qfnu-skill")
    skill_version = args.skill_version or args.version
    if not VERSION_RE.fullmatch(skill_version):
        raise ReleaseError("skill 版本必须使用 vYYYY.MM.DD.HHmm 或 vX.Y.Z 格式")
    if args.public_only and not args.publish:
        raise ReleaseError("--public-only 只能与 --publish 一起使用")
    module = validate_repo(repo)
    command(["gh", "auth", "status"])
    if args.replace and not args.publish:
        raise ReleaseError("--replace 只能与 --publish 一起使用")

    print(f"源码仓库: {repo}")
    print(f"目标 Release: {public_repo}")
    print(f"Release: {args.version}")
    print(f"CLI 版本: {args.version}")
    print(f"skill 版本: {skill_version}")
    print("模式: publish" if args.publish else "模式: dry-run")
    with tempfile.TemporaryDirectory(prefix="easy-qfnu-release-") as temp:
        assets = build(repo, module, args.version, skill_version, Path(temp))
        print("构建产物:")
        for asset in assets:
            print(f"  {asset.name} ({asset.stat().st_size} bytes)")
        if args.publish:
            publish(repo, args.version, args.version, skill_version, public_repo, assets, args.replace, args.public_only)
            print(f"发布完成: https://github.com/{public_repo}/releases/tag/{args.version}")
        else:
            print("dry-run 完成；获得用户明确确认后再加 --publish。")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ReleaseError as exc:
        print(f"错误: {exc}", file=sys.stderr)
        raise SystemExit(1)

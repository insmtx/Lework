#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import shutil
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

CATEGORIES = {"tender", "bidder", "reference"}
WORK_DIRS = ["项目", "招标/normalized", "投标文件/模板", "填充内容", "独立复核", ".tmp"]
SKILL_ROOT = Path(__file__).resolve().parents[3]


def validate_project_code(project_code: str) -> None:
    if not project_code or project_code in {".", ".."}:
        raise ValueError("PROJECT_CODE 不能为空或使用 . / ..")
    if Path(project_code).name != project_code or "/" in project_code or "\\" in project_code:
        raise ValueError("PROJECT_CODE 必须是单一目录名，不能包含路径分隔符")


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def write_json_atomic(path: Path, payload: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", suffix=".tmp", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8", newline="") as handle:
            handle.write(json.dumps(payload, ensure_ascii=False, indent=2) + "\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def project_file(root: Path) -> Path:
    return root / "work" / "项目" / "project.json"


def manifest_file(root: Path) -> Path:
    return root / "work" / "招标" / "input-manifest.json"


def safe_destination(directory: Path, source: Path) -> Path:
    candidate = directory / source.name
    if not candidate.exists():
        return candidate
    for index in range(2, 10000):
        candidate = directory / f"{source.stem}__{index}{source.suffix}"
        if not candidate.exists():
            return candidate
    raise RuntimeError("无法为输入文件生成安全名称")


def cmd_init(args: argparse.Namespace) -> int:
    validate_project_code(args.project_code)
    workspace = Path(args.workspace_root).expanduser().resolve()
    root = workspace / args.project_code
    if root.exists() and any(root.iterdir()):
        raise FileExistsError(f"目标项目目录非空: {root}")
    root.mkdir(parents=True, exist_ok=True)
    (root / "input").mkdir()
    for rel in WORK_DIRS:
        (root / "work" / rel).mkdir(parents=True, exist_ok=True)
    (root / "output").mkdir()
    version_path = SKILL_ROOT / "VERSION"
    version = version_path.read_text(encoding="utf-8").strip() if version_path.is_file() else "unknown"
    payload = {
        "schema_version": "bid-project/v2",
        "project_code": args.project_code,
        "project_name": args.project_name,
        "workspace_root": str(workspace),
        "project_root": str(root),
        "created_at": now_iso(),
        "skill_version": version,
        "status": "active",
    }
    write_json_atomic(project_file(root), payload)
    (root / "workspace.yaml").write_text(
        "schema_version: bid-workspace/v2\n"
        f"project_code: {args.project_code}\n"
        "paths:\n  input: input\n  work: work\n  output: output\n",
        encoding="utf-8",
    )
    print(root)
    return 0


def load_project(root: Path) -> dict[str, Any]:
    path = project_file(root)
    if not path.is_file():
        raise FileNotFoundError(f"缺少 project.json: {path}")
    payload = json.loads(path.read_text(encoding="utf-8"))
    if Path(payload.get("project_root", "")).resolve() != root.resolve():
        raise ValueError("project.json 与项目根不一致")
    return payload


def cmd_import(args: argparse.Namespace) -> int:
    root = Path(args.project_root).expanduser().resolve()
    load_project(root)
    destination_dir = root / "input"
    imported = []
    for raw in args.file:
        source = Path(raw).expanduser().resolve()
        if not source.is_file():
            raise FileNotFoundError(source)
        destination = safe_destination(destination_dir, source)
        shutil.copy2(source, destination)
        status = "copied"
        item = {"category": args.category, "source": str(source), "stored_path": str(destination.relative_to(root)), "status": status}
        imported.append(item)
    # 输入类别属于冻结清单本身；导入阶段只维护一个可重建的草稿清单，
    # 不另建运行历史或事件日志。
    files = scan_inputs(root)
    imported_categories = {item["stored_path"]: item["category"] for item in imported}
    for item in files:
        if item["stored_path"] in imported_categories:
            item["category"] = imported_categories[item["stored_path"]]
    write_json_atomic(
        manifest_file(root),
        {
            "schema_version": "bid-input-manifest/v2",
            "status": "draft",
            "updated_at": now_iso(),
            "files": files,
        },
    )
    print(json.dumps(imported, ensure_ascii=False, indent=2))
    return 0


def scan_inputs(root: Path) -> list[dict[str, Any]]:
    imported: dict[str, str] = {}
    manifest = manifest_file(root)
    if manifest.is_file():
        try:
            payload = json.loads(manifest.read_text(encoding="utf-8"))
            imported = {
                str(item.get("stored_path")): str(item.get("category", "reference"))
                for item in payload.get("files", [])
                if isinstance(item, dict) and item.get("stored_path")
            }
        except json.JSONDecodeError:
            pass
    result = []
    for index, path in enumerate(sorted(p for p in (root / "input").rglob("*") if p.is_file()), 1):
        rel = str(path.relative_to(root))
        result.append({
            "file_id": f"INPUT-{index:04d}",
            "category": imported.get(rel, "reference"),
            "original_name": path.name,
            "stored_path": rel,
            "extension": path.suffix.lower(),
            "size_bytes": path.stat().st_size,
            "status": "active",
        })
    return result


def cmd_freeze(args: argparse.Namespace) -> int:
    root = Path(args.project_root).expanduser().resolve()
    load_project(root)
    files = scan_inputs(root)
    if not files:
        raise ValueError("input/ 中没有可冻结文件")
    payload = {"schema_version": "bid-input-manifest/v2", "frozen_at": now_iso(), "files": files}
    write_json_atomic(manifest_file(root), payload)
    print(json.dumps(payload, ensure_ascii=False, indent=2))
    return 0


def cmd_verify(args: argparse.Namespace) -> int:
    root = Path(args.project_root).expanduser().resolve()
    load_project(root)
    if not manifest_file(root).is_file():
        raise FileNotFoundError("尚未冻结输入")
    payload = json.loads(manifest_file(root).read_text(encoding="utf-8"))
    missing = [item["stored_path"] for item in payload.get("files", []) if not (root / item["stored_path"]).is_file()]
    print(json.dumps({"status": "passed" if not missing else "failed", "missing": missing}, ensure_ascii=False))
    return 0 if not missing else 2


def parser() -> argparse.ArgumentParser:
    command = argparse.ArgumentParser(description="bid-init 项目创建、输入导入与冻结工具")
    sub = command.add_subparsers(dest="command", required=True)

    item = sub.add_parser("init")
    item.add_argument("--workspace-root", required=True)
    item.add_argument("--project-code", required=True)
    item.add_argument("--project-name", required=True)
    item.set_defaults(func=cmd_init)

    item = sub.add_parser("import-input")
    item.add_argument("--project-root", required=True)
    item.add_argument("--category", required=True, choices=sorted(CATEGORIES))
    item.add_argument("--file", required=True, action="append")
    item.set_defaults(func=cmd_import)

    for name, handler in [("freeze-inputs", cmd_freeze), ("verify-inputs", cmd_verify)]:
        item = sub.add_parser(name)
        item.add_argument("--project-root", required=True)
        item.set_defaults(func=handler)
    return command


def main() -> int:
    try:
        args = parser().parse_args()
        return args.func(args)
    except Exception as exc:
        print(f"ERROR: {exc}")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())

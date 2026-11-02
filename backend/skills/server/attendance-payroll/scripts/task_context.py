#!/usr/bin/env python3
"""Discover the current Lework Task ID from its Runtime-provided turn path."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from datetime import datetime
from pathlib import Path


TASK_ID_RE = re.compile(r"^task_[1-9A-HJ-NP-Za-km-z]+$")
REQUEST_ID_RE = re.compile(r"^req_[A-Za-z0-9_-]+$")
OPERATION_ID_MAX_LENGTH = 100


class ContextError(ValueError):
    """A fail-closed task context or batch identifier error."""


def _reject_symlink_components(path: Path) -> None:
    """Reject symlink traversal instead of trusting its resolved destination."""
    absolute = Path(path.anchor)
    for part in path.parts[1:]:
        absolute = absolute / part
        try:
            if absolute.is_symlink():
                raise ContextError("SYMLINK_PATH_REJECTED")
        except OSError as exc:
            raise ContextError("PATH_INSPECTION_FAILED") from exc


def discover_task_context(repo_root: str, turn_tmp_dir: str) -> dict:
    """Validate the fixed Runtime path shape and derive its Task ID."""
    if not isinstance(repo_root, str) or not repo_root.strip():
        raise ContextError("PROJECT_ROOT_MISSING")
    if not isinstance(turn_tmp_dir, str) or not turn_tmp_dir.strip():
        raise ContextError("TURN_TMP_PATH_MISSING")

    repo_input = Path(repo_root)
    tmp_input = Path(turn_tmp_dir)
    if not repo_input.is_absolute() or not tmp_input.is_absolute():
        raise ContextError("ABSOLUTE_PATH_REQUIRED")
    if ".." in repo_input.parts or ".." in tmp_input.parts:
        raise ContextError("PATH_TRAVERSAL_REJECTED")
    if "\\" in repo_root or "\\" in turn_tmp_dir:
        raise ContextError("PATH_SEPARATOR_INVALID")

    repo = Path(Path(repo_input.anchor), *[part for part in repo_input.parts[1:] if part != "."])
    tmp = Path(Path(tmp_input.anchor), *[part for part in tmp_input.parts[1:] if part != "."])
    _reject_symlink_components(repo)
    _reject_symlink_components(tmp)
    try:
        repo_real = repo.resolve(strict=True)
        tmp_real = tmp.resolve(strict=True)
    except (OSError, RuntimeError) as exc:
        raise ContextError("PATH_NOT_FOUND") from exc
    if not repo_real.is_dir() or not tmp_real.is_dir():
        raise ContextError("PATH_NOT_DIRECTORY")

    try:
        relative = tmp_real.relative_to(repo_real)
    except ValueError as exc:
        raise ContextError("TURN_TMP_OUTSIDE_PROJECT") from exc
    parts = relative.parts
    if len(parts) != 6 or parts[0] != ".lework" or parts[1] != "tasks" or parts[3] != "turns" or parts[5] != "tmp":
        raise ContextError("TASK_PATH_SHAPE_INVALID")
    task_id, request_id = parts[2], parts[4]
    if not TASK_ID_RE.fullmatch(task_id):
        raise ContextError("TASK_ID_INVALID")
    if not REQUEST_ID_RE.fullmatch(request_id):
        raise ContextError("REQUEST_ID_INVALID")
    return {
        "ok": True,
        "task_id": task_id,
        "source": "runtime_turn_tmp_path",
        "confidence": "context_derived",
        "is_authorization_proof": False,
    }


def _month(value: str) -> str:
    if not isinstance(value, str) or not re.fullmatch(r"\d{4}-\d{2}", value):
        raise ContextError("MONTH_INVALID")
    try:
        parsed = datetime.strptime(value, "%Y-%m")
    except ValueError as exc:
        raise ContextError("MONTH_INVALID") from exc
    if parsed.strftime("%Y-%m") != value:
        raise ContextError("MONTH_INVALID")
    return value


def derive_batch_context(
    task_id: str,
    month: str,
    attendance_sha256: str,
    identity_sha256: str,
    person_ids: list[int],
    expected_scope_sha256: str | None = None,
) -> dict:
    """Derive stable batch/operation IDs and a canonical request-scope digest."""
    if not isinstance(task_id, str) or not TASK_ID_RE.fullmatch(task_id):
        raise ContextError("TASK_ID_INVALID")
    month = _month(month)
    if not isinstance(attendance_sha256, str) or not re.fullmatch(r"[0-9a-fA-F]{64}", attendance_sha256):
        raise ContextError("ATTENDANCE_SHA256_INVALID")
    if not isinstance(identity_sha256, str) or not re.fullmatch(r"[0-9a-fA-F]{64}", identity_sha256):
        raise ContextError("IDENTITY_SHA256_INVALID")
    if not isinstance(person_ids, list) or not person_ids:
        raise ContextError("PERSON_IDS_REQUIRED")
    normalized: list[int] = []
    for value in person_ids:
        if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
            raise ContextError("PERSON_ID_INVALID")
        normalized.append(value)
    if len(normalized) != len(set(normalized)):
        raise ContextError("PERSON_ID_DUPLICATE")
    normalized.sort()

    batch_id = f"{task_id}-{month}"
    operation_ids = {
        "submit": f"submit:{batch_id}",
        "precheck": f"precheck:{batch_id}",
        "calculate": f"calculate:{batch_id}",
    }
    for value in [batch_id, *operation_ids.values()]:
        if len(value) > OPERATION_ID_MAX_LENGTH or not re.fullmatch(r"[A-Za-z0-9._:-]+", value):
            raise ContextError("PAYROLL_ID_CONTRACT_EXCEEDED")

    scope = {
        "attendance_month": month,
        "attendance_sha256": attendance_sha256.lower(),
        "identity_sha256": identity_sha256.lower(),
        "payroll_person_ids": normalized,
    }
    encoded = json.dumps(scope, sort_keys=True, separators=(",", ":")).encode("utf-8")
    scope_sha256 = hashlib.sha256(encoded).hexdigest()
    if expected_scope_sha256 is not None:
        if not re.fullmatch(r"[0-9a-fA-F]{64}", expected_scope_sha256):
            raise ContextError("EXPECTED_SCOPE_SHA256_INVALID")
        if scope_sha256 != expected_scope_sha256.lower():
            raise ContextError("BATCH_SCOPE_CHANGED")
    return {
        "batch_id": batch_id,
        "operation_ids": operation_ids,
        "scope": scope,
        "scope_sha256": scope_sha256,
        "scope_change_requires_stop": True,
        "is_authorization_proof": False,
        "is_payroll_receipt": False,
    }


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    discover = commands.add_parser("discover", help="discover current Task ID from Runtime workspace paths")
    discover.add_argument("--repo-root", required=True)
    discover.add_argument("--turn-tmp-dir", required=True)
    batch = commands.add_parser("batch-id", help="derive stable Payroll IDs for the current task and request scope")
    batch.add_argument("--repo-root", required=True)
    batch.add_argument("--turn-tmp-dir", required=True)
    batch.add_argument("--month", required=True)
    batch.add_argument("--attendance-sha256", required=True)
    batch.add_argument("--identity-sha256", required=True)
    batch.add_argument("--payroll-person-ids", required=True, help="JSON array of unique positive integer IDs")
    batch.add_argument(
        "--expected-scope-sha256",
        help="previously authorized/verified scope digest; mismatch is a hard error",
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    try:
        context = discover_task_context(args.repo_root, args.turn_tmp_dir)
        if args.command == "discover":
            output = context
        else:
            try:
                person_ids = json.loads(args.payroll_person_ids)
            except json.JSONDecodeError as exc:
                raise ContextError("PERSON_IDS_JSON_INVALID") from exc
            output = {
                **context,
                **derive_batch_context(
                    context["task_id"], args.month, args.attendance_sha256, args.identity_sha256, person_ids,
                    args.expected_scope_sha256,
                ),
            }
        print(json.dumps(output, ensure_ascii=False, sort_keys=True))
        return 0
    except ContextError as exc:
        print(json.dumps({"ok": False, "error": str(exc), "is_authorization_proof": False}, ensure_ascii=False, sort_keys=True))
        return 2


if __name__ == "__main__":
    sys.exit(main())

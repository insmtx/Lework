#!/usr/bin/env python3
"""Create and validate task-local, user-confirmed Payroll project bindings."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import tempfile
import unicodedata
from pathlib import Path


SCHEMA_VERSION = 1
RULES_VERSION = "E01-R3.11"
CONFIRMATION_FORMS = (
    "确认以上考勤及项目对应关系",
    "确认以上考勤和项目对应关系",
    "确认上述项目映射",
    "确认上述项目映射无误",
    "以上项目对应关系无误",
    "以上项目对应关系正确",
    "确认按上述项目归属处理",
)
COMBINED_ATTENDANCE_CONFIRMATION_FORMS = (
    "确认以上考勤及项目对应关系",
    "确认以上考勤和项目对应关系",
)


class ProjectBindingError(ValueError):
    pass


def _read(path: str | Path) -> tuple[object, bytes]:
    try:
        raw = Path(path).read_bytes()
        return json.loads(raw), raw
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ProjectBindingError("PROJECT_BINDING_SOURCE_UNREADABLE") from exc


def _sha(raw: bytes) -> str:
    return hashlib.sha256(raw).hexdigest()


def _canonical_sha(value: object) -> str:
    raw = json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False).encode("utf-8")
    return _sha(raw)


def _normalized_bindings(by_source: dict[str, str]) -> list[dict[str, str]]:
    return [
        {"source_project_name": source, "payroll_project_name": by_source[source]}
        for source in sorted(by_source)
    ]


def _normalize_confirmation(message: str) -> str:
    return "".join(
        char for char in message
        if not char.isspace() and not unicodedata.category(char).startswith("P")
    )


def _is_confirmation(message: str, bindings: list[dict[str, str]]) -> bool:
    """Accept only a complete confirmation form; appended clauses never inherit its intent."""
    if not isinstance(message, str) or any(mark in message for mark in ("?", "？")):
        return False
    normalized = _normalize_confirmation(message)
    allowed = {_normalize_confirmation(form) for form in CONFIRMATION_FORMS}
    for prefix in ("好", "好的", "我"):
        allowed.update(_normalize_confirmation(prefix + form) for form in CONFIRMATION_FORMS)

    # Keep the prior explicit per-project wording, but require the whole message to match.
    rows = sorted(bindings, key=lambda row: row["source_project_name"])
    mapped = [f"{row['source_project_name']}对应{row['payroll_project_name']}" for row in rows]
    legacy_forms = {
        "确认" + "项目" + "；项目".join(mapped),
        "确认" + "项目" + "；".join(mapped),
        "确认" + "；".join(mapped),
    }
    if len(mapped) == 1:
        legacy_forms.add("确认" + mapped[0])
    allowed.update(_normalize_confirmation(form) for form in legacy_forms)
    return normalized in allowed


def is_combined_attendance_confirmation(message: str) -> bool:
    """Recognize the exact first-turn confirmation for attendance and project binding."""
    if not isinstance(message, str) or any(mark in message for mark in ("?", "？")):
        return False
    allowed = {_normalize_confirmation(form) for form in COMBINED_ATTENDANCE_CONFIRMATION_FORMS}
    for prefix in ("好", "好的", "我"):
        allowed.update(_normalize_confirmation(prefix + form) for form in COMBINED_ATTENDANCE_CONFIRMATION_FORMS)
    return _normalize_confirmation(message) in allowed


def _conversation_message(messages: object, index: object, role: str, content: object,
                          content_sha256: object, error_code: str) -> str:
    if isinstance(index, bool) or not isinstance(index, int) or index < 0:
        raise ProjectBindingError(error_code)
    if not isinstance(content, str) or not content.strip() or not re.fullmatch(r"[0-9a-f]{64}", str(content_sha256)):
        raise ProjectBindingError(error_code)
    if not isinstance(messages, list):
        raise ProjectBindingError(error_code)
    matching = [
        entry for entry in messages
        if isinstance(entry, dict) and type(entry.get("index")) is int and entry.get("index") == index
    ]
    if (len(matching) != 1 or matching[0].get("role") != role or matching[0].get("content") != content
            or _sha(content.encode("utf-8")) != content_sha256):
        raise ProjectBindingError(error_code)
    return content


def _validate_display_content(message: str, month: str, bindings: list[dict[str, str]], people_count: int) -> None:
    compact = re.sub(r"\s+", "", message).replace(":", "：")
    if f"考勤月份：{month}" not in compact:
        raise ProjectBindingError("PROJECT_BINDING_DISPLAY_MONTH_MISSING")
    if f"考勤人数：{people_count}人" not in compact:
        raise ProjectBindingError("PROJECT_BINDING_DISPLAY_ATTENDANCE_COUNT_MISSING")
    if not any(label in compact for label in ("考勤确认表", "考勤核对表")):
        raise ProjectBindingError("PROJECT_BINDING_DISPLAY_REVIEW_MISSING")
    if not any(label in compact for label in ("待核事项：", "异常事项：")):
        raise ProjectBindingError("PROJECT_BINDING_DISPLAY_EXCEPTIONS_MISSING")
    for row in bindings:
        expected_line = f"来源项目：{row['source_project_name']} → Payroll 项目：{row['payroll_project_name']}"
        if expected_line not in message:
            raise ProjectBindingError("PROJECT_BINDING_DISPLAY_MAPPING_INCOMPLETE")


def _write(path: str | Path, value: dict) -> None:
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    payload = (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
    fd, temp_name = tempfile.mkstemp(prefix=f".{target.name}.", dir=target.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(payload)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temp_name, target)
    finally:
        if os.path.exists(temp_name):
            os.unlink(temp_name)


def _sources(attendance: object) -> tuple[str, list[str]]:
    if not isinstance(attendance, dict) or attendance.get("schema_version") != 1:
        raise ProjectBindingError("PROJECT_BINDING_ATTENDANCE_INVALID")
    month = attendance.get("month")
    people = attendance.get("people")
    if not isinstance(month, str) or not re.fullmatch(r"\d{4}-(0[1-9]|1[0-2])", month):
        raise ProjectBindingError("PROJECT_BINDING_MONTH_INVALID")
    if not isinstance(people, list) or not people:
        raise ProjectBindingError("PROJECT_BINDING_PEOPLE_INVALID")
    projects = set()
    for person in people:
        project = person.get("project") if isinstance(person, dict) else None
        if not isinstance(project, str) or not project.strip():
            raise ProjectBindingError("PROJECT_BINDING_SOURCE_PROJECT_UNKNOWN")
        projects.add(project)
    return month, sorted(projects)


def roster_data(roster: object) -> dict:
    """Return a direct DTO or the repository's verified MCP JSON wrapper."""
    if not isinstance(roster, dict):
        raise ProjectBindingError("PROJECT_BINDING_ROSTER_INVALID")
    if not ({"content", "structuredContent", "isError", "_meta"} & set(roster)):
        return roster
    allowed = {"content", "structuredContent", "isError", "_meta"}
    if set(roster) - allowed or "structuredContent" not in roster or roster.get("isError", False) is not False:
        raise ProjectBindingError("PROJECT_BINDING_ROSTER_MCP_ENVELOPE_INVALID")
    if "_meta" in roster and not isinstance(roster["_meta"], dict):
        raise ProjectBindingError("PROJECT_BINDING_ROSTER_MCP_ENVELOPE_INVALID")
    content = roster.get("content")
    if not isinstance(content, list) or len(content) != 1 or not isinstance(content[0], dict):
        raise ProjectBindingError("PROJECT_BINDING_ROSTER_MCP_CONTENT_INVALID")
    text_content = content[0]
    if set(text_content) != {"type", "text"} or text_content.get("type") != "text" or not isinstance(
        text_content.get("text"), str
    ):
        raise ProjectBindingError("PROJECT_BINDING_ROSTER_MCP_CONTENT_INVALID")
    try:
        text_dto = json.loads(text_content["text"])
    except json.JSONDecodeError as exc:
        raise ProjectBindingError("PROJECT_BINDING_ROSTER_MCP_CONTENT_INVALID") from exc
    structured = roster["structuredContent"]
    if not isinstance(structured, dict) or _canonical_sha(text_dto) != _canonical_sha(structured):
        raise ProjectBindingError("PROJECT_BINDING_ROSTER_MCP_CONTENT_MISMATCH")
    return structured


def _candidate_projects(roster: object) -> list[str]:
    roster = roster_data(roster)
    if not isinstance(roster, dict) or not isinstance(roster.get("items"), list):
        raise ProjectBindingError("PROJECT_BINDING_ROSTER_INVALID")
    items = roster["items"]
    if "count" in roster:
        count = roster["count"]
        if isinstance(count, bool) or not isinstance(count, int) or count != len(items):
            raise ProjectBindingError("PROJECT_BINDING_ROSTER_COUNT_MISMATCH")
    projects = set()
    roster_ids = set()
    for item in items:
        if not isinstance(item, dict):
            raise ProjectBindingError("PROJECT_BINDING_ROSTER_INVALID")
        roster_id = item.get("roster_id")
        if isinstance(roster_id, bool) or not isinstance(roster_id, int) or roster_id <= 0 or roster_id in roster_ids:
            raise ProjectBindingError("PROJECT_BINDING_ROSTER_ID_INVALID")
        roster_ids.add(roster_id)
        project = item.get("project_name")
        if project is not None:
            if not isinstance(project, str):
                raise ProjectBindingError("PROJECT_BINDING_ROSTER_PROJECT_INVALID")
            if project.strip():
                projects.add(project.strip())
    return sorted(projects)


def prepare(attendance_path: str | Path, roster_path: str | Path) -> dict:
    attendance, attendance_raw = _read(attendance_path)
    roster, roster_raw = _read(roster_path)
    month, source_projects = _sources(attendance)
    people_count = len(attendance["people"])
    return {
        "status": "PROJECT_BINDING_REQUIRED",
        "month": month,
        "people_count": people_count,
        "attendance_sha256": _sha(attendance_raw),
        "candidate_response_sha256": _sha(roster_raw),
        "candidate_source": "list_roster",
        "source_projects": source_projects,
        "payroll_project_candidates": _candidate_projects(roster),
        "message_source_verified": False,
        "is_authorization_proof": False,
    }


def confirm(attendance_path: str | Path, roster_path: str | Path, request_path: str | Path,
            output_path: str | Path) -> dict:
    expected_roster, expected_binding = _default_paths(str(attendance_path))
    if Path(roster_path).resolve() != expected_roster.resolve() or Path(output_path).resolve() != expected_binding.resolve():
        raise ProjectBindingError("PROJECT_BINDING_MUST_USE_TASK_WORK_DIRECTORY")
    prepared = prepare(attendance_path, roster_path)
    request, _ = _read(request_path)
    if not isinstance(request, dict):
        raise ProjectBindingError("PROJECT_BINDING_CONFIRMATION_INVALID")
    if (request.get("month") != prepared["month"]
            or request.get("attendance_sha256") != prepared["attendance_sha256"]
            or request.get("candidate_response_sha256") != prepared["candidate_response_sha256"]):
        raise ProjectBindingError("PROJECT_BINDING_CONFIRMATION_STALE")
    candidates = set(prepared["payroll_project_candidates"])
    requested = request.get("bindings")
    if not isinstance(requested, list):
        raise ProjectBindingError("PROJECT_BINDING_BINDINGS_REQUIRED")
    by_source = {}
    for item in requested:
        if not isinstance(item, dict):
            raise ProjectBindingError("PROJECT_BINDING_BINDINGS_INVALID")
        source, target = item.get("source_project_name"), item.get("payroll_project_name")
        if (not isinstance(source, str) or not source.strip() or source in by_source
                or not isinstance(target, str) or target not in candidates):
            raise ProjectBindingError("PROJECT_BINDING_TARGET_NOT_IN_ACTIVE_ROSTER")
        by_source[source] = target
    if set(by_source) != set(prepared["source_projects"]):
        raise ProjectBindingError("PROJECT_BINDING_SOURCE_SET_MISMATCH")
    normalized_bindings = _normalized_bindings(by_source)
    mapping_sha256 = _canonical_sha(normalized_bindings)
    messages = request.get("conversation_messages")
    display = request.get("display_evidence")
    if not isinstance(display, dict):
        raise ProjectBindingError("PROJECT_BINDING_DISPLAY_EVIDENCE_REQUIRED")
    display_index = display.get("message_index")
    display_message = _conversation_message(
        messages, display_index, "assistant", display.get("message"), display.get("message_sha256"),
        "PROJECT_BINDING_DISPLAY_EVIDENCE_INVALID",
    )
    if (display.get("month") != prepared["month"]
            or display.get("attendance_sha256") != prepared["attendance_sha256"]
            or display.get("candidate_response_sha256") != prepared["candidate_response_sha256"]
            or display.get("bindings") != normalized_bindings
            or display.get("mapping_sha256") != mapping_sha256):
        raise ProjectBindingError("PROJECT_BINDING_DISPLAY_EVIDENCE_MISMATCH")
    _validate_display_content(display_message, prepared["month"], normalized_bindings, prepared["people_count"])
    confirmation = request.get("user_confirmation")
    if not isinstance(confirmation, dict):
        raise ProjectBindingError("PROJECT_BINDING_USER_CONFIRMATION_REQUIRED")
    message_index = confirmation.get("message_index")
    message = _conversation_message(
        messages, message_index, "user", confirmation.get("message"), confirmation.get("message_sha256"),
        "PROJECT_BINDING_CONFIRMATION_EVIDENCE_INVALID",
    )
    if display_index >= message_index:
        raise ProjectBindingError("PROJECT_BINDING_DISPLAY_AFTER_CONFIRMATION")
    if not _is_confirmation(message, normalized_bindings):
        raise ProjectBindingError("PROJECT_BINDING_CONFIRMATION_MESSAGE_INVALID")
    body = {
        "schema_version": SCHEMA_VERSION,
        "rules_version": RULES_VERSION,
        "status": "CONFIRMED",
        "month": prepared["month"],
        "attendance_sha256": prepared["attendance_sha256"],
        "candidate_source": {
            "tool": "list_roster",
            "file": "payroll-roster.json",
            "sha256": prepared["candidate_response_sha256"],
            "payroll_project_names": prepared["payroll_project_candidates"],
        },
        "source_projects": prepared["source_projects"],
        "bindings": normalized_bindings,
        "display_evidence": {
            "message_index": display_index,
            "message_sha256": display["message_sha256"],
            "message": display_message,
            "month": prepared["month"],
            "attendance_sha256": prepared["attendance_sha256"],
            "candidate_response_sha256": prepared["candidate_response_sha256"],
            "mapping_sha256": mapping_sha256,
            "message_source_verified": False,
            "is_authorization_proof": False,
        },
        "user_confirmation": {
            "message_index": message_index,
            "message_sha256": _sha(message.encode("utf-8")),
            "message": message,
            "message_source_verified": False,
            "is_authorization_proof": False,
        },
    }
    binding = {**body, "binding_sha256": _canonical_sha(body)}
    _write(output_path, binding)
    return binding


def validate(attendance_path: str | Path, roster_path: str | Path, binding_path: str | Path) -> dict:
    expected_roster, expected_binding = _default_paths(str(attendance_path))
    if Path(roster_path).resolve() != expected_roster.resolve() or Path(binding_path).resolve() != expected_binding.resolve():
        raise ProjectBindingError("PROJECT_BINDING_MUST_USE_TASK_WORK_DIRECTORY")
    attendance, attendance_raw = _read(attendance_path)
    roster, roster_raw = _read(roster_path)
    binding, _ = _read(binding_path)
    if not isinstance(binding, dict):
        raise ProjectBindingError("PROJECT_BINDING_INVALID")
    month, source_projects = _sources(attendance)
    candidates = _candidate_projects(roster)
    body = {key: value for key, value in binding.items() if key != "binding_sha256"}
    if (binding.get("schema_version") != SCHEMA_VERSION or binding.get("rules_version") != RULES_VERSION
            or binding.get("status") != "CONFIRMED" or binding.get("month") != month
            or binding.get("attendance_sha256") != _sha(attendance_raw)
            or binding.get("source_projects") != source_projects
            or binding.get("binding_sha256") != _canonical_sha(body)):
        raise ProjectBindingError("PROJECT_BINDING_STALE_OR_INVALID")
    candidate_source = binding.get("candidate_source")
    if (not isinstance(candidate_source, dict) or candidate_source.get("tool") != "list_roster"
            or candidate_source.get("file") != "payroll-roster.json"
            or candidate_source.get("sha256") != _sha(roster_raw)
            or candidate_source.get("payroll_project_names") != candidates):
        raise ProjectBindingError("PROJECT_BINDING_CANDIDATE_SOURCE_MISMATCH")
    confirmation = binding.get("user_confirmation")
    if (not isinstance(confirmation, dict) or isinstance(confirmation.get("message_index"), bool)
            or not isinstance(confirmation.get("message_index"), int) or confirmation["message_index"] < 0
            or not re.fullmatch(r"[0-9a-f]{64}", str(confirmation.get("message_sha256", "")))
            or not isinstance(confirmation.get("message"), str)
            or _sha(confirmation["message"].encode("utf-8")) != confirmation.get("message_sha256")
            or confirmation.get("message_source_verified") is not False
            or confirmation.get("is_authorization_proof") is not False):
        raise ProjectBindingError("PROJECT_BINDING_CONFIRMATION_INVALID")
    bindings = binding.get("bindings")
    if not isinstance(bindings, list) or len(bindings) != len(source_projects):
        raise ProjectBindingError("PROJECT_BINDING_BINDINGS_INVALID")
    mapping = {}
    for item in bindings:
        if not isinstance(item, dict):
            raise ProjectBindingError("PROJECT_BINDING_BINDINGS_INVALID")
        source, target = item.get("source_project_name"), item.get("payroll_project_name")
        if (not isinstance(source, str) or not isinstance(target, str)
                or source not in source_projects or source in mapping or target not in candidates):
            raise ProjectBindingError("PROJECT_BINDING_BINDINGS_INVALID")
        mapping[source] = target
    if set(mapping) != set(source_projects):
        raise ProjectBindingError("PROJECT_BINDING_SOURCE_SET_MISMATCH")
    normalized_bindings = _normalized_bindings(mapping)
    display = binding.get("display_evidence")
    display_index = display.get("message_index") if isinstance(display, dict) else None
    if (not isinstance(display, dict) or isinstance(display_index, bool) or not isinstance(display_index, int)
            or display_index < 0 or display_index >= confirmation["message_index"]
            or not re.fullmatch(r"[0-9a-f]{64}", str(display.get("message_sha256", "")))
            or not isinstance(display.get("message"), str)
            or _sha(display["message"].encode("utf-8")) != display.get("message_sha256")
            or display.get("month") != month
            or display.get("attendance_sha256") != _sha(attendance_raw)
            or display.get("candidate_response_sha256") != _sha(roster_raw)
            or display.get("mapping_sha256") != _canonical_sha(normalized_bindings)
            or display.get("message_source_verified") is not False
            or display.get("is_authorization_proof") is not False):
        raise ProjectBindingError("PROJECT_BINDING_DISPLAY_EVIDENCE_INVALID")
    if not _is_confirmation(confirmation["message"], normalized_bindings):
        raise ProjectBindingError("PROJECT_BINDING_CONFIRMATION_INVALID")
    _validate_display_content(display["message"], month, normalized_bindings, len(attendance["people"]))
    project_map = {}
    for source, target in mapping.items():
        for value in (source, target):
            prior = project_map.get(value)
            if prior is not None and prior != target:
                raise ProjectBindingError("PROJECT_BINDING_PROJECT_COLLISION")
            project_map[value] = target
    return {"binding": binding, "project_map": project_map}


def validate_submission(attendance_path: str | Path, matches_path: str | Path, submission_path: str | Path,
                        selected_record_ids: list[str], month: str, batch_id: str, operation_id: str) -> dict:
    roster_path, binding_path = _default_paths(str(attendance_path))
    verified = validate(attendance_path, roster_path, binding_path)
    attendance, attendance_raw = _read(attendance_path)
    matches, _ = _read(matches_path)
    submission, submission_raw = _read(submission_path)
    if (not isinstance(matches, dict) or matches.get("schema_version") != 1
            or matches.get("attendance_sha256") != _sha(attendance_raw)
            or matches.get("project_binding_sha256") != verified["binding"]["binding_sha256"]):
        raise ProjectBindingError("PROJECT_BINDING_IDENTITY_DIGEST_MISMATCH")
    if not isinstance(submission, dict) or submission.get("attendance_month") != month:
        raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_MONTH_MISMATCH")
    if (submission.get("submission_mode") != "BATCH_SNAPSHOT" or submission.get("batch_id") != batch_id
            or submission.get("operation_id") != operation_id):
        raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_BATCH_MISMATCH")
    people = attendance.get("people") if isinstance(attendance, dict) else None
    match_rows = matches.get("matches")
    records = submission.get("records")
    if not isinstance(people, list) or not isinstance(match_rows, list) or not isinstance(records, list):
        raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_INVALID")
    person_by_record = {p.get("record_id"): p for p in people if isinstance(p, dict)}
    match_by_record = {m.get("record_id"): m for m in match_rows if isinstance(m, dict)}
    if (len(person_by_record) != len(people) or len(match_by_record) != len(match_rows)
            or any(not isinstance(record_id, str) or not record_id for record_id in person_by_record)
            or any(not isinstance(record_id, str) or not record_id for record_id in match_by_record)):
        raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_IDENTITY_INVALID")
    selected = set(selected_record_ids)
    if not selected or len(selected) != len(selected_record_ids):
        raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_SCOPE_INVALID")
    if not selected.issubset(person_by_record) or not selected.issubset(match_by_record):
        raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_SCOPE_MISMATCH")
    expected_by_pid = {}
    for record_id in selected:
        source = person_by_record[record_id].get("project")
        match = match_by_record[record_id]
        if not isinstance(source, str) or match.get("status") != "matched":
            raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_IDENTITY_INVALID")
        payroll_id = match.get("payroll_person_id")
        if isinstance(payroll_id, bool) or not isinstance(payroll_id, int) or payroll_id <= 0 or payroll_id in expected_by_pid:
            raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_PAYROLL_ID_INVALID_OR_DUPLICATE")
        target = verified["project_map"].get(source)
        if target is None:
            raise ProjectBindingError("PROJECT_BINDING_SOURCE_PROJECT_UNBOUND")
        expected_by_pid[payroll_id] = (source.strip(), target)
    seen = set()
    ordered_ids = []
    for record in records:
        if not isinstance(record, dict):
            raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_RECORD_INVALID")
        payroll_id = record.get("payroll_person_id")
        if isinstance(payroll_id, bool) or not isinstance(payroll_id, int) or payroll_id in seen:
            raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_PAYROLL_ID_INVALID_OR_DUPLICATE")
        seen.add(payroll_id)
        ordered_ids.append(payroll_id)
        expected = expected_by_pid.get(payroll_id)
        if expected is None:
            raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_SCOPE_MISMATCH")
        if (record.get("source_project_snapshot") != expected[0]
                or record.get("project_snapshot") != expected[1]):
            raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_PROJECT_MISMATCH")
    if seen != set(expected_by_pid):
        raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_SCOPE_MISMATCH")

    # Reuse the exporter's canonical projection so the pre-write gate checks the
    # complete Payroll DTO against the same source mapping as workbook export.
    try:
        import build_payroll_workbook as workbook

        workbook._keys(submission, workbook.SUBMISSION_ROOT_KEYS, workbook.SUBMISSION_ROOT_KEYS,
                       "SUBMISSION_REQUEST")
        canonical_ids = [match_by_record[record_id].get("payroll_person_id") for record_id in selected_record_ids]
        if ordered_ids != canonical_ids:
            raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_SCOPE_ORDER_MISMATCH")
        for record_id in selected_record_ids:
            match = match_by_record[record_id]
            source_person = person_by_record[record_id]
            expected = workbook._attendance_record_projection(
                source_person, match, month, verified["project_map"], allow_unmapped_statuses=False,
            )
            expected.pop("unmapped_days", None)
            actual = records[canonical_ids.index(match["payroll_person_id"])]
            workbook._keys(actual, workbook.SUBMISSION_RECORD_KEYS,
                           workbook.ATTENDANCE_RECORD_REQUIRED_KEYS, "SUBMISSION_REQUEST_RECORD")
            workbook._compare_attendance_record(expected, actual)
    except ProjectBindingError:
        raise
    except (workbook.ExportError, KeyError, TypeError, ValueError) as exc:
        raise ProjectBindingError("PROJECT_BINDING_SUBMISSION_PAYLOAD_MISMATCH") from exc

    return {
        **verified,
        "submission_request": submission,
        "submission_request_sha256": _sha(submission_raw),
    }


def _default_paths(attendance: str) -> tuple[Path, Path]:
    parent = Path(attendance).resolve().parent
    work = parent if parent.name == ".attendance-work" else parent / ".attendance-work"
    return work / "payroll-roster.json", work / "project-binding.json"


def default_paths(attendance: str | Path) -> tuple[Path, Path]:
    """Return the task-local roster and binding paths for an attendance file."""
    return _default_paths(str(attendance))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    prepare_parser = sub.add_parser("prepare", help="从当前考勤和只读花名册生成项目确认清单")
    prepare_parser.add_argument("--attendance", required=True)
    prepare_parser.add_argument("--roster", required=True)
    prepare_parser.add_argument("--output", required=True)
    confirm_parser = sub.add_parser("confirm", help="验证用户确认并保存任务级项目绑定")
    confirm_parser.add_argument("--attendance", required=True)
    confirm_parser.add_argument("--roster", required=True)
    confirm_parser.add_argument("--request", required=True)
    confirm_parser.add_argument("--output", required=True)
    args = parser.parse_args()
    roster_default, binding_default = _default_paths(args.attendance)
    expected_roster = roster_default.resolve()
    if Path(args.roster).resolve() != expected_roster:
        raise ProjectBindingError("PAYROLL_ROSTER_MUST_BE_SAVED_IN_TASK_WORK_DIRECTORY")
    if Path(args.output).resolve() != binding_default.resolve():
        raise ProjectBindingError("PROJECT_BINDING_MUST_BE_SAVED_IN_TASK_WORK_DIRECTORY")
    if args.command == "prepare":
        result = prepare(args.attendance, args.roster)
        Path(args.output).parent.mkdir(parents=True, exist_ok=True)
        Path(args.output).write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    else:
        result = confirm(args.attendance, args.roster, args.request, args.output)
    print(json.dumps({"ok": True, "status": result["status"], "path": args.output}, ensure_ascii=False))


if __name__ == "__main__":
    main()

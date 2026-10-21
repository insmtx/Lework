#!/usr/bin/env python3
"""Check normal Skill stage prerequisites and explicit user intent.

This is a workflow guard for the Skill's normal route, not a trusted Runtime
authorization service. It never calls Payroll or persists authorization.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from pathlib import Path

import payroll_readiness
import project_binding


STAGES = (
    "ATTENDANCE_REVIEW",
    "IDENTITY_MATCHING",
    "SALARY_READINESS",
    "PAYROLL_EXECUTION",
    "SUBMISSION_SCOPE_REVIEW",
    "ATTENDANCE_SUBMISSION",
    "PAYROLL_PRECHECK",
    "PAYROLL_CALCULATION",
    "PAYROLL_EXPORT",
)


class GateError(ValueError):
    pass


def _read(path: str) -> dict:
    try:
        value = json.loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise GateError("REQUEST_INVALID_OR_UNREADABLE") from exc
    if not isinstance(value, dict):
        raise GateError("REQUEST_MUST_BE_OBJECT")
    return value


def _scope(evidence: object, key: str, expected_status: str) -> dict:
    if not isinstance(evidence, dict):
        raise GateError(f"{key}_EVIDENCE_REQUIRED")
    status = evidence.get("status")
    if status != expected_status:
        raise GateError(f"{key}_NOT_COMPLETE")
    month, version, record_ids = evidence.get("month"), evidence.get("version"), evidence.get("record_ids")
    attendance_sha256 = evidence.get("attendance_sha256")
    if not isinstance(month, str) or not re.fullmatch(r"\d{4}-(0[1-9]|1[0-2])", month):
        raise GateError(f"{key}_MONTH_INVALID")
    if not isinstance(version, str) or not version.strip():
        raise GateError(f"{key}_VERSION_REQUIRED")
    if not isinstance(attendance_sha256, str) or not re.fullmatch(r"[0-9a-f]{64}", attendance_sha256):
        raise GateError(f"{key}_CONTENT_DIGEST_REQUIRED")
    if not isinstance(record_ids, list) or not record_ids or any(not isinstance(x, str) or not x.strip() for x in record_ids):
        raise GateError(f"{key}_RECORD_IDS_INVALID")
    if len(record_ids) != len(set(record_ids)):
        raise GateError(f"{key}_RECORD_IDS_DUPLICATE")
    return {
        "month": month,
        "version": version,
        "attendance_sha256": attendance_sha256,
        "record_ids": sorted(record_ids),
    }


def _user_messages(value: object) -> list[str]:
    if not isinstance(value, list) or not value or any(not isinstance(item, str) or not item.strip() for item in value):
        raise GateError("CURRENT_TASK_USER_MESSAGES_REQUIRED")
    return value


def _contains_all(text: str, scope: dict, labels: object) -> bool:
    if scope["month"] not in text or scope["version"] not in text:
        return False
    if not isinstance(labels, list) or len(labels) != len(scope["record_ids"]):
        return False
    if any(not isinstance(label, str) or not label.strip() for label in labels) or len(set(labels)) != len(labels):
        return False
    return all(label in text for label in labels)


def _person_labels(evidence: object, scope: dict, key: str) -> list[str]:
    people = evidence.get("people") if isinstance(evidence, dict) else None
    if not isinstance(people, list) or len(people) != len(scope["record_ids"]):
        raise GateError(f"{key}_PEOPLE_REQUIRED")
    by_id = {}
    for person in people:
        if not isinstance(person, dict):
            raise GateError(f"{key}_PEOPLE_INVALID")
        record_id, name = person.get("record_id"), person.get("name")
        if (
            not isinstance(record_id, str) or record_id not in scope["record_ids"] or record_id in by_id
            or not isinstance(name, str) or not name.strip()
        ):
            raise GateError(f"{key}_PEOPLE_INVALID")
        by_id[record_id] = name
    if set(by_id) != set(scope["record_ids"]):
        raise GateError(f"{key}_PEOPLE_SCOPE_MISMATCH")
    labels = [by_id[record_id] for record_id in scope["record_ids"]]
    if len(set(labels)) != len(labels):
        raise GateError(f"{key}_AMBIGUOUS_NAMES_REQUIRE_CLARIFICATION")
    return labels


def _negated(text: str) -> bool:
    return any(word in text for word in (
        "不得", "不能", "不要", "禁止", "不授权", "未授权", "尚未", "不同意", "不提交", "暂不", "先不",
        "先别", "不用", "不必", "无需", "不需要", "不是", "并非", "暂缓", "不想", "别",
    ))


def _uncertain(text: str) -> bool:
    return any(mark in text for mark in (
        "?", "？", "是否", "要不要", "可以吗", "能否", "能不能", "可否", "如果",
    ))


def _batch_scope(evidence: object, scope: dict) -> dict:
    if not isinstance(evidence, dict) or evidence.get("status") != "VERIFIED_NO_CONFLICT":
        raise GateError("BATCH_CONFLICT_OR_UNVERIFIED")
    task_id = evidence.get("task_id")
    batch_id = evidence.get("batch_id")
    operation_id = evidence.get("operation_id")
    if not isinstance(task_id, str) or not re.fullmatch(r"task_[1-9A-HJ-NP-Za-km-z]+", task_id):
        raise GateError("TASK_CONTEXT_INVALID")
    expected_batch = f"{task_id}-{scope['month']}"
    if batch_id != expected_batch or operation_id != f"submit:{expected_batch}":
        raise GateError("BATCH_OR_OPERATION_ID_MISMATCH")
    if {key: evidence.get(key) for key in ("month", "version", "attendance_sha256", "record_ids")} != scope:
        raise GateError("BATCH_SCOPE_MISMATCH")
    return evidence


def _payroll_operation_state(evidence: dict, scope: dict, batch_context: dict) -> dict:
    """Validate fresh read-only lookups for the fixed Task operation IDs."""
    state = evidence.get("payroll_operation_state")
    if not isinstance(state, dict) or state.get("status") != "VERIFIED":
        raise GateError("PAYROLL_OPERATION_STATE_UNVERIFIED")
    batch_id = batch_context["batch_id"]
    if (state.get("task_id") != batch_context["task_id"] or state.get("batch_id") != batch_id
            or state.get("month") != scope["month"]):
        raise GateError("PAYROLL_OPERATION_STATE_SCOPE_MISMATCH")
    operations = state.get("operations")
    if not isinstance(operations, dict):
        raise GateError("PAYROLL_OPERATION_STATE_UNVERIFIED")
    checked = {}
    for key, prefix in (("submission", "submit"), ("calculation", "calculate")):
        item = operations.get(key)
        if not isinstance(item, dict) or item.get("operation_id") != f"{prefix}:{batch_id}":
            raise GateError("PAYROLL_OPERATION_STATE_UNVERIFIED")
        status = item.get("status")
        if not isinstance(status, str) or status not in {"NOT_FOUND", "SUCCEEDED"}:
            raise GateError("PAYROLL_OPERATION_STATE_UNVERIFIED")
        if status == "SUCCEEDED":
            old_scope = {
                "month": item.get("month"),
                "attendance_sha256": item.get("attendance_sha256"),
                "record_ids": item.get("record_ids"),
                "payroll_person_ids": item.get("payroll_person_ids"),
            }
            if (old_scope["month"] != scope["month"]
                    or not isinstance(old_scope["attendance_sha256"], str)
                    or not re.fullmatch(r"[0-9a-f]{64}", old_scope["attendance_sha256"])
                    or not isinstance(old_scope["record_ids"], list)
                    or any(not isinstance(value, str) or not value.strip() for value in old_scope["record_ids"])
                    or len(old_scope["record_ids"]) != len(set(old_scope["record_ids"]))
                    or not isinstance(old_scope["payroll_person_ids"], list)
                    or any(isinstance(value, bool) or not isinstance(value, int) or value <= 0
                           for value in old_scope["payroll_person_ids"])
                    or len(old_scope["payroll_person_ids"]) != len(set(old_scope["payroll_person_ids"]))):
                raise GateError("PAYROLL_OPERATION_STATE_UNVERIFIED")
            old_scope["record_ids"] = sorted(old_scope["record_ids"])
            old_scope["payroll_person_ids"] = sorted(old_scope["payroll_person_ids"])
            current_person_ids = _positive_id_list(
                batch_context.get("payroll_person_ids"), "BATCH_PAYROLL_PERSON_IDS",
            )
            if old_scope["payroll_person_ids"] != current_person_ids:
                raise GateError("PAYROLL_OPERATION_STATE_PERSON_SCOPE_MISMATCH")
            checked[key] = {"status": status, **old_scope}
        else:
            checked[key] = {"status": status}
    return checked


def _require_no_prior_payroll_write(operations: dict, scope: dict) -> None:
    if operations["submission"]["status"] == "SUCCEEDED":
        _require_current_operation_scope(operations["submission"], scope, "SUBMISSION")
        raise GateError("PAYROLL_SUBMISSION_ALREADY_EXISTS")
    if operations["calculation"]["status"] == "SUCCEEDED":
        _require_current_operation_scope(operations["calculation"], scope, "CALCULATION")
        raise GateError("PAYROLL_CALCULATION_ALREADY_EXISTS")


def _require_current_operation_scope(operation: dict, scope: dict, label: str) -> None:
    if operation.get("status") != "SUCCEEDED":
        raise GateError(f"{label}_RECEIPT_REQUIRED")
    if any(operation.get(key) != scope[key] for key in ("month", "attendance_sha256", "record_ids")):
        raise GateError(f"ATTENDANCE_CORRECTION_AFTER_{label}_BLOCKED")


def _sha256(value: object, label: str) -> str:
    if not isinstance(value, str) or not re.fullmatch(r"[0-9a-f]{64}", value):
        raise GateError(f"{label}_DIGEST_INVALID")
    return value


def _text_sha256(value: str) -> str:
    return hashlib.sha256(value.encode("utf-8")).hexdigest()


def _normalize_scope_display(text: str) -> str:
    """Normalize only presentation markup; names, counts, and scope text stay exact."""
    text = text.replace("**", "").replace("__", "").replace("\r\n", "\n").replace("\r", "\n")
    lines = []
    for line in text.split("\n"):
        line = line.strip()
        line = re.sub(r"^(?:[-*+]|\d+[.)、])[ \t]+", "、", line)
        lines.append(line)
    text = "".join(lines)
    text = re.sub(r"([：:、])(?:[-*+]|\d+[.)、])[ \t]+", r"\1、", text)
    text = re.sub(r"(工资核算范围|纳入|排除事项)\s*[:：]\s*", r"\1：", text)
    text = re.sub(r"\s*([、，,;；。])\s*", r"\1", text)
    return re.sub(r"(?<=\d)\s+(?=人)", "", text)


def _positive_id_list(value: object, label: str) -> list[int]:
    if not isinstance(value, list) or not value or any(
        isinstance(item, bool) or not isinstance(item, int) or item <= 0 for item in value
    ) or len(value) != len(set(value)):
        raise GateError(f"{label}_INVALID")
    return sorted(value)


def _scope_digest(scope: dict, identity_sha: str, person_ids: list[int], readiness_sha: str,
                  exclusions_sha: str | None = None) -> str:
    payload = {
        "month": scope["month"], "version": scope["version"],
        "attendance_sha256": scope["attendance_sha256"],
        "person_matches_sha256": identity_sha,
        "readiness_normalized_response_sha256": readiness_sha,
        "record_ids": scope["record_ids"], "payroll_person_ids": person_ids,
    }
    if exclusions_sha is not None:
        payload["scope_exclusions_sha256"] = exclusions_sha
    raw = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(raw).hexdigest()


def _actual_readiness_report(evidence: dict, review_scope: dict, identity_sha: str) -> tuple[dict, dict]:
    """Revalidate the on-disk report; local consistency is not remote authenticity."""
    attendance_path = evidence.get("attendance_path")
    matches_path = evidence.get("person_matches_path")
    report_path = evidence.get("readiness_report_path")
    paths = (attendance_path, matches_path, report_path)
    if any(not isinstance(path, str) or not path.strip() for path in paths):
        raise GateError("READINESS_REPORT_PATH_REQUIRED")
    attendance_file, matches_file, report_file = (Path(path) for path in paths)
    if (any(path.is_symlink() or not path.is_file() for path in (attendance_file, matches_file, report_file))
            or attendance_file.parent.is_symlink() or matches_file.parent.is_symlink()
            or report_file.parent.is_symlink() or attendance_file.name != "attendance.json"
            or matches_file.name != "person-matches.json"):
        raise GateError("READINESS_REPORT_PATH_UNTRUSTED_OR_UNAVAILABLE")
    try:
        work_dir = attendance_file.parent.resolve(strict=True)
        if (work_dir.name != ".attendance-work" or matches_file.parent.resolve(strict=True) != work_dir
                or report_file.parent.resolve(strict=True) != work_dir
                or report_file.name != "payroll-readiness.json"):
            raise GateError("READINESS_REPORT_PATH_UNTRUSTED_OR_UNAVAILABLE")
    except OSError as exc:
        raise GateError("READINESS_REPORT_PATH_UNTRUSTED_OR_UNAVAILABLE") from exc

    try:
        report_before = report_file.read_bytes()
    except OSError as exc:
        raise GateError("READINESS_REPORT_INVALID") from exc
    result = payroll_readiness.report_status(
        str(report_file), str(attendance_file), str(matches_file), review_scope["month"],
    )
    if not isinstance(result, dict) or result.get("ok") is not True or result.get("status") != "COVERAGE_CHECKED":
        status = result.get("status") if isinstance(result, dict) else None
        errors = {
            "UNAVAILABLE": "READINESS_REPORT_UNAVAILABLE",
            "READINESS_REPORT_INVALID": "READINESS_REPORT_INVALID",
            "READINESS_STALE": "READINESS_REPORT_STALE",
            "READINESS_CHECK_BLOCKED": "READINESS_REPORT_CHECK_BLOCKED",
        }
        raise GateError(errors.get(status, "READINESS_REPORT_INVALID"))
    try:
        report_after = report_file.read_bytes()
        if report_before != report_after:
            raise GateError("READINESS_REPORT_CHANGED_DURING_VALIDATION")
        actual = json.loads(report_after.decode("utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise GateError("READINESS_REPORT_INVALID") from exc
    if not isinstance(actual, dict) or actual.get("status") != "COVERAGE_CHECKED":
        raise GateError("READINESS_REPORT_INVALID")
    if (actual.get("attendance_sha256") != review_scope["attendance_sha256"]
            or actual.get("person_matches_sha256") != identity_sha
            or actual.get("payroll_month") != review_scope["month"]):
        raise GateError("READINESS_REPORT_SOURCE_MISMATCH")
    return actual, result


def _compare_readiness_evidence(readiness: dict, actual: dict, status_result: dict) -> None:
    if readiness.get("status") != "COVERAGE_CHECKED":
        raise GateError("READINESS_NOT_COMPLETE")
    if readiness.get("normalized_response_sha256") != actual.get("normalized_response_sha256"):
        raise GateError("READINESS_NORMALIZED_RESPONSE_MISMATCH")
    if readiness.get("counts") != actual.get("counts") or status_result.get("counts") != actual.get("counts"):
        raise GateError("READINESS_COUNTS_MISMATCH")
    actual_items = actual.get("items")
    provided_items = readiness.get("items")
    item_keys = ("record_id", "name", "payroll_person_id", "status", "source_month", "reason_code")
    if not isinstance(actual_items, list) or not isinstance(provided_items, list):
        raise GateError("READINESS_ITEMS_REQUIRED")
    if any(not isinstance(item, dict) or set(item) != set(item_keys) for item in provided_items):
        raise GateError("READINESS_ITEM_INVALID")

    def project(items: list) -> list:
        return [
            {key: item.get(key) for key in item_keys} if isinstance(item, dict) else None for item in items
        ]

    if project(provided_items) != project(actual_items):
        raise GateError("READINESS_ITEMS_REPORT_MISMATCH")
    if readiness.get("payroll_response_summary") != actual.get("payroll_response_summary"):
        raise GateError("READINESS_RESPONSE_SUMMARY_MISMATCH")
    if readiness.get("record_ids") != actual.get("selected_record_ids"):
        raise GateError("READINESS_RECORD_SCOPE_MISMATCH")
    if readiness.get("requested_payroll_person_ids") != actual.get("requested_payroll_person_ids"):
        raise GateError("READINESS_PERSON_SCOPE_MISMATCH")


def _scope_exclusion_decisions(evidence: dict, conversation: list[dict], review_scope: dict,
                               identity_sha: str, by_record: dict[str, dict]) -> list[dict]:
    unresolved = {
        record_id: person for record_id, person in by_record.items()
        if person["status"] in {"ambiguous", "pending_confirmation"}
    }
    decisions = evidence.get("scope_exclusion_decisions", [])
    if not isinstance(decisions, list) or len(decisions) != len(unresolved):
        raise GateError("UNRESOLVED_IDENTITY_CONFLICT")
    checked = {}
    required_fields = {
        "record_id", "name", "reason", "month", "version", "attendance_sha256",
        "person_matches_sha256", "confirmation_message_index", "confirmation_message_sha256",
        "message_source_verified",
    }
    for decision in decisions:
        if not isinstance(decision, dict) or set(decision) != required_fields:
            raise GateError("SCOPE_EXCLUSION_DECISION_INVALID")
        record_id = decision.get("record_id")
        person = unresolved.get(record_id)
        reason = decision.get("reason")
        index = decision.get("confirmation_message_index")
        if (person is None or record_id in checked or decision.get("name") != person["name"]
                or not isinstance(reason, str) or not reason.strip() or len(reason) > 500
                or any(decision.get(key) != review_scope[key] for key in
                       ("month", "version", "attendance_sha256"))
                or decision.get("person_matches_sha256") != identity_sha
                or decision.get("message_source_verified") is not False
                or isinstance(index, bool) or not isinstance(index, int)
                or not 0 <= index < len(conversation) or conversation[index].get("role") != "user"):
            raise GateError("SCOPE_EXCLUSION_DECISION_INVALID")
        message = conversation[index]["content"]
        normalized = re.sub(r"[\s，,。；;：:]", "", message)
        if (decision.get("confirmation_message_sha256") != _text_sha256(message)
                or person["name"] not in message or _uncertain(message)
                or not re.search(r"(?:暂不|不)纳入(?:本次|这次)?(?:工资)?核算", normalized)
                and not re.search(r"(?:本次|这次)(?:暂不|不)纳入(?:工资)?核算", normalized)):
            raise GateError("SCOPE_EXCLUSION_USER_CONFIRMATION_REQUIRED")
        checked[record_id] = {
            "record_id": record_id, "name": person["name"], "reason": reason.strip(),
            "month": review_scope["month"], "version": review_scope["version"],
            "attendance_sha256": review_scope["attendance_sha256"],
            "person_matches_sha256": identity_sha,
            "confirmation_message_index": index,
            "confirmation_message_sha256": decision["confirmation_message_sha256"],
            "message_source_verified": False,
        }
    if set(checked) != set(unresolved):
        raise GateError("UNRESOLVED_IDENTITY_CONFLICT")
    return [checked[record_id] for record_id in sorted(checked)]


def _execution_context(evidence: dict, conversation: list[dict]) -> dict:
    review_scope = _scope(evidence.get("attendance_review"), "ATTENDANCE_REVIEW", "DECLARED")
    identity = evidence.get("identity_matching")
    identity_scope = _scope(identity, "IDENTITY_MATCHING", "COMPLETE")
    readiness = evidence.get("salary_readiness")
    readiness_scope = _scope(readiness, "SALARY_READINESS", "COVERAGE_CHECKED")
    for other, error in ((identity_scope, "IDENTITY_REVIEW_SCOPE_MISMATCH"),
                         (readiness_scope, "READINESS_REVIEW_SCOPE_MISMATCH")):
        if any(other[key] != review_scope[key] for key in ("month", "version", "attendance_sha256")):
            raise GateError(error)

    identity_sha = _sha256(identity.get("person_matches_sha256"), "IDENTITY_MAPPING")
    _verify_current_identity(evidence.get("person_matches_path"), identity_sha)
    if readiness.get("person_matches_sha256") != identity_sha:
        raise GateError("IDENTITY_READINESS_DIGEST_MISMATCH")
    readiness_sha = _sha256(readiness.get("normalized_response_sha256"), "READINESS_RESPONSE")
    actual_readiness, readiness_status = _actual_readiness_report(evidence, review_scope, identity_sha)
    _compare_readiness_evidence(readiness, actual_readiness, readiness_status)

    identity_people = identity.get("people")
    if not isinstance(identity_people, list) or len(identity_people) != len(review_scope["record_ids"]):
        raise GateError("IDENTITY_PEOPLE_REQUIRED")
    by_record: dict[str, dict] = {}
    matched_by_pid: dict[int, str] = {}
    for person in identity_people:
        if not isinstance(person, dict):
            raise GateError("IDENTITY_PEOPLE_INVALID")
        record_id, name, status = person.get("record_id"), person.get("name"), person.get("status")
        if (not isinstance(record_id, str) or record_id not in review_scope["record_ids"]
                or record_id in by_record or not isinstance(name, str) or not name.strip()):
            raise GateError("IDENTITY_PEOPLE_INVALID")
        if not isinstance(status, str) or status not in {"matched", "unmatched", "ambiguous", "pending_confirmation"}:
            raise GateError("IDENTITY_STATUS_INVALID")
        pid = person.get("payroll_person_id")
        if status == "matched":
            if isinstance(pid, bool) or not isinstance(pid, int) or pid <= 0 or pid in matched_by_pid:
                raise GateError("IDENTITY_PAYROLL_PERSON_ID_INVALID_OR_DUPLICATE")
            matched_by_pid[pid] = record_id
        elif pid is not None:
            raise GateError("UNMATCHED_IDENTITY_HAS_PAYROLL_ID")
        by_record[record_id] = person
    if set(by_record) != set(review_scope["record_ids"]):
        raise GateError("IDENTITY_PEOPLE_SCOPE_MISMATCH")
    identity_source = _read(evidence["person_matches_path"])
    source_rows = identity_source.get("matches")
    if (identity_source.get("schema_version") != 1
            or identity_source.get("attendance_sha256") != review_scope["attendance_sha256"]
            or not isinstance(source_rows, list)):
        raise GateError("IDENTITY_MAPPING_CONTENT_INVALID")
    attendance_path = evidence.get("attendance_path")
    if not isinstance(attendance_path, str) or not attendance_path:
        raise GateError("CURRENT_ATTENDANCE_PATH_REQUIRED")
    roster_path, binding_path = project_binding.default_paths(attendance_path)
    try:
        binding = project_binding.validate(attendance_path, roster_path, binding_path)
    except project_binding.ProjectBindingError as exc:
        raise GateError(str(exc)) from exc
    if (binding["binding"].get("month") != review_scope["month"]
            or binding["binding"].get("attendance_sha256") != review_scope["attendance_sha256"]
            or identity_source.get("project_binding_sha256") != binding["binding"].get("binding_sha256")):
        raise GateError("PROJECT_BINDING_IDENTITY_SCOPE_MISMATCH")
    source_by_record = {}
    for source_row in source_rows:
        if not isinstance(source_row, dict):
            raise GateError("IDENTITY_MAPPING_CONTENT_INVALID")
        source_id = source_row.get("record_id")
        if not isinstance(source_id, str) or source_id in source_by_record:
            raise GateError("IDENTITY_MAPPING_CONTENT_INVALID")
        source_by_record[source_id] = source_row
    if set(source_by_record) != set(by_record):
        raise GateError("IDENTITY_MAPPING_CONTENT_INVALID")
    for record_id, person in by_record.items():
        source = source_by_record[record_id]
        if source.get("status") != person["status"]:
            raise GateError("IDENTITY_STATUS_CONTENT_MISMATCH")
        if person["status"] == "matched" and source.get("payroll_person_id") != person.get("payroll_person_id"):
            raise GateError("IDENTITY_PERSON_ID_CONTENT_MISMATCH")
        if person["status"] in {"ambiguous", "pending_confirmation"}:
            facts = source.get("facts")
            if (not isinstance(facts, dict) or facts.get("name") != person["name"]
                    or person.get("payroll_person_id") is not None):
                raise GateError("IDENTITY_CONFLICT_SOURCE_MISMATCH")

    exclusion_decisions = _scope_exclusion_decisions(
        evidence, conversation, review_scope, identity_sha, by_record,
    )
    exclusion_by_record = {item["record_id"]: item for item in exclusion_decisions}

    items = readiness.get("items")
    if not isinstance(items, list) or len(items) != len(matched_by_pid):
        raise GateError("READINESS_ITEMS_REQUIRED")
    readiness_by_record: dict[str, dict] = {}
    for item in items:
        if not isinstance(item, dict):
            raise GateError("READINESS_ITEM_INVALID")
        record_id, pid = item.get("record_id"), item.get("payroll_person_id")
        if (not isinstance(record_id, str) or record_id in readiness_by_record
                or isinstance(pid, bool) or not isinstance(pid, int) or pid <= 0
                or matched_by_pid.get(pid) != record_id):
            raise GateError("READINESS_IDENTITY_MAPPING_MISMATCH")
        status = item.get("status")
        if not isinstance(status, str) or status not in {"COVERED", "MISSING_HISTORY", "AMBIGUOUS_HISTORY", "UNKNOWN"}:
            raise GateError("READINESS_STATUS_INVALID")
        source_month = item.get("source_month")
        if status == "COVERED":
            if not isinstance(source_month, str) or not re.fullmatch(r"\d{4}-(0[1-9]|1[0-2])", source_month):
                raise GateError("READINESS_SOURCE_MONTH_INVALID")
            if source_month > review_scope["month"]:
                raise GateError("READINESS_SOURCE_MONTH_AFTER_TARGET")
        elif source_month is not None:
            raise GateError("READINESS_NONCOVERED_SOURCE_MONTH_INVALID")
        if item.get("name") != by_record[record_id]["name"]:
            raise GateError("READINESS_IDENTITY_NAME_MISMATCH")
        readiness_by_record[record_id] = item
    if set(readiness_by_record) != set(matched_by_pid.values()):
        raise GateError("READINESS_IDENTITY_SCOPE_MISMATCH")

    eligible_ids = sorted(
        record_id for record_id, item in readiness_by_record.items() if item["status"] == "COVERED"
    )
    if not eligible_ids:
        raise GateError("NO_COVERED_EXECUTION_SCOPE")
    eligible_pids = sorted(by_record[record_id]["payroll_person_id"] for record_id in eligible_ids)
    selected = evidence.get("execution_scope")
    if not isinstance(selected, dict):
        raise GateError("EXECUTION_SCOPE_REQUIRED")
    if (selected.get("month") != review_scope["month"] or selected.get("version") != review_scope["version"]
            or selected.get("attendance_sha256") != review_scope["attendance_sha256"]
            or selected.get("person_matches_sha256") != identity_sha):
        raise GateError("EXECUTION_SCOPE_STALE")
    selected_ids = selected.get("record_ids")
    if (not isinstance(selected_ids, list)
            or any(not isinstance(item, str) for item in selected_ids)
            or len(selected_ids) != len(set(selected_ids))
            or sorted(selected_ids) != eligible_ids):
        raise GateError("EXECUTION_SCOPE_NOT_UNIQUE_COVERED_SET")
    selected_pids = _positive_id_list(selected.get("payroll_person_ids"), "EXECUTION_PAYROLL_PERSON_IDS")
    if selected_pids != eligible_pids:
        raise GateError("EXECUTION_SCOPE_PERSON_IDS_MISMATCH")
    selected_people = selected.get("people")
    if not isinstance(selected_people, list) or len(selected_people) != len(eligible_ids):
        raise GateError("EXECUTION_SCOPE_PEOPLE_REQUIRED")
    expected_people = [
        {"record_id": rid, "name": by_record[rid]["name"],
         "payroll_person_id": by_record[rid]["payroll_person_id"]}
        for rid in eligible_ids
    ]
    if len({person["name"] for person in expected_people}) != len(expected_people):
        raise GateError("EXECUTION_SCOPE_NAMES_AMBIGUOUS_REQUIRE_CLARIFICATION")
    if selected_people != expected_people:
        raise GateError("EXECUTION_SCOPE_PEOPLE_MISMATCH")
    selected_scope = {**review_scope, "record_ids": eligible_ids}
    try:
        _batch_scope(evidence.get("batch_context"), selected_scope)
    except GateError:
        raise
    batch_context = evidence["batch_context"]
    if batch_context.get("payroll_person_ids") != eligible_pids or batch_context.get("person_matches_sha256") != identity_sha:
        raise GateError("BATCH_EXECUTION_SCOPE_MISMATCH")
    actual_default = readiness_status.get("default_calculable_scope")
    if not isinstance(actual_default, dict) or any((
        actual_default.get("payroll_month") != selected.get("month"),
        actual_default.get("attendance_sha256") != selected.get("attendance_sha256"),
        actual_default.get("person_matches_sha256") != selected.get("person_matches_sha256"),
        actual_default.get("record_ids") != selected.get("record_ids"),
        actual_default.get("payroll_person_ids") != selected.get("payroll_person_ids"),
        actual_default.get("people") != selected.get("people"),
    )):
        raise GateError("READINESS_DEFAULT_SCOPE_MISMATCH")

    exclusions = []
    for record_id, person in by_record.items():
        status = person["status"]
        if status == "unmatched":
            exclusions.append({"record_id": record_id, "name": person["name"], "reason": "身份待核实"})
        elif status in {"ambiguous", "pending_confirmation"}:
            decision = exclusion_by_record[record_id]
            exclusions.append({
                "record_id": record_id, "name": person["name"],
                "reason": f"用户确认本次暂不纳入：{decision['reason']}",
            })
    for record_id, item in readiness_by_record.items():
        if item["status"] != "COVERED":
            reasons = {
                "MISSING_HISTORY": "缺少历史工资", "AMBIGUOUS_HISTORY": "历史工资歧义",
                "UNKNOWN": "历史工资无法核实",
            }
            exclusions.append({"record_id": record_id, "name": item["name"], "reason": reasons[item["status"]]})
    exclusions.sort(key=lambda item: (item["record_id"], item["reason"]))
    exclusions_sha = _text_sha256(json.dumps(exclusion_decisions, ensure_ascii=False, sort_keys=True,
                                            separators=(",", ":"))) if exclusion_decisions else None
    return {
        "scope": selected_scope,
        "person_matches_sha256": identity_sha,
        "payroll_person_ids": eligible_pids,
        "people": expected_people,
        "exclusions": exclusions,
        "scope_sha256": _scope_digest(selected_scope, identity_sha, eligible_pids, readiness_sha, exclusions_sha),
        "scope_exclusion_decisions": exclusion_decisions,
        "scope_exclusions_sha256": exclusions_sha,
        "readiness_normalized_response_sha256": readiness_sha,
        "readiness_report_revalidated": True,
        "payroll_response_sha256_verified": readiness_status.get("payroll_response_sha256_verified") is True,
        "raw_payroll_response_authenticity_verified": False,
        "batch_context": batch_context,
    }


def _validate_project_submission(evidence: dict, scope: dict, batch_context: dict) -> dict:
    attendance_path = evidence.get("attendance_path")
    matches_path = evidence.get("person_matches_path")
    submission_path = evidence.get("submission_request_path")
    if not all(isinstance(path, str) and path for path in (attendance_path, matches_path, submission_path)):
        raise GateError("PROJECT_BINDING_SUBMISSION_EVIDENCE_REQUIRED")
    batch_id = batch_context.get("batch_id")
    if not isinstance(batch_id, str) or not batch_id:
        raise GateError("PROJECT_BINDING_BATCH_ID_REQUIRED")
    try:
        verified = project_binding.validate_submission(
            attendance_path, matches_path, submission_path, scope["record_ids"], scope["month"],
            batch_id, f"submit:{batch_id}",
        )
    except project_binding.ProjectBindingError as exc:
        raise GateError(str(exc)) from exc
    if verified["binding"].get("attendance_sha256") != scope["attendance_sha256"]:
        raise GateError("PROJECT_BINDING_SUBMISSION_ATTENDANCE_MISMATCH")
    return verified


def _execution_intent(text: str, scope_count: int) -> bool:
    if _negated(text) or _uncertain(text):
        return False
    if any(phrase in text for phrase in ("查看工资表", "看一下工资表", "生成工资表")):
        return False
    normalized = re.sub(r"\s+", "", text)
    # Bounded action wording: require a decisive execution verb plus a payroll
    # calculation action, or an established direct calculation form.
    intent = bool(re.search(
        r"(?:开始|启动|进行|执行)(?:本次|此次|这次)?(?:的)?(?:工资|薪资|薪酬)?"
        r"(?:核算|计算)(?:工资|薪资|薪酬)?", normalized,
    )) or any(phrase in normalized for phrase in (
        "核算工资", "开始算工资", "按上述人员核算",
    ))
    if not intent:
        return False
    stated_counts = [int(value) for value in re.findall(r"(?<!\d)(\d+)\s*人", text)]
    return not stated_counts or all(value == scope_count for value in stated_counts)


def _scope_change_intent(
    text: str,
    unchanged_exclusions: list[str] | None = None,
    ambiguous_exclusions: list[str] | None = None,
) -> bool:
    """Recognize bounded, structural requests to alter the displayed people set."""
    normalized = re.sub(r"\s+", "", text)
    normalized = normalized.replace("排除事项", "")
    for person in ambiguous_exclusions or []:
        if not isinstance(person, str) or not person:
            continue
        name = re.escape(person)
        if re.search(
            rf"{name}(?:本次|这次)?(?:仍然?|还是|继续|保持|维持)?"
            rf"(?:暂不|不)(?:纳入|参与|计入)(?:本次|这次)?(?:工资)?核算?",
            normalized,
        ):
            return True
    for person in unchanged_exclusions or []:
        if not isinstance(person, str) or not person:
            continue
        name = re.escape(person)
        # A listed exclusion is only exempt when the message explicitly keeps it
        # excluded. Positive inclusion language must remain visible to the change
        # detector, even when the name is already listed as an exclusion.
        positive_inclusion_patterns = (
            rf"{name}(?:本次|这次)?(?:还是|仍然?|改为|改成|调整为)?"
            rf"(?:纳入|参与|计入|加入)(?:本次|这次)?(?:工资)?核算?",
            rf"(?:把|将){name}(?:也)?(?:算上|纳入|加入|加上|补进|算进去)",
        )
        if any(re.search(pattern, normalized) for pattern in positive_inclusion_patterns):
            return True
        reaffirmation_patterns = (
            rf"(?:继续|保持|维持|仍然?)排除{name}",
            rf"{name}(?:本次|这次)?(?:仍然?|还是|继续|保持|维持)?"
            rf"(?:暂不|不)(?:纳入|参与|计入)(?:本次|这次)?(?:工资)?核算?",
            rf"(?:本次|这次)(?:仍然?|还是|继续)?(?:暂不|不)"
            rf"(?:纳入|参与|计入){name}(?:工资)?核算?",
        )
        if re.search(rf"{name}.{{0,16}}(?:但|不过|除非|如果|若|否则|改为|改成|替换)", normalized):
            return True
        for pattern in reaffirmation_patterns:
            normalized = re.sub(pattern, "", normalized)
    name = r"[\u3400-\u9fffA-Za-z·]{1,16}"
    patterns = (
        rf"(?:把|将){name}(?:也)?(?:算上|纳入|加入|加上|补进|算进去)",
        rf"(?:不要|排除|不算|去掉){name}",
        rf"(?:把|将){name}(?:换成|替换成|改成){name}",
        r"除了[\u3400-\u9fffA-Za-z·]{1,16}[，,、]其他人",
        r"(?:未匹配|未覆盖|未关联)的(?:人员|人)(?:也)?(?:算进去|纳入|加进去|加上)",
        r"(?:另外|再|额外)(?:增加|加入|加上|纳入)(?:\d+|[一二两三四五六七八九十几多]+)(?:个人|名?人|名?人员)",
        r"(?:人员名单|人员范围|处理范围|名单).{0,8}(?:调整|变更|增减|更换|替换)",
        r"(?:调整|变更|增减|更换|替换).{0,8}(?:人员名单|人员范围|处理范围|名单)",
    )
    return any(re.search(pattern, normalized) for pattern in patterns)


def _unambiguous_excluded_names(context: dict) -> list[str]:
    included_names = {person.get("name") for person in context.get("people", [])
                      if isinstance(person, dict) and isinstance(person.get("name"), str)}
    excluded_names = {person.get("name") for person in context.get("exclusions", [])
                      if isinstance(person, dict) and isinstance(person.get("name"), str)}
    return sorted(name for name in excluded_names - included_names if name)


def _ambiguous_excluded_names(context: dict) -> list[str]:
    included_names = {person.get("name") for person in context.get("people", [])
                      if isinstance(person, dict) and isinstance(person.get("name"), str)}
    excluded_names = {person.get("name") for person in context.get("exclusions", [])
                      if isinstance(person, dict) and isinstance(person.get("name"), str)}
    return sorted(name for name in excluded_names & included_names if name)


def _conversation(value: object, user_messages: list[str]) -> list[dict]:
    if not isinstance(value, list) or not value:
        raise GateError("CURRENT_TASK_CONVERSATION_REQUIRED")
    users = []
    for item in value:
        if not isinstance(item, dict) or item.get("role") not in {"user", "assistant"}:
            raise GateError("CURRENT_TASK_CONVERSATION_INVALID")
        content = item.get("content")
        if not isinstance(content, str) or not content.strip():
            raise GateError("CURRENT_TASK_CONVERSATION_INVALID")
        if item["role"] == "user":
            users.append(content)
    if users != user_messages:
        raise GateError("CURRENT_TASK_CONVERSATION_MISMATCH")
    return value


def _scope_display_index(conversation: list[dict], scope: dict, context: dict, supplied: object) -> int:
    if not isinstance(supplied, dict):
        raise GateError("EXECUTION_SCOPE_DISPLAY_REQUIRED")
    index = supplied.get("message_index")
    if isinstance(index, bool) or not isinstance(index, int) or not 0 <= index < len(conversation):
        raise GateError("EXECUTION_SCOPE_DISPLAY_INVALID")
    if conversation[index]["role"] != "assistant":
        raise GateError("EXECUTION_SCOPE_DISPLAY_NOT_ASSISTANT")
    text = conversation[index]["content"]
    compact = _normalize_scope_display(text)
    if "工资核算范围：" not in compact:
        raise GateError("EXECUTION_SCOPE_DISPLAY_MARKER_MISSING")
    count_pattern = rf"(?<!\d){len(context['people'])}人"
    inclusion_start = compact.find("纳入：")
    exclusion_start = compact.find("排除事项：")
    if (scope["month"] not in compact or not re.search(count_pattern, compact)
            or inclusion_start < 0 or exclusion_start < inclusion_start):
        raise GateError("EXECUTION_SCOPE_DISPLAY_SUMMARY_MISMATCH")
    inclusion_end = compact.find("。", inclusion_start)
    if inclusion_end < 0 or inclusion_end > exclusion_start:
        inclusion_end = exclusion_start
    included_text = compact[inclusion_start + len("纳入："):inclusion_end]
    included_names = [name for name in re.split(r"[、，,;；]|和|及", included_text) if name]
    if sorted(included_names) != sorted(person["name"] for person in context["people"]):
        raise GateError("EXECUTION_SCOPE_DISPLAY_PEOPLE_INCOMPLETE")
    exclusions_end = compact.find("后续", exclusion_start)
    if exclusions_end < 0:
        raise GateError("EXECUTION_SCOPE_DISPLAY_SUMMARY_MISMATCH")
    exclusion_text = compact[exclusion_start + len("排除事项："):exclusions_end].strip("。；;")
    displayed_exclusions = [] if exclusion_text in {"", "无", "暂无"} else [
        item.strip("。；;") for item in re.split(r"[；;、]", exclusion_text) if item.strip("。；;")
    ]
    remaining_exclusions = list(context["exclusions"])
    for displayed in displayed_exclusions:
        matches = [index for index, expected in enumerate(remaining_exclusions)
                   if re.fullmatch(
                       re.escape(expected["name"]) + r"\s*[:：]\s*" + re.escape(expected["reason"]),
                       displayed,
                   )]
        if len(matches) != 1:
            raise GateError("EXECUTION_SCOPE_DISPLAY_EXCLUSIONS_INCOMPLETE")
        remaining_exclusions.pop(matches[0])
    if remaining_exclusions:
        raise GateError("EXECUTION_SCOPE_DISPLAY_EXCLUSIONS_INCOMPLETE")
    if supplied.get("scope_sha256") != context["scope_sha256"]:
        raise GateError("EXECUTION_SCOPE_DISPLAY_BINDING_MISMATCH")
    if not all(term in compact for term in ("正式提交考勤", "正式预检查", "工资核算")):
        raise GateError("EXECUTION_SCOPE_SIDE_EFFECTS_NOT_DISCLOSED")
    scope_markers = [
        i for i, item in enumerate(conversation)
        if item["role"] == "assistant" and "工资核算范围" in _normalize_scope_display(item["content"])
    ]
    if not scope_markers or scope_markers[-1] != index:
        raise GateError("EXECUTION_SCOPE_DISPLAY_NOT_LATEST")
    return index


def _authorize_payroll_execution(request: dict, messages: list[str], evidence: dict) -> dict:
    conversation = _conversation(request.get("conversation_messages"), messages)
    context = _execution_context(evidence, conversation)
    operations = _payroll_operation_state(evidence, context["scope"], context["batch_context"])
    _require_no_prior_payroll_write(operations, context["scope"])
    display_index = _scope_display_index(
        conversation, context["scope"], context, evidence.get("scope_display"),
    )
    auth_index = len(messages) - 1
    current_message = messages[auth_index]
    user_positions = [i for i, item in enumerate(conversation) if item["role"] == "user"]
    if user_positions[auth_index] <= display_index:
        raise GateError("EXECUTION_AUTHORIZATION_MUST_FOLLOW_DISPLAY")
    _attendance_confirmation(
        messages, evidence.get("attendance_review"),
        _scope(evidence.get("attendance_review"), "ATTENDANCE_REVIEW", "DECLARED"),
        evidence.get("attendance_confirmation"),
    )
    unchanged_exclusions = _unambiguous_excluded_names(context)
    if _scope_change_intent(
        current_message, unchanged_exclusions, _ambiguous_excluded_names(context),
    ):
        raise GateError("EXECUTION_AUTHORIZATION_SCOPE_CHANGE_REQUIRES_REVIEW")
    if not _execution_intent(current_message, len(context["people"])):
        if re.search(r"(?<!\d)\d+\s*人", current_message):
            raise GateError("EXECUTION_AUTHORIZATION_SCOPE_MISMATCH")
        raise GateError("PAYROLL_EXECUTION_INTENT_REQUIRED")
    return {
        "status": "EXECUTION_AUTHORIZED_FOR_DISPLAYED_SCOPE",
        "authorization_message_index": auth_index,
        "authorization_message_sha256": _text_sha256(current_message),
        "scope_display_message_index": display_index,
        "scope_display_message_sha256": _text_sha256(conversation[display_index]["content"]),
        "scope_sha256": context["scope_sha256"],
        "scope_exclusion_decisions": context["scope_exclusion_decisions"],
        "scope_exclusions_sha256": context["scope_exclusions_sha256"],
        "month": context["scope"]["month"],
        "version": context["scope"]["version"],
        "attendance_sha256": context["scope"]["attendance_sha256"],
        "person_matches_sha256": context["person_matches_sha256"],
        "readiness_normalized_response_sha256": context["readiness_normalized_response_sha256"],
        "readiness_report_revalidated": context["readiness_report_revalidated"],
        "payroll_response_sha256_verified": context["payroll_response_sha256_verified"],
        "raw_payroll_response_authenticity_verified": context["raw_payroll_response_authenticity_verified"],
        "record_ids": context["scope"]["record_ids"],
        "payroll_person_ids": context["payroll_person_ids"],
        "task_id": context["batch_context"]["task_id"],
        "batch_id": context["batch_context"]["batch_id"],
        "message_source_verified": False,
        "is_authorization_proof": False,
        "runtime_enforced": False,
    }


def _validate_execution_authorization(
    request: dict, messages: list[str], evidence: dict, expected_scope: dict | None = None,
) -> tuple[dict, dict]:
    conversation = _conversation(request.get("conversation_messages"), messages)
    context = _execution_context(evidence, conversation)
    token = evidence.get("execution_authorization")
    if not isinstance(token, dict) or token.get("status") != "EXECUTION_AUTHORIZED_FOR_DISPLAYED_SCOPE":
        raise GateError("PAYROLL_EXECUTION_AUTHORIZATION_REQUIRED")
    auth_index = token.get("authorization_message_index")
    display_index = token.get("scope_display_message_index")
    if (isinstance(auth_index, bool) or not isinstance(auth_index, int) or auth_index != len(messages) - 1
            or isinstance(display_index, bool) or not isinstance(display_index, int)):
        raise GateError("PAYROLL_EXECUTION_AUTHORIZATION_STALE")
    user_positions = [i for i, item in enumerate(conversation) if item["role"] == "user"]
    if auth_index != len(messages) - 1 or user_positions[auth_index] <= display_index:
        raise GateError("PAYROLL_EXECUTION_AUTHORIZATION_STALE")
    display = evidence.get("scope_display")
    checked_display = _scope_display_index(conversation, context["scope"], context, display)
    if checked_display != display_index:
        raise GateError("PAYROLL_EXECUTION_AUTHORIZATION_STALE")
    if (token.get("scope_display_message_sha256")
            != _text_sha256(conversation[display_index]["content"])
            or token.get("authorization_message_sha256") != _text_sha256(messages[auth_index])):
        raise GateError("PAYROLL_EXECUTION_AUTHORIZATION_MESSAGE_CHANGED")
    unchanged_exclusions = _unambiguous_excluded_names(context)
    if _scope_change_intent(
        messages[auth_index], unchanged_exclusions, _ambiguous_excluded_names(context),
    ):
        raise GateError("PAYROLL_EXECUTION_AUTHORIZATION_SCOPE_CHANGE_REQUIRES_REVIEW")
    if not _execution_intent(messages[auth_index], len(context["people"])):
        raise GateError("PAYROLL_EXECUTION_AUTHORIZATION_STALE")
    expected = {
        "scope_sha256": context["scope_sha256"],
        "scope_exclusion_decisions": context["scope_exclusion_decisions"],
        "scope_exclusions_sha256": context["scope_exclusions_sha256"],
        "month": context["scope"]["month"], "version": context["scope"]["version"],
        "attendance_sha256": context["scope"]["attendance_sha256"],
        "person_matches_sha256": context["person_matches_sha256"],
        "readiness_normalized_response_sha256": context["readiness_normalized_response_sha256"],
        "readiness_report_revalidated": context["readiness_report_revalidated"],
        "payroll_response_sha256_verified": context["payroll_response_sha256_verified"],
        "raw_payroll_response_authenticity_verified": context["raw_payroll_response_authenticity_verified"],
        "record_ids": context["scope"]["record_ids"],
        "payroll_person_ids": context["payroll_person_ids"],
        "task_id": context["batch_context"]["task_id"],
        "batch_id": context["batch_context"]["batch_id"],
        "message_source_verified": False, "is_authorization_proof": False, "runtime_enforced": False,
    }
    if any(token.get(key) != value for key, value in expected.items()):
        raise GateError("PAYROLL_EXECUTION_AUTHORIZATION_SCOPE_MISMATCH")
    if expected_scope is not None and expected_scope != context["scope"]:
        raise GateError("PAYROLL_EXECUTION_AUTHORIZATION_SCOPE_MISMATCH")
    return context, token


def _attendance_confirmation(
    messages: list[str], review: object, scope: dict, confirmation: object,
) -> dict:
    """Validate a confirmation against the currently visible, byte-bound review."""
    if not isinstance(review, dict):
        raise GateError("ATTENDANCE_REVIEW_EVIDENCE_REQUIRED")
    visible_after = review.get("visible_after_user_message_index")
    if isinstance(visible_after, bool) or not isinstance(visible_after, int) or not 0 <= visible_after < len(messages):
        raise GateError("ATTENDANCE_REVIEW_VISIBILITY_UNVERIFIED")

    candidate = confirmation
    if candidate is None:
        current = messages[-1]
        if not _is_attendance_confirmation(current):
            raise GateError("ATTENDANCE_CONFIRMATION_REQUIRED")
        candidate = {
            "status": "CONFIRMED_FOR_CURRENT_REVIEW",
            **scope,
            "confirmation_message_index": len(messages) - 1,
        }
    if not isinstance(candidate, dict):
        raise GateError("ATTENDANCE_CONFIRMATION_EVIDENCE_INVALID")
    if candidate.get("status") != "CONFIRMED_FOR_CURRENT_REVIEW":
        raise GateError("ATTENDANCE_CONFIRMATION_EVIDENCE_INVALID")

    confirmation_index = candidate.get("confirmation_message_index")
    if (
        isinstance(confirmation_index, bool) or not isinstance(confirmation_index, int)
        or confirmation_index <= visible_after or confirmation_index >= len(messages)
    ):
        raise GateError("ATTENDANCE_CONFIRMATION_NOT_AFTER_CURRENT_REVIEW")
    if {key: candidate.get(key) for key in ("month", "version", "attendance_sha256", "record_ids")} != scope:
        raise GateError("ATTENDANCE_CONFIRMATION_STALE")
    message = messages[confirmation_index]
    payroll_task = _has_payroll_workflow_intent(messages[:confirmation_index])
    if not _is_attendance_confirmation(message, require_project_binding=payroll_task):
        raise GateError("ATTENDANCE_CONFIRMATION_MESSAGE_INVALID")
    return {
        "status": "CONFIRMED_FOR_CURRENT_REVIEW",
        **scope,
        "confirmation_message_index": confirmation_index,
        "message_source_verified": False,
    }


def _require_joint_project_confirmation(evidence: dict, messages: list[str], scope: dict) -> None:
    attendance_path = evidence.get("attendance_path")
    if not isinstance(attendance_path, str) or not attendance_path:
        raise GateError("PROJECT_BINDING_REQUIRED_BEFORE_PAYROLL_CONFIRMATION")
    roster_path, binding_path = project_binding.default_paths(attendance_path)
    try:
        verified = project_binding.validate(attendance_path, roster_path, binding_path)
    except project_binding.ProjectBindingError as exc:
        raise GateError("PROJECT_BINDING_REQUIRED_BEFORE_PAYROLL_CONFIRMATION") from exc
    binding = verified["binding"]
    confirmation = binding.get("user_confirmation")
    if (binding.get("month") != scope["month"]
            or binding.get("attendance_sha256") != scope["attendance_sha256"]
            or not isinstance(confirmation, dict)
            or not project_binding.is_combined_attendance_confirmation(confirmation.get("message", ""))
            or confirmation.get("message") not in messages):
        raise GateError("JOINT_ATTENDANCE_PROJECT_CONFIRMATION_REQUIRED")


def _is_attendance_confirmation(message: str, require_project_binding: bool = False) -> bool:
    """Accept a complete attendance confirmation, including the joined project-binding form."""
    if project_binding.is_combined_attendance_confirmation(message):
        return True
    if require_project_binding:
        return False
    if _negated(message) or _uncertain(message) or "?" in message or "？" in message:
        return False
    normalized = re.sub(r"[\s，,。；;：:！？!?]", "", message)
    return normalized in {
        "确认考勤", "我确认考勤", "好的确认考勤", "好确认考勤",
        "考勤确认", "我考勤确认", "考勤没问题", "考勤没有问题",
    }


def _has_payroll_workflow_intent(messages: list[str]) -> bool:
    """Detect an existing explicit payroll-processing request, not a write authorization."""
    nouns = ("工资", "薪资", "薪酬")
    actions = ("核算", "计算", "工资覆盖", "历史工资", "薪资覆盖", "薪酬覆盖")
    return any(any(noun in message for noun in nouns) and any(action in message for action in actions)
               for message in messages)


def _verify_current_attendance(path: object, review_scope: dict) -> None:
    if not isinstance(path, str) or not path.strip():
        raise GateError("CURRENT_ATTENDANCE_PATH_REQUIRED")
    current = Path(path)
    if current.is_symlink() or not current.is_file():
        raise GateError("CURRENT_ATTENDANCE_FILE_UNAVAILABLE")
    try:
        digest = hashlib.sha256(current.read_bytes()).hexdigest()
    except OSError as exc:
        raise GateError("CURRENT_ATTENDANCE_FILE_UNAVAILABLE") from exc
    if digest != review_scope["attendance_sha256"]:
        raise GateError("ATTENDANCE_REVIEW_CONTENT_MISMATCH")


def _verify_current_identity(path: object, expected_sha256: str) -> None:
    if not isinstance(path, str) or not path.strip():
        raise GateError("CURRENT_IDENTITY_PATH_REQUIRED")
    current = Path(path)
    if current.is_symlink() or not current.is_file():
        raise GateError("CURRENT_IDENTITY_FILE_UNAVAILABLE")
    try:
        digest = hashlib.sha256(current.read_bytes()).hexdigest()
    except OSError as exc:
        raise GateError("CURRENT_IDENTITY_FILE_UNAVAILABLE") from exc
    if digest != expected_sha256:
        raise GateError("IDENTITY_MAPPING_CONTENT_MISMATCH")


def _evaluate(request: dict) -> dict:
    action = request.get("action")
    if action not in STAGES:
        raise GateError("ACTION_INVALID")
    messages = _user_messages(request.get("user_messages"))
    current_message = messages[-1]
    evidence = request.get("evidence", {})
    if not isinstance(evidence, dict):
        raise GateError("EVIDENCE_MUST_BE_OBJECT")
    result = {
        "ok": False,
        "action": action,
        "status": "WORKFLOW_BLOCKED",
        "error": "STAGE_PREREQUISITE_NOT_MET",
        "is_authorization_proof": False,
        "message_source_verified": False,
        "runtime_enforced": False,
        "next_action": "STOP_AND_FOLLOW_SKILL_ROUTE",
    }
    review = evidence.get("attendance_review")
    identity = evidence.get("identity_matching")
    readiness = evidence.get("salary_readiness")
    scope_review = evidence.get("submission_scope_review")
    submission = evidence.get("submission_receipt")
    precheck = evidence.get("payroll_precheck")
    calculation = evidence.get("calculation_receipt")

    if action != "ATTENDANCE_REVIEW" or (isinstance(review, dict) and review.get("status") == "DECLARED"):
        current_scope = _scope(review, "ATTENDANCE_REVIEW", "DECLARED")
        _verify_current_attendance(evidence.get("attendance_path"), current_scope)

    if action == "ATTENDANCE_REVIEW":
        if isinstance(review, dict) and review.get("status") == "DECLARED":
            if any(word in current_message for word in ("更正", "修改考勤", "修改出勤", "撤销修改")):
                return {**result, "ok": True, "status": "STAGE_ADMITTED", "error": None,
                        "next_action": "APPLY_CONTROLLED_CORRECTION_REEXPORT_AND_STOP"}
            result["error"] = "ATTENDANCE_REVIEW_ALREADY_COMPLETE"
            return result
        if not any("考勤" in message and any(word in message for word in ("识别", "核对", "确认表")) for message in messages):
            result["error"] = "ATTENDANCE_REVIEW_NOT_REQUESTED"
            return result
        next_action = (
            "PREPARE_CURRENT_ROSTER_AND_DISPLAY_REVIEW_WITH_ALL_PROJECT_BINDINGS_THEN_WAIT_FOR_ONE_CONFIRMATION"
            if _has_payroll_workflow_intent(messages)
            else "DELIVER_ATTENDANCE_REVIEW_AND_STOP"
        )
        return {**result, "ok": True, "status": "STAGE_ADMITTED", "error": None,
                "next_action": next_action}

    if action == "IDENTITY_MATCHING":
        review_scope = _scope(review, "ATTENDANCE_REVIEW", "DECLARED")
        match_words = ("人员匹配", "匹配人员", "身份匹配")
        explicit_current_match = any(word in current_message for word in match_words)
        prior_match_directive = any(any(word in message for word in match_words) for message in messages[:-1])
        if not (explicit_current_match or prior_match_directive or
                _has_payroll_workflow_intent(messages[:-1])):
            result["error"] = "IDENTITY_MATCHING_NOT_EXPLICITLY_REQUESTED"
            return result
        try:
            confirmed = _attendance_confirmation(
                messages, review, review_scope, evidence.get("attendance_confirmation"),
            )
            if _has_payroll_workflow_intent(messages[:confirmed["confirmation_message_index"]]):
                _require_joint_project_confirmation(evidence, messages, review_scope)
        except GateError as exc:
            result["error"] = str(exc)
            return result
        return {**result, "ok": True, "status": "STAGE_ADMITTED", "error": None,
                "attendance_confirmation_evidence": confirmed,
                "next_action": "MATCH_IDENTITIES_THEN_STOP_BEFORE_SUBMISSION"}

    if action == "SALARY_READINESS":
        review_scope = _scope(review, "ATTENDANCE_REVIEW", "DECLARED")
        identity_scope = _scope(identity, "IDENTITY_MATCHING", "COMPLETE")
        if review_scope != identity_scope:
            result["error"] = "IDENTITY_SCOPE_MISMATCH"
            return result
        try:
            confirmed = _attendance_confirmation(
                messages, review, review_scope, evidence.get("attendance_confirmation"),
            )
            if _has_payroll_workflow_intent(messages[:confirmed["confirmation_message_index"]]):
                _require_joint_project_confirmation(evidence, messages, review_scope)
        except GateError as exc:
            result["error"] = str(exc)
            return result
        if not _has_payroll_workflow_intent(messages):
            result["error"] = "SALARY_WORKFLOW_NOT_REQUESTED"
            return result
        return {**result, "ok": True, "status": "STAGE_ADMITTED", "error": None,
                "attendance_confirmation_evidence": confirmed,
                "next_action": "RUN_READ_ONLY_COVERAGE_CHECK_THEN_STOP"}

    if action == "PAYROLL_EXECUTION":
        try:
            authorization = _authorize_payroll_execution(request, messages, evidence)
        except GateError as exc:
            result["error"] = str(exc)
            return result
        return {**result, "ok": True, "status": "EXECUTION_AUTHORIZATION_ACCEPTED", "error": None,
                "execution_authorization": authorization,
                "next_action": "RECHECK_COVERAGE_AND_BATCH_THEN_SUBMIT_SAME_SCOPE"}

    if action == "SUBMISSION_SCOPE_REVIEW":
        base = _scope(readiness, "SALARY_READINESS", "COVERAGE_CHECKED")
        review_scope = _scope(review, "ATTENDANCE_REVIEW", "DECLARED")
        identity_scope = _scope(identity, "IDENTITY_MATCHING", "COMPLETE")
        version_binding = ("month", "version", "attendance_sha256")
        if any(base[key] != review_scope[key] for key in version_binding):
            result["error"] = "READINESS_REVIEW_SCOPE_MISMATCH"
            return result
        if any(identity_scope[key] != review_scope[key] for key in version_binding):
            result["error"] = "IDENTITY_REVIEW_SCOPE_MISMATCH"
            return result
        if _negated(current_message) or _uncertain(current_message) or not any(
            word in current_message for word in ("本次范围", "处理范围", "纳入人员", "就按这")
        ):
            result["error"] = "SCOPE_NOT_EXPLICITLY_CONFIRMED"
            return result
        proposed = request.get("proposed_scope")
        proposed_scope = _scope(
            {"status": "CONFIRMED", **(proposed if isinstance(proposed, dict) else {})},
            "PROPOSED_SCOPE", "CONFIRMED",
        )
        if any(proposed_scope[key] != review_scope[key] for key in version_binding):
            result["error"] = "PROPOSED_SCOPE_VERSION_MISMATCH"
            return result
        if not set(proposed_scope["record_ids"]).issubset(identity_scope["record_ids"]):
            result["error"] = "PROPOSED_SCOPE_CONTAINS_UNMATCHED_RECORDS"
            return result
        if not set(proposed_scope["record_ids"]).issubset(base["record_ids"]):
            result["error"] = "PROPOSED_SCOPE_NOT_COVERED_BY_READINESS_CHECK"
            return result
        labels = _person_labels(proposed, proposed_scope, "PROPOSED_SCOPE")
        if not _contains_all(current_message, proposed_scope, labels):
            result["error"] = "SCOPE_MESSAGE_DOES_NOT_BIND_MONTH_VERSION_AND_PEOPLE"
            return result
        return {**result, "ok": True, "status": "STAGE_ADMITTED", "error": None,
                "next_action": "RECHECK_COVERAGE_FOR_EXACT_SCOPE_THEN_STOP",
                "scope_confirmation_evidence": {
                    "status": "CONFIRMED", **proposed_scope,
                    "people": proposed["people"], "confirmation_message_index": len(messages) - 1,
                }}

    if action == "ATTENDANCE_SUBMISSION":
        if "execution_authorization" in evidence:
            try:
                context, _ = _validate_execution_authorization(request, messages, evidence)
            except GateError as exc:
                result["error"] = str(exc)
                return result
            selected = context["scope"]
            try:
                batch_context = _batch_scope(evidence.get("batch_context"), selected)
                operations = _payroll_operation_state(evidence, selected, batch_context)
                _require_no_prior_payroll_write(operations, selected)
                validated_submission = _validate_project_submission(evidence, selected, batch_context)
            except GateError as exc:
                result["error"] = str(exc)
                return result
            return {**result, "ok": True, "status": "WRITE_STAGE_ELIGIBLE", "error": None,
                    "authorization_text_checked": True,
                    "execution_authorization_reused": True,
                    "submission_scope_review": {
                        "status": "EXECUTION_AUTHORIZED", **selected,
                        "people": context["people"],
                        "execution_scope_sha256": context["scope_sha256"],
                    },
                    "validated_submission_request": validated_submission["submission_request"],
                    "submission_request_sha256": validated_submission["submission_request_sha256"],
                    "next_action": "PASS_VALIDATED_SUBMISSION_REQUEST_VERBATIM_TO_MCP_AND_READ_BACK_FIXED_OPERATION"}

        selected = _scope(scope_review, "SUBMISSION_SCOPE_REVIEW", "CONFIRMED")
        final_readiness = _scope(readiness, "SALARY_READINESS", "COVERAGE_CHECKED")
        if selected != final_readiness:
            result["error"] = "FINAL_SCOPE_COVERAGE_CHECK_REQUIRED"
            return result
        if _negated(current_message) or _uncertain(current_message) or not any(
            word in current_message for word in ("正式提交考勤", "授权提交考勤", "同意正式提交")
        ):
            result["error"] = "FORMAL_SUBMISSION_AUTHORIZATION_REQUIRED"
            return result
        if any(word in current_message for word in ("开始工资核算", "授权工资核算", "同意核算")):
            result["error"] = "SEPARATE_WRITE_AUTHORIZATION_REQUIRED"
            return result
        if not _contains_all(current_message, selected, _person_labels(scope_review, selected, "SUBMISSION_SCOPE_REVIEW")):
            result["error"] = "SUBMISSION_AUTHORIZATION_SCOPE_MISMATCH"
            return result
        confirmation_index = scope_review.get("confirmation_message_index")
        scope_labels = _person_labels(scope_review, selected, "SUBMISSION_SCOPE_REVIEW")
        if (
            isinstance(confirmation_index, bool) or not isinstance(confirmation_index, int)
            or confirmation_index < 0 or confirmation_index >= len(messages) - 1
            or _negated(messages[confirmation_index])
            or not any(word in messages[confirmation_index] for word in ("本次范围", "处理范围", "纳入人员", "就按这"))
            or not _contains_all(messages[confirmation_index], selected, scope_labels)
        ):
            result["error"] = "PRIOR_EXACT_SCOPE_CONFIRMATION_REQUIRED"
            return result
        try:
            batch_context = _batch_scope(evidence.get("batch_context"), selected)
            operations = _payroll_operation_state(evidence, selected, batch_context)
            _require_no_prior_payroll_write(operations, selected)
            validated_submission = _validate_project_submission(evidence, selected, batch_context)
        except GateError as exc:
            result["error"] = str(exc)
            return result
        return {**result, "ok": True, "status": "WRITE_STAGE_ELIGIBLE", "error": None,
                "authorization_text_checked": True,
                "validated_submission_request": validated_submission["submission_request"],
                "submission_request_sha256": validated_submission["submission_request_sha256"],
                "next_action": "PASS_VALIDATED_SUBMISSION_REQUEST_VERBATIM_TO_MCP_AND_READ_BACK_FIXED_OPERATION"}

    if action == "PAYROLL_PRECHECK":
        submitted = _scope(submission, "SUBMISSION_RECEIPT", "SUCCEEDED")
        execution_mode = "execution_authorization" in evidence
        if execution_mode:
            try:
                context, _ = _validate_execution_authorization(request, messages, evidence)
            except GateError as exc:
                result["error"] = str(exc)
                return result
            scope = context["scope"]
        else:
            scope = _scope(scope_review, "SUBMISSION_SCOPE_REVIEW", "CONFIRMED")
        try:
            batch_context = _batch_scope(evidence.get("batch_context"), scope)
        except GateError as exc:
            result["error"] = str(exc)
            return result
        try:
            operations = _payroll_operation_state(evidence, scope, batch_context)
            _require_current_operation_scope(operations["submission"], scope, "SUBMISSION")
            if operations["calculation"]["status"] == "SUCCEEDED":
                _require_current_operation_scope(operations["calculation"], scope, "CALCULATION")
                raise GateError("PAYROLL_CALCULATION_ALREADY_EXISTS")
        except GateError as exc:
            result["error"] = str(exc)
            return result
        expected_batch_id = batch_context["batch_id"]
        if (
            submitted != scope
            or submission.get("batch_id") != expected_batch_id
            or submission.get("operation_id") != f"submit:{expected_batch_id}"
            or submission.get("operation_id") != request.get("expected_submit_operation_id")
        ):
            result["error"] = "SUCCESSFUL_SUBMISSION_RECEIPT_REQUIRED"
            return result
        return {**result, "ok": True, "status": "STAGE_ADMITTED", "error": None,
                "next_action": (
                    "RUN_PRECHECK_THEN_CALCULATE_SAME_SCOPE_IF_VALID"
                    if execution_mode else "RUN_PRECHECK_FOR_SUBMITTED_BATCH_THEN_STOP"
                )}

    if action == "PAYROLL_CALCULATION":
        checked = _scope(precheck, "PAYROLL_PRECHECK", "VALID")
        submitted = _scope(submission, "SUBMISSION_RECEIPT", "SUCCEEDED")
        if "execution_authorization" in evidence:
            try:
                context, _ = _validate_execution_authorization(request, messages, evidence)
            except GateError as exc:
                result["error"] = str(exc)
                return result
            scope = context["scope"]
            if checked != scope or submitted != scope or precheck.get("valid") is not True:
                result["error"] = "VALID_PRECHECK_FOR_SUBMITTED_SCOPE_REQUIRED"
                return result
            try:
                batch_context = _batch_scope(evidence.get("batch_context"), scope)
            except GateError as exc:
                result["error"] = str(exc)
                return result
            try:
                operations = _payroll_operation_state(evidence, scope, batch_context)
                _require_current_operation_scope(operations["submission"], scope, "SUBMISSION")
                if operations["calculation"]["status"] == "SUCCEEDED":
                    _require_current_operation_scope(operations["calculation"], scope, "CALCULATION")
                    raise GateError("PAYROLL_CALCULATION_ALREADY_EXISTS")
            except GateError as exc:
                result["error"] = str(exc)
                return result
            precheck_operation = f"precheck:{batch_context['batch_id']}"
            if (precheck.get("batch_id") != batch_context["batch_id"]
                    or precheck.get("operation_id") != precheck_operation
                    or precheck_operation != request.get("expected_precheck_operation_id")):
                result["error"] = "PRECHECK_BATCH_OR_OPERATION_ID_MISMATCH"
                return result
            if request.get("expected_calculate_operation_id") != f"calculate:{batch_context['batch_id']}":
                result["error"] = "CALCULATE_OPERATION_ID_MISMATCH"
                return result
            return {**result, "ok": True, "status": "WRITE_STAGE_ELIGIBLE", "error": None,
                    "authorization_text_checked": True, "execution_authorization_reused": True,
                    "next_action": "VERIFY_BATCH_RECEIPT_THEN_CALCULATE_SAME_SCOPE"}
        if checked != submitted or precheck.get("valid") is not True:
            result["error"] = "VALID_PRECHECK_FOR_SUBMITTED_SCOPE_REQUIRED"
            return result
        if _negated(current_message) or _uncertain(current_message) or not any(
            word in current_message for word in ("开始工资核算", "授权工资核算", "同意开始核算")
        ):
            result["error"] = "SEPARATE_PAYROLL_CALCULATION_AUTHORIZATION_REQUIRED"
            return result
        if any(word in current_message for word in ("正式提交考勤", "授权提交考勤", "同意正式提交")):
            result["error"] = "SEPARATE_WRITE_AUTHORIZATION_REQUIRED"
            return result
        if not _contains_all(current_message, checked, _person_labels(precheck, checked, "PAYROLL_PRECHECK")):
            result["error"] = "CALCULATION_AUTHORIZATION_SCOPE_MISMATCH"
            return result
        try:
            batch_context = _batch_scope(evidence.get("batch_context"), submitted)
        except GateError as exc:
            result["error"] = str(exc)
            return result
        try:
            operations = _payroll_operation_state(evidence, submitted, batch_context)
            _require_current_operation_scope(operations["submission"], submitted, "SUBMISSION")
            if operations["calculation"]["status"] == "SUCCEEDED":
                _require_current_operation_scope(operations["calculation"], submitted, "CALCULATION")
                raise GateError("PAYROLL_CALCULATION_ALREADY_EXISTS")
        except GateError as exc:
            result["error"] = str(exc)
            return result
        precheck_operation = f"precheck:{batch_context['batch_id']}"
        if (
            precheck.get("batch_id") != batch_context["batch_id"]
            or precheck.get("operation_id") != precheck_operation
            or precheck_operation != request.get("expected_precheck_operation_id")
        ):
            result["error"] = "PRECHECK_BATCH_OR_OPERATION_ID_MISMATCH"
            return result
        if request.get("expected_calculate_operation_id") != f"calculate:{batch_context['batch_id']}":
            result["error"] = "CALCULATE_OPERATION_ID_MISMATCH"
            return result
        return {**result, "ok": True, "status": "WRITE_STAGE_ELIGIBLE", "error": None,
                "authorization_text_checked": True,
                "next_action": "VERIFY_MESSAGE_IN_CURRENT_CONVERSATION_BEFORE_CALCULATE"}

    if "execution_authorization" in evidence:
        try:
            context, _ = _validate_execution_authorization(request, messages, evidence)
        except GateError as exc:
            result["error"] = str(exc)
            return result
        calculated = _scope(calculation, "CALCULATION_RECEIPT", "SUCCEEDED")
        submitted = _scope(submission, "SUBMISSION_RECEIPT", "SUCCEEDED")
        if calculated != context["scope"] or submitted != context["scope"]:
            result["error"] = "SUCCESSFUL_CALCULATION_RECEIPT_REQUIRED"
            return result
        try:
            batch_context = _batch_scope(evidence.get("batch_context"), context["scope"])
        except GateError as exc:
            result["error"] = str(exc)
            return result
        try:
            operations = _payroll_operation_state(evidence, context["scope"], batch_context)
            _require_current_operation_scope(operations["submission"], context["scope"], "SUBMISSION")
            _require_current_operation_scope(operations["calculation"], context["scope"], "CALCULATION")
        except GateError as exc:
            result["error"] = str(exc)
            return result
        expected_batch_id = batch_context["batch_id"]
        if (calculation.get("batch_id") != expected_batch_id
                or calculation.get("operation_id") != f"calculate:{expected_batch_id}"
                or calculation.get("operation_id") != request.get("expected_calculate_operation_id")):
            result["error"] = "SUCCESSFUL_CALCULATION_RECEIPT_REQUIRED"
            return result
        scope_review_value = scope_review
        if not isinstance(scope_review_value, dict) or scope_review_value.get("status") != "EXECUTION_AUTHORIZED":
            result["error"] = "EXECUTION_AUTHORIZED_SCOPE_REQUIRED_FOR_EXPORT"
            return result
        try:
            export_scope = _scope(scope_review_value, "SUBMISSION_SCOPE_REVIEW", "EXECUTION_AUTHORIZED")
        except GateError as exc:
            result["error"] = str(exc)
            return result
        if export_scope != context["scope"]:
            result["error"] = "EXECUTION_EXPORT_SCOPE_MISMATCH"
            return result
        return {**result, "ok": True, "status": "STAGE_ADMITTED", "error": None,
                "next_action": "GENERATE_AND_DECLARE_REAL_PAYROLL_OUTPUT"}

    calculated = _scope(calculation, "CALCULATION_RECEIPT", "SUCCEEDED")
    submitted = _scope(submission, "SUBMISSION_RECEIPT", "SUCCEEDED")
    try:
        batch_context = _batch_scope(evidence.get("batch_context"), submitted)
    except GateError as exc:
        result["error"] = str(exc)
        return result
    try:
        operations = _payroll_operation_state(evidence, submitted, batch_context)
        _require_current_operation_scope(operations["submission"], submitted, "SUBMISSION")
        _require_current_operation_scope(operations["calculation"], submitted, "CALCULATION")
    except GateError as exc:
        result["error"] = str(exc)
        return result
    expected_batch_id = batch_context["batch_id"]
    if (
        calculated != submitted
        or calculation.get("batch_id") != expected_batch_id
        or calculation.get("operation_id") != f"calculate:{expected_batch_id}"
        or calculation.get("operation_id") != request.get("expected_calculate_operation_id")
    ):
        result["error"] = "SUCCESSFUL_CALCULATION_RECEIPT_REQUIRED"
        return result
    return {**result, "ok": True, "status": "STAGE_ADMITTED", "error": None,
            "next_action": "GENERATE_AND_DECLARE_REAL_PAYROLL_OUTPUT"}


def evaluate(request: dict) -> dict:
    try:
        return _evaluate(request)
    except GateError as exc:
        return {
            "ok": False,
            "status": "WORKFLOW_BLOCKED",
            "error": str(exc),
            "is_authorization_proof": False,
            "message_source_verified": False,
            "runtime_enforced": False,
        }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--request", required=True, help="JSON containing current task user messages and stage evidence")
    args = parser.parse_args(argv)
    try:
        result = evaluate(_read(args.request))
    except GateError as exc:
        result = {"ok": False, "status": "WORKFLOW_BLOCKED", "error": str(exc),
                  "is_authorization_proof": False, "message_source_verified": False, "runtime_enforced": False}
    print(json.dumps(result, ensure_ascii=False, sort_keys=True))
    return 0 if result["ok"] else 2


if __name__ == "__main__":
    sys.exit(main())

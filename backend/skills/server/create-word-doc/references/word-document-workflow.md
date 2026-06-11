# Word Document Workflow

## Document Brief

Start by resolving the minimum brief needed to write a useful document:

- Purpose: inform, persuade, decide, record, train, sell, or comply.
- Audience: executives, operators, engineers, customers, regulators, students, or general readers.
- Tone: formal, concise, instructional, analytical, persuasive, or neutral.
- Length: one-page memo, short brief, standard report, long-form guide, or custom page/word count.
- Inputs: source notes, URLs, meeting transcript, draft text, data tables, brand/template files, or examples.
- Constraints: required language, confidentiality, citation standard, approval workflow, or formatting template.

When the user does not specify these, infer conservative defaults from the task and state them briefly.

## Outline Design

Use an outline that reflects the document's job:

- Memo: title, context, recommendation, rationale, risks, next steps.
- Report: title page, executive summary, background, findings, analysis, recommendations, appendix.
- Proposal: problem, goals, approach, scope, timeline, deliverables, pricing or resource plan, acceptance criteria.
- Guide/manual: overview, prerequisites, step-by-step procedures, examples, troubleshooting, reference.
- Policy: purpose, scope, definitions, policy statements, roles, procedures, exceptions, review cadence.

For long documents, keep each top-level section responsible for one question. Add subsections only when they improve navigation.

## Content Expansion

Expand from outline to draft section by section:

- Lead with the point, then add explanation and evidence.
- Convert vague claims into concrete observations, decisions, risks, or actions.
- Add examples only when they clarify how the idea works in practice.
- Use tables for information that readers compare: options, risks, roles, timelines, requirements, costs, or decisions.
- Add callouts sparingly for important notes, assumptions, or dependencies.
- Keep source-backed claims traceable. If sources are unavailable, label claims as assumptions or recommendations.

Avoid filler patterns: repetitive introductions, generic benefits, unsupported superlatives, and redundant closing paragraphs.

## Editing Passes

Run focused passes instead of trying to fix everything at once:

1. Structure pass: confirm section order, heading hierarchy, and missing sections.
2. Substance pass: check whether claims are supported and useful for the audience.
3. Clarity pass: shorten sentences, remove repetition, and replace vague terms.
4. Formatting pass: normalize styles, spacing, tables, captions, headers, footers, and page breaks.
5. Delivery pass: verify filenames, metadata, visible placeholders, and layout.

When editing an existing draft, preserve the user's argument and voice unless the request asks for a rewrite.

## DOCX Implementation Notes

Prefer style-based generation:

- Use built-in heading styles so navigation panes and tables of contents can work.
- Define document-level font, size, and spacing before adding content.
- Use section margins that match the document type; do not rely blindly on defaults.
- Set table widths and repeat header rows for multi-page tables when supported.
- Keep images proportional and below page width.
- Put version, confidentiality labels, or page numbers in headers/footers only when useful.

For existing DOCX files:

- Preserve the original file by writing a new output path.
- Search paragraphs, tables, headers, and footers for placeholders.
- Avoid rewriting the whole document if a small targeted XML or placeholder edit preserves more formatting.
- Inspect rendered output after changes, especially around tables and page breaks.

## Final Review Checklist

- The document answers the user's requested job.
- The title and first page make the document's purpose clear.
- Heading levels are consistent.
- Tables and images fit within margins.
- Page breaks are not awkward or empty.
- No unresolved placeholders remain unless intentional.
- Claims that need sources are cited, attributed, or marked as assumptions.
- The DOCX opens cleanly and, when possible, has been rendered for visual inspection.

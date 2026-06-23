---
name: find-skills
description: Helps users discover and install agent skills when they ask questions like "how do I do X", "find a skill for X", "is there a skill that can...", or express interest in extending capabilities. This skill should be used when the user is looking for functionality that might exist as an installable skill.
---

# Find & Install Skills

This skill helps you discover and install skills from the open Agent skill ecosystem.

## When to Trigger

Trigger when the user:

- Asks "how do I do X" where X could be a common task with an existing skill
- Says "find a skill for X" or "is there a skill that can..."
- Asks "can you do X" where X falls under a specialized skill domain
- Expresses interest in extending agent capabilities
- Wants to search for tools, templates, or workflows
- Mentions needing help in a specific domain (design, testing, deployment, etc.)

## Skill Management

The Leros CLI provides skill management:

```
leros skill install <identifier>  Install a skill. Supports three identifier formats:
                                  - Short name (e.g. code-review)
                                  - GitHub path (e.g. owner/repo/path)
                                  - Direct URL (e.g. https://.../SKILL.md)
leros skill search <query>        Search remote skills
```

| Flag       | Scope             | Description                                  |
| ---------- | ----------------- | -------------------------------------------- |
| `--json`   | install / search  | JSON output                                  |
| `--force`  | install           | Overwrite existing skill                     |
| `--yes`    | install           | Skip confirmation prompts                    |
| `--limit`  | search            | Max number of results (default 10)           |
| `--source` | install / search  | Limit to a specific source (Leros/ClawHub/SkillsSh) |

## How to Help Users Find Skills

### Step 1: Understand the Need

When the user asks for help, identify:

1. **Domain** (e.g. React, testing, design, deployment)
2. **Specific task** (e.g. write tests, create animations, review PRs)
3. **Whether the task is common** enough that a skill likely exists

### Step 2: Search for Skills

Use `leros skill search` to search across skill marketplaces:

```bash
leros skill search <query>
```

This command searches **multiple sources** automatically:

1. **Leros** — SingerOS built-in skill marketplace
2. **ClawHub** — [clawhub.ai](https://clawhub.ai/) open skill ecosystem. Browse at https://clawhub.com/
3. **SkillsSh** — [skills.sh](https://skills.sh/) global open-source skill index. Browse at https://skills.sh/

Examples:

- User asks "how do I make my React app faster?" → `leros skill search react+performance`
- User asks "can you review my PR?" → `leros skill search pr+review`
- User asks "I need to generate a changelog" → `leros skill search changelog`

Search results in JSON format include the following fields:

- `name` — skill name
- `author` — author
- `identifier` — skill identifier
- `description` — description
- `source` — source (Leros / SkillsSh / ClawHub)
- `version` — version number
- `installs` — install count

Use `--json`, `--limit`, and `--source` flags to control output format and result count:

```bash
leros skill search --json --limit 20 <query>
leros skill search --source SkillsSh <query>
```

### Step 3: Verify Quality

**Don't recommend a skill based on search results alone.** Always check:

1. **Installs** — prefer skills with higher install counts
2. **Source reliability** — skills from trusted sources (e.g. official sources like leros, anthropics, microsoft) are more reliable
3. **GitHub stars** — check the skill's source repository. Be skeptical of skills from repos with fewer than 100 stars.

### Step 4: Present Options to the User

After finding relevant skills, show the user:

1. Skill name and description
2. Install count and source
3. Ready-to-run install command

Example response:

```
I found a skill that might help!

**code-review** — AI-powered code review that finds bugs and performance issues.
  Source: Leros marketplace | Installs: 5K+

Install command:
leros skill install code-review
```

If multiple results are relevant, list the top 3-5 and let the user choose.

### Step 5: Install the Skill

If the user decides to install, run:

```bash
leros skill install --yes <identifier>
```

`--yes` skips the confirmation prompt.

**Post-install verification**: confirm the skill was installed correctly:

```bash
ls <workspace_root>/.leros/skills/<name>/
```

If installed successfully, you should see a `SKILL.md` file. You can also list all installed skills:

```bash
ls <workspace_root>/.leros/skills/
```

## Common Skill Categories

Use these categories as reference when searching:

| Category      | Example Keywords                        |
| ------------- | --------------------------------------- |
| Web Dev       | react, nextjs, typescript, css, tailwind |
| Testing       | testing, jest, playwright, e2e          |
| DevOps        | deploy, docker, kubernetes, ci-cd       |
| Documentation | docs, readme, changelog, api-docs       |
| Code Quality  | review, lint, refactor, best-practices  |
| Design        | ui, ux, design-system, accessibility    |
| Productivity  | workflow, automation, git               |

## Search Tips

1. **Use specific keywords**: "react testing" works better than just "testing"
2. **Try both English and Chinese**: the skill marketplace supports semantic search in both languages; if one language returns no results, try the other
3. **Try synonyms**: if "deploy" returns nothing, try "deployment" or "ci-cd"

## When No Skills Are Found

If no relevant skills are found:

1. Tell the user no existing skill was found
2. Offer to help them with the task directly using general capabilities
3. Suggest they create a custom skill (see `skill-creator` skill)

Example:

```
I searched for skills related to "xxx" but didn't find a match.
I can still help you with this task directly! Would you like me to proceed?

If you do this often, you might also consider creating a custom skill.
See skill-creator for guidance on how to do that.
```

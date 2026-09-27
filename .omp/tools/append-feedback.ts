import type { CustomToolFactory } from "@oh-my-pi/pi-coding-agent";
import * as path from "node:path";
import * as fs from "node:fs/promises";
import * as os from "node:os";

function expandHome(p: string): string {
  return p.startsWith("~") ? path.join(os.homedir(), p.slice(1)) : p;
}

/**
 * Parse a YAML-ish list of values following a `customDirectories:` key.
 * Looks for lines like `- /some/path` indented under the key.
 */
function parseCustomDirectories(yaml: string): string[] {
  const dirs: string[] = [];
  const lines = yaml.split("\n");
  let inList = false;
  let listIndent = 0;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const indent = line.length - line.trimStart().length;
    const trimmed = line.trimStart();

    if (trimmed.startsWith("customDirectories:")) {
      inList = true;
      listIndent = indent;
      continue;
    }

    if (inList) {
      // Empty/comment line inside the list — keep looking
      if (trimmed === "" || trimmed.startsWith("#")) continue;
      // Less or equal indent + non-list content = left the section
      if (indent <= listIndent && !trimmed.startsWith("- ")) break;
      if (trimmed.startsWith("- ")) {
        dirs.push(trimmed.slice(2).trim());
      }
    }
  }

  return dirs;
}

async function loadCustomDirectories(configPath: string): Promise<string[]> {
  try {
    const content = await fs.readFile(configPath, "utf-8");
    return parseCustomDirectories(content);
  } catch {
    return [];
  }
}

async function getSkillRoots(cwd: string): Promise<string[]> {
  const roots: string[] = [];

  // From cwd/.omp/config.yml skills.customDirectories
  for (const dir of await loadCustomDirectories(path.join(cwd, ".omp", "config.yml"))) {
    roots.push(expandHome(dir));
  }

  // From ~/.omp/agent/config.yml skills.customDirectories
  for (const dir of await loadCustomDirectories(path.join(os.homedir(), ".omp", "agent", "config.yml"))) {
    roots.push(expandHome(dir));
  }

  // Static convention-based directories (may overlap with config — dedup later)
  roots.push(
    path.join(cwd, ".omp", "skills"),
    path.join(cwd, ".agents", "skills"),
    path.join(os.homedir(), ".agents", "skills"),
    path.join(os.homedir(), ".omp", "agent", "skills"),
  );

  // Deduplicate
  const seen = new Set<string>();
  return roots.filter((r) => {
    const normalized = path.resolve(r);
    if (seen.has(normalized)) return false;
    seen.add(normalized);
    return true;
  });
}

async function hasSkillMd(dir: string): Promise<boolean> {
  try {
    await fs.access(path.join(dir, "SKILL.md"));
    return true;
  } catch {
    return false;
  }
}

/**
 * Home-level convention roots hold installed bundles and deployed runtime copies.
 * They are overwrite-prone deployment targets, not canonical feedback sources:
 * durable feedback written there would create shadow sources and be lost on update.
 */
const HOME_RUNTIME_ROOTS = [
  path.join(os.homedir(), ".agents", "skills"),
  path.join(os.homedir(), ".omp", "agent", "skills"),
];

function isHomeRuntimeRoot(dir: string): boolean {
  const resolved = path.resolve(dir);
  return HOME_RUNTIME_ROOTS.some((root) => resolved.startsWith(root + path.sep));
}

async function resolveSkillDir(
  skillPath: string,
  cwd: string,
): Promise<string | null> {
  // 1. Try cwd-relative
  const cwdResolved = path.resolve(cwd, skillPath);
  if (await hasSkillMd(cwdResolved)) return cwdResolved;

  // 2. Try each configured/convention skill root
  for (const root of await getSkillRoots(cwd)) {
    const globalResolved = path.resolve(root, skillPath);
    if (await hasSkillMd(globalResolved)) return globalResolved;
  }

  // 3. Try as absolute or ~-expanded path
  if (skillPath.startsWith("/") || skillPath.startsWith("~")) {
    const absResolved = path.resolve(expandHome(skillPath));
    if (await hasSkillMd(absResolved)) return absResolved;
  }

  return null;
}

const factory: CustomToolFactory = (pi) => ({
  name: "append_feedback",
  label: "Append Skill Feedback",
  description:
    "Append a structured entry to a skill's FEEDBACK.md file. Use after consulting a skill " +
    "and encountering unclear instructions, missing patterns, wrong outputs, or any issue that " +
    "should be reviewed. Records timestamp, severity, issue, context, and suggested fix. " +
    "The skill_path can be relative to cwd, relative to a configured skill root, or absolute. " +
    "Writes to home runtime roots (~/.agents/skills, ~/.omp/agent/skills) " +
    "are rejected: installed bundles and deployed copies are not feedback sources — route " +
    "upstream instead (Expo defects via expo-skill-feedback).",
  parameters: pi.zod.object({
    skill_path: pi.zod
      .string()
      .describe(
        "Path to the skill directory containing SKILL.md. Can be relative to cwd " +
        "(e.g., '.omp/skills/webdev-standards' or '.agents/skills/webdev-standards'), " +
        "relative to a configured skill root, or absolute. Home runtime roots " +
        "(~/.agents/skills, ~/.omp/agent/skills) are rejected: installed " +
        "bundles and deployed copies are not feedback targets.",
      ),
    severity: pi.zod
      .enum(["critical", "high", "medium", "low", "nit"])
      .describe("How severe the issue is"),
    issue: pi.zod
      .string()
      .describe("What went wrong — be specific about the gap in the skill"),
    context: pi.zod
      .string()
      .describe("What you were doing when the issue occurred"),
    suggested_fix: pi.zod
      .string()
      .describe("What should change in the skill to fix this"),
  }),
  async execute(_toolCallId, params, _onUpdate, _ctx, _signal) {
    const resolved = await resolveSkillDir(params.skill_path, pi.cwd);

    if (!resolved) {
      return {
        content: [
          {
            type: "text",
            text:
              `Rejected: no SKILL.md found for "${params.skill_path}". ` +
              "Checked cwd-relative, configured skill roots " +
              "(cwd/.omp/config.yml, ~/.omp/agent/config.yml), " +
              "convention directories (~/.agents/skills, ~/.omp/agent/skills), " +
              "and absolute path. Is this a valid skill directory?",
          },
        ],
        details: { error: "no_skill_md", path: params.skill_path },
      };
    }

    if (isHomeRuntimeRoot(resolved)) {
      return {
        content: [
          {
            type: "text",
            text:
              `Rejected: "${params.skill_path}" resolves to ${resolved}, an installed/` +
              "runtime skill copy, which is not a canonical feedback source. Durable feedback " +
              "there would create a shadow source and be lost when the bundle is updated. " +
              "Route by provenance instead: Codarr-authored skills go to .agents/skills/<name>/, " +
              "project-local skills to their project skill root, and installed bundles upstream " +
              "to the author/marketplace — Expo defects via expo-skill-feedback " +
              "(`npx --yes submit-expo-feedback@latest`).",
          },
        ],
        details: { error: "installed_runtime_root", path: resolved },
      };
    }

    const feedbackPath = path.join(resolved, "FEEDBACK.md");
    const ts = new Date().toISOString();
    const entry = [
      `### ${ts} — ${params.severity.toUpperCase()}`,
      `- **Issue:** ${params.issue}`,
      `- **Context:** ${params.context}`,
      `- **Suggested fix:** ${params.suggested_fix}`,
      `- **Status:** open`,
      "",
    ].join("\n");

    try {
      await fs.access(feedbackPath);
    } catch {
      await fs.writeFile(
        feedbackPath,
        "# Feedback\n\nEntries from agents using this skill.\n\n",
      );
    }

    await fs.appendFile(feedbackPath, entry);
    return {
      content: [
        {
          type: "text",
          text: `Appended ${params.severity} feedback to ${feedbackPath}`,
        },
      ],
      details: { path: feedbackPath, severity: params.severity },
    };
  },
});

export default factory;

# Skills for writing sanderling specs

Agent skills for adopting sanderling: getting it running against your app, writing
property specifications, reviewing them for the failure modes that make a spec
look like it works when it does not, and reading a run honestly.

Copy the ones you want into your agent's skills directory (`.claude/skills/` for
Claude Code) or point your agent at this directory directly.

Start with `sanderling-setup`, then `sanderling-spec-authoring`. Run
`sanderling-spec-review` over anything before you trust it.

The reasoning behind the rules these encode is in
[docs/development/design-principles.md](../docs/development/design-principles.md),
section 8 in particular.

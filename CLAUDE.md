## Project Guidelines

- Do not call the task done until it is fully complete and tested.
- Always write tests for new features and bug fixes.
- Do not dismiss bug as a pre-existing" issue even if it was present before your change. It does not matter, it's still your responsibility to fix it. When you see a bug, fix it. Don't ignore it.
- Always build a feature in a new branch. Do not push directly to the main branch. Always check current branch before pushing.

## Coding Guidelines

  - Keep code simple and easy to read.
  - Avoid excessive comments. Only comment when absolutely necessary.
  - Create WIP pull requests when you start working on a feature, and update the PR as you make progress

## Git Branch Rules

  - No slashes in branch names (e.g., use `fix-something` not `fix/something`).

## PR Rules

  - Simple PR title, few-line description. Never write a wall of text. Nobody reads it.
  - Everything lowercase in PR titles, descriptions, and comments.

### PR Description Rules

  - Plain text only. No markdown, no headings, no bullets, no bold, no code blocks, no emoji.
  - A few lines, that's it. Don't write an essay.
  - Super casual, like you're telling a teammate over chat. Lowercase is fine.
  - Don't polish it. A few typos and loose grammar are fine and preferred over something that reads like a template.
  - Keep the facts right even though the tone is casual. Casual is about the voice, not about being vague or wrong.

## Git Commit Rules

  - Commit after every small, atomic change. Each commit should touch 1-3 files max.
  - Use conventional commit format: `feat|fix|refactor|docs|test|chore|ci(scope): message`
  - Never use `git add .` or `git add -A`. Always stage specific files by name.
  - Keep commits small: aim for under 20 lines changed per commit.
  - Don't batch multiple unrelated changes into one commit.
  - Commit early and often. A working 5-line change is better than a pending 200-line change.

## Project Guidelines

- Do not call the task done until it is fully complete and tested.
- Always write tests for new features and bug fixes.
- Do not dismiss bug as a pre-existing" issue even if it was present before your change. It does not matter, it's still your responsibility to fix it. When you see a bug, fix it. Don't ignore it.
- Always build a feature in a new branch. Do not push directly to the main branch. Always check current branch before pushing.
- Delete every temporary file, scratch script, probe, backup copy and build artifact you created
  before committing. Check `git status` for strays that are yours.

## Coding Guidelines

  - Make the smallest change that achieves the goal. Do not refactor, rename, or restructure
    code you were not asked to touch, and do not add abstraction for a second caller that does
    not exist yet.
  - Keep code simple and easy to read. Code should read without commentary: name things so the
    intent is obvious rather than explaining an unclear name in a comment.
  - Write zero comments first. Add one only where the code genuinely cannot carry the intent,
    and then say WHY, never WHAT. A comment restating the line below it is noise.
  - Create WIP pull requests when you start working on a feature, and update the PR as you make progress

## Testing Guidelines

  - Tests are first-class code and are held to the same standard as the code they cover, or
    higher. A weak test is worse than no test: it reports safety that does not exist.
  - A regression test must fail against the bug it covers. Confirm it fails before the fix,
    and say so. If a test cannot produce a red, say that plainly instead of implying it did.
  - Assert the real outcome, not a proxy for it. Prefer asserting what reached the driver, the
    file, or the wire over asserting an intermediate struct.
  - Never weaken an assertion to make a test pass, and never change what a test is testing in
    order to accommodate a signature change. Its original subject must survive.
  - Carry intent through test names and assertions rather than through prose comments.

## Verification Targets

  - All three targets run on this machine when it is macOS: the Android emulator, the iOS
    simulator, and Chrome. A change to a driver, the runner, the verifier or the spec surface
    is verified on every target it can affect, not on whichever one is already booted.
  - If a simulator is not running, start it. "Nothing was booted" is not a reason to skip a
    target, and neither is a missing tool on PATH: fix discovery or the install flow rather
    than prefixing the run with environment variables.

## Git Branch Rules

  - No slashes in branch names (e.g., use `fix-something` not `fix/something`).

## PR Rules

  - Never merge a pull request. I merge, nobody else. That covers `gh pr merge`, enabling
    auto-merge, squash, rebase, fast-forward, and merging the branch into anything locally.
    Push the branch, say it is ready, and stop there.
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

## Delegation

  - Do the work in subagents, not in the main context. Installs, builds, test runs,
    file-by-file writing, greps across the tree and any multi-step verification go to an
    Agent that reports back a short result.
  - The main context is for deciding what to do, reviewing what comes back, and talking to
    the user. Keep it clean. Never paste build output, test output or file listings into it.
  - One specific task per subagent, with the context it needs. Launch independent tasks in
    parallel in a single message.
  - Give each agent its own scratchpad subdirectory, named for its task, and tell it to
    delete nothing it did not create. Agents run concurrently and a shared scratch directory
    means one deletes another's work mid-run.

## Keeping the record

  - Finishing a task includes updating the files that describe its subject. Status lines,
    schedules, gates and readiness notes go stale the moment work lands, and a plan that
    says "not started" about something that ran is worse than no plan.
  - Write down what was found, not just what was changed. A measurement, a number, a thing
    that turned out not to work: it goes in the file where someone would look for it, with
    the path, commit or number behind it.
  - Correct old assumptions explicitly. When something turns out to be wrong, fix the
    sentence that said it rather than adding a newer sentence beside it. Say what it used
    to claim if the change matters.
  - Verify against the repository rather than recalling. A file that says it was checked
    against HEAD and was not is the failure this project exists to catch.

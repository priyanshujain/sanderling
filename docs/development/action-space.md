# Action Space

Inventory of user interaction primitives Sanderling currently supports, compared against the full set available through the Maestro driver layer.

## Current Actions

| Action | Parameters | Notes |
|--------|-----------|-------|
| `Tap` | `on` (selector or element) | Single tap on element or coordinates |
| `DoubleTap` | `on` (selector or element) | Two taps within a sub-100 ms window |
| `LongPress` | `on` (selector or element) | Press and hold; resolves to coordinates |
| `InputText` | `into`, `text` | Type into focused field |
| `Swipe` | `from`, `to`, `durationMillis?` | Point-to-point or element-to-element |
| `Scroll` | `direction`, `in?`, `durationMillis?` | Scroll a container up, down, left, or right |
| `PressKey` | `key` | back, home, enter, tab, up, down, left, right |
| `Wait` | `durationMillis` | Sleep for N ms |

## Gaps vs Maestro Driver

The Maestro driver layer (both `AndroidDriver` and `IOSDriver`) already implements these actions. They are not yet surfaced in Sanderling's action model.

### User Interaction Gaps

| Action | Priority | Notes |
|--------|----------|-------|
| `EraseText` | Medium | Clear N characters from focused field |
| `HideKeyboard` | Medium | Dismiss software keyboard after text input |
| `PasteText` | Low | Paste clipboard content into focused field |

### Device/Environment Gaps

These are less about user gesture modeling and more about test environment setup. Separate decision on whether they belong in the action model.

| Action | Notes |
|--------|-------|
| `SetLocation` | GPS coordinate mocking |
| `SetOrientation` | Portrait, landscape-left, landscape-right |
| `SetAirplaneMode` | Network condition simulation |
| `OpenLink` | Deep links and URLs |
| `AddMedia` | Inject photos or videos into device gallery |

### Out of Scope

These Maestro commands don't fit Sanderling's interaction model:

- `LaunchApp`, `StopApp`, `KillApp`, `ClearState` - app lifecycle, handled outside the action stream
- `AssertVisible`, `AssertWithAI` - Sanderling has its own property and assertion model
- `RunScript`, `EvalScript`, `RunFlow`, `Repeat` - Sanderling has its own flow and generator model
- `InputRandomText/Number/Email/...` - Sanderling's `from()` and `weighted()` generators cover this

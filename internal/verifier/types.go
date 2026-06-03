package verifier

// ActionKind identifies the category of a generated UI action.
type ActionKind string

const (
	ActionKindTap       ActionKind = "Tap"
	ActionKindDoubleTap ActionKind = "DoubleTap"
	ActionKindInputText ActionKind = "InputText"
	ActionKindSwipe     ActionKind = "Swipe"
	ActionKindPressKey  ActionKind = "PressKey"
	ActionKindWait      ActionKind = "Wait"
	ActionKindLongPress ActionKind = "LongPress"
	ActionKindScroll    ActionKind = "Scroll"
)

// Action is a single UI interaction produced by the spec's action generator.
type Action struct {
	Kind ActionKind
	On   string
	Text string
	// X, Y hold the element center when the spec passed an ax element to
	// Tap/InputText. Zero means the runner must resolve On against the
	// current hierarchy.
	X, Y int
	// Swipe coordinates (raw px). Used only for ActionKindSwipe.
	FromX, FromY int
	ToX, ToY     int
	// DurationMillis is the Swipe gesture duration or the Wait duration.
	DurationMillis int
	// Key is the logical key name for ActionKindPressKey.
	Key string
	// Direction is the scroll direction for ActionKindScroll: one of "up",
	// "down", "left", "right". Empty for every other kind.
	Direction string
}

// LogEntry mirrors a logcat line captured between steps.
type LogEntry struct {
	UnixMillis int64
	Level      string
	Tag        string
	Message    string
}

// Exception mirrors an SDK-captured uncaught throwable.
type Exception struct {
	Class      string
	Message    string
	StackTrace string
	UnixMillis int64
}

// ExtractorChange records a single extractor's value transition across one
// step. Used to surface "what changed at this step" breadcrumbs at violation
// markers in the replay UI.
type ExtractorChange struct {
	Prev []byte
	Curr []byte
}

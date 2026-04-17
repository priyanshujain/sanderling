package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/priyanshujain/uatu/internal/agent"
	"github.com/priyanshujain/uatu/internal/driver"
	"github.com/priyanshujain/uatu/internal/ltl"
	"github.com/priyanshujain/uatu/internal/trace"
	"github.com/priyanshujain/uatu/internal/verifier"
)

type Options struct {
	BundleID        string
	ClearState      bool
	Duration        time.Duration
	SnapshotTimeout time.Duration
	IdleTimeout     time.Duration

	Connection  *agent.Conn
	Driver      driver.Driver
	Verifier    *verifier.Verifier
	TraceWriter *trace.Writer
}

type Summary struct {
	StartTime  time.Time
	EndTime    time.Time
	Steps      int
	Violations []ViolationRecord
}

type ViolationRecord struct {
	StepIndex  int
	Properties []string
}

func Run(ctx context.Context, options Options) (Summary, error) {
	if err := validate(options); err != nil {
		return Summary{}, err
	}

	summary := Summary{StartTime: time.Now()}

	if err := options.Driver.Launch(ctx, options.BundleID, options.ClearState); err != nil {
		return summary, fmt.Errorf("launch: %w", err)
	}
	defer func() {
		_ = options.Driver.Terminate(context.Background())
	}()

	deadline := summary.StartTime.Add(options.Duration)
	stepIndex := 0
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			break
		}
		stepIndex++
		stepStart := time.Now()

		snapshot, err := snapshotStep(ctx, options)
		if err != nil {
			return summary, fmt.Errorf("step %d snapshot: %w", stepIndex, err)
		}

		if err := options.Verifier.PushSnapshot(verifier.Snapshots(snapshot.Snapshots)); err != nil {
			return summary, fmt.Errorf("step %d push: %w", stepIndex, err)
		}
		verdicts := options.Verifier.EvaluateProperties()
		violations := violationNames(verdicts)

		nextAction, nextErr := options.Verifier.NextAction()
		var traceAction *trace.Action
		if nextErr == nil {
			traceAction = traceActionFor(nextAction)
		} else if !errors.Is(nextErr, verifier.ErrNoAction) {
			return summary, fmt.Errorf("step %d next action: %w", stepIndex, nextErr)
		}

		step := trace.Step{
			Index:      stepIndex,
			Timestamp:  stepStart,
			Screen:     screenFromSnapshot(snapshot.Snapshots),
			Snapshots:  snapshot.Snapshots,
			Action:     traceAction,
			Violations: violations,
		}
		if err := options.TraceWriter.WriteStep(step); err != nil {
			return summary, fmt.Errorf("step %d trace: %w", stepIndex, err)
		}
		summary.Steps = stepIndex
		if len(violations) > 0 {
			summary.Violations = append(summary.Violations, ViolationRecord{
				StepIndex:  stepIndex,
				Properties: violations,
			})
		}

		if err := options.Connection.Release(ctx); err != nil {
			return summary, fmt.Errorf("step %d release: %w", stepIndex, err)
		}

		if nextErr == nil {
			if err := applyAction(ctx, options.Driver, nextAction); err != nil {
				return summary, fmt.Errorf("step %d apply: %w", stepIndex, err)
			}
		}

		idleCtx, idleCancel := context.WithTimeout(ctx, options.IdleTimeout)
		_ = options.Driver.WaitForIdle(idleCtx, options.IdleTimeout)
		idleCancel()
	}

	summary.EndTime = time.Now()
	return summary, nil
}

func validate(options Options) error {
	if options.Connection == nil {
		return errors.New("runner: Connection is required")
	}
	if options.Driver == nil {
		return errors.New("runner: Driver is required")
	}
	if options.Verifier == nil {
		return errors.New("runner: Verifier is required")
	}
	if options.TraceWriter == nil {
		return errors.New("runner: TraceWriter is required")
	}
	if options.Duration <= 0 {
		return errors.New("runner: Duration must be positive")
	}
	if options.SnapshotTimeout <= 0 {
		options.SnapshotTimeout = 5 * time.Second
	}
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = 2 * time.Second
	}
	return nil
}

func snapshotStep(ctx context.Context, options Options) (agent.Message, error) {
	snapshotTimeout := options.SnapshotTimeout
	if snapshotTimeout <= 0 {
		snapshotTimeout = 5 * time.Second
	}
	snapshotCtx, snapshotCancel := context.WithTimeout(ctx, snapshotTimeout)
	defer snapshotCancel()
	return options.Connection.Snapshot(snapshotCtx)
}

func violationNames(verdicts map[string]ltl.Verdict) []string {
	var names []string
	for name, verdict := range verdicts {
		if verdict == ltl.VerdictViolated {
			names = append(names, name)
		}
	}
	return names
}

func screenFromSnapshot(snapshots map[string]json.RawMessage) string {
	raw, ok := snapshots["screen"]
	if !ok {
		return ""
	}
	var screen string
	_ = json.Unmarshal(raw, &screen)
	return screen
}

func applyAction(ctx context.Context, drv driver.Driver, action verifier.Action) error {
	switch action.Kind {
	case verifier.ActionKindTap:
		if action.On == "" {
			return nil
		}
		return drv.TapSelector(ctx, action.On)
	case verifier.ActionKindInputText:
		return drv.InputText(ctx, action.Text)
	default:
		return fmt.Errorf("unknown action kind %q", action.Kind)
	}
}

func traceActionFor(action verifier.Action) *trace.Action {
	traceAction := &trace.Action{Kind: string(action.Kind)}
	switch action.Kind {
	case verifier.ActionKindTap:
		// Selector lives in the trace step's action.text field for now —
		// trace.Action only has X/Y/Text and we don't resolve coordinates
		// at the runner layer.
		traceAction.Text = action.On
	case verifier.ActionKindInputText:
		traceAction.Text = action.Text
	}
	return traceAction
}

import XCTest
import CoreGraphics

// Synthesizes one timestamped event record from a list of primitive events.
//
// The synthesizer constrains how precise timing can be expressed:
// - Within one pointer path, event offsets are honored to the millisecond,
//   but nothing after the path's first lift is delivered: a second press is
//   silently dropped, whether added through the press helper or as a raw
//   pointer event.
// - Across paths, every event is delivered, but each path's timeline is
//   normalized to its own first event, so a later start offset collapses.
//
// Sequential taps therefore become one path per tap, each anchored by a raw
// zero-offset move event at the tap point: the anchor pins the path's
// timeline origin to the record's, turning the tap's absolute press offset
// into an in-path delta, which is honored. This preserves the inter-tap gap
// to the millisecond.
enum Gesture {
    enum GestureError: Error {
        case missingField(String)
        case unknownKind(String)
        case touchUpWithoutTouchDown
        case touchDownWhileTouchActive
        case synthesisFailed(String)
    }

    // minimumHoldSeconds keeps a tap's press observable when its down and up
    // arrive at the same offset; far below any long-press threshold.
    private static let minimumHoldSeconds = 0.03

    // pathGapSeconds orders a press strictly after the previous lift when a
    // requested gap is shorter than the minimum hold.
    private static let pathGapSeconds = 0.005

    // The raw shape of a touch move, read once from a probe path built with
    // the path helpers so no private enum values are hardcoded.
    private struct MoveEventShape {
        let type: Int
        let button: Int
        let clicks: UInt
    }

    private static let moveShape: MoveEventShape = {
        let probe = XCPointerEventPath(forTouchAt: CGPoint(x: 1, y: 1), offset: 0)
        probe.move(to: CGPoint(x: 2, y: 2), atOffset: minimumHoldSeconds)
        probe.liftUp(atOffset: minimumHoldSeconds * 2)
        let events = probe.pointerEvents
        let move = events.count > 1 ? events[1] : nil
        return MoveEventShape(
            type: move?.eventType ?? 2,
            button: move?.buttonType ?? 0,
            clicks: move.map { UInt($0.clickCount) } ?? 0
        )
    }()

    // probe reports the raw events the path helpers produce, for diagnostics.
    static func probe() -> [String: Any] {
        let path = XCPointerEventPath(forTouchAt: CGPoint(x: 1, y: 2), offset: 0)
        path.move(to: CGPoint(x: 3, y: 4), atOffset: 0.05)
        path.liftUp(atOffset: 0.1)
        return [
            "descriptions": path.pointerEvents.map { $0.description },
            "types": path.pointerEvents.map { $0.eventType },
            "buttons": path.pointerEvents.map { $0.buttonType },
            "offsets": path.pointerEvents.map { $0.offset },
        ]
    }

    // touchSegment is one press-to-lift span at absolute record offsets.
    private struct touchSegment {
        var downPoint: CGPoint
        var downOffset: Double
        var upPoint: CGPoint
        var upOffset: Double
        var movePoint: CGPoint?
        var moveOffset: Double = 0
    }

    static func perform(events: [[String: Any]]) throws {
        var offset = 0.0
        var lastLiftOffset = -1.0
        var segments: [touchSegment] = []
        var active: touchSegment?

        func point(_ event: [String: Any], _ xKey: String, _ yKey: String) throws -> CGPoint {
            guard let x = event[xKey] as? Double, let y = event[yKey] as? Double else {
                throw GestureError.missingField("\(xKey)/\(yKey)")
            }
            return CGPoint(x: x, y: y)
        }

        for event in events {
            guard let kind = event["kind"] as? String else {
                throw GestureError.missingField("kind")
            }
            switch kind {
            case "touchDown":
                guard active == nil else {
                    throw GestureError.touchDownWhileTouchActive
                }
                let location = try point(event, "x", "y")
                let downOffset = max(offset, lastLiftOffset + Gesture.pathGapSeconds)
                active = touchSegment(
                    downPoint: location, downOffset: downOffset,
                    upPoint: location, upOffset: downOffset + Gesture.minimumHoldSeconds)
            case "touchUp":
                guard var segment = active else {
                    throw GestureError.touchUpWithoutTouchDown
                }
                segment.upPoint = try point(event, "x", "y")
                segment.upOffset = max(offset, segment.downOffset + Gesture.minimumHoldSeconds)
                lastLiftOffset = segment.upOffset
                segments.append(segment)
                active = nil
            case "delay":
                guard let milliseconds = event["milliseconds"] as? Double else {
                    throw GestureError.missingField("milliseconds")
                }
                offset += milliseconds / 1000.0
            case "swipe":
                guard active == nil else {
                    throw GestureError.touchDownWhileTouchActive
                }
                let from = try point(event, "fromX", "fromY")
                let to = try point(event, "toX", "toY")
                guard let seconds = event["seconds"] as? Double else {
                    throw GestureError.missingField("seconds")
                }
                let downOffset = max(offset, lastLiftOffset + Gesture.pathGapSeconds)
                segments.append(touchSegment(
                    downPoint: from, downOffset: downOffset,
                    upPoint: to, upOffset: downOffset + seconds,
                    movePoint: to, moveOffset: downOffset + seconds))
                lastLiftOffset = downOffset + seconds
                offset = downOffset + seconds
            default:
                throw GestureError.unknownKind(kind)
            }
        }
        if let segment = active {
            // A down without an up would leave a stuck touch on screen.
            segments.append(segment)
        }
        guard !segments.isEmpty else {
            throw GestureError.missingField("events")
        }

        // The synthesis stack raises ObjC exceptions for invalid paths; bridge
        // them into a thrown error so the server survives a bad record.
        var caughtException: NSError?
        var synthesisError: Error?
        let completed = CompanionRunCatching({
            do {
                try Gesture.synthesize(segments: segments)
            } catch {
                synthesisError = error
            }
        }, &caughtException)
        if !completed {
            throw GestureError.synthesisFailed(caughtException?.localizedDescription ?? "unknown exception")
        }
        if let synthesisError = synthesisError {
            throw synthesisError
        }
    }

    private static func synthesize(segments: [touchSegment]) throws {
        let record = XCSynthesizedEventRecord(name: "companion", interfaceOrientation: 0)
        for (index, segment) in segments.enumerated() {
            // Paths play back one after another with their start offsets
            // normalized away, so each path carries its events relative to its
            // own press. A trailing hover move then stretches the path out to
            // the next press's absolute offset, which preserves the requested
            // inter-tap gap under sequential playback.
            let base = segment.downOffset
            let path = XCPointerEventPath(forTouchAt: segment.downPoint, offset: 0)
            // Distinct pointer identities: without this, sequential taps at
            // the same point merge into one multi-tap gesture and the app
            // sees a single click no matter the gap.
            path.index = UInt64(index)
            if let movePoint = segment.movePoint {
                path.move(to: movePoint, atOffset: segment.moveOffset - base)
            }
            let upDelta = segment.upOffset - base
            path.liftUp(atOffset: upDelta)
            if index + 1 < segments.count {
                let tailDelta = segments[index + 1].downOffset - base
                if tailDelta > upDelta {
                    path._addPointerEvent(XCPointerEvent(
                        type: Gesture.moveShape.type,
                        buttonType: Gesture.moveShape.button,
                        coordinate: segment.upPoint,
                        offset: tailDelta,
                        clickCount: Gesture.moveShape.clicks))
                }
            }
            record.add(path)
        }

        // Synchronous delivery: returns whether the event was synthesized and
        // populates the error out-pointer on failure. This avoids the async
        // completion-block path.
        try record.synthesize()
    }
}

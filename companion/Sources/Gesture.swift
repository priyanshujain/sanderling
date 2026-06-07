import XCTest
import CoreGraphics

// Synthesizes a single timestamped touch event from a list of primitive events.
// A running offset (seconds) is advanced as events are consumed so sequential
// taps collapse into one pointer path with press/lift/press/lift at exact
// offsets, honoring the inter-tap gap to the millisecond.
enum Gesture {
    enum GestureError: Error {
        case missingField(String)
        case unknownKind(String)
        case synthesisTimeout
        case synthesisFailed(String)
    }

    static func perform(events: [[String: Any]]) throws {
        var offset = 0.0
        var path: XCPointerEventPath?

        func point(_ event: [String: Any], _ xKey: String, _ yKey: String) throws -> CGPoint {
            guard let x = event[xKey] as? Double, let y = event[yKey] as? Double else {
                throw GestureError.missingField("\(xKey)/\(yKey)")
            }
            return CGPoint(x: x, y: y)
        }

        func ensurePath(at location: CGPoint) -> XCPointerEventPath {
            if let existing = path {
                existing.move(to: location, atOffset: offset)
                return existing
            }
            let created = XCPointerEventPath(forTouchAt: location, offset: offset)
            path = created
            return created
        }

        for event in events {
            guard let kind = event["kind"] as? String else {
                throw GestureError.missingField("kind")
            }
            switch kind {
            case "touchDown":
                let location = try point(event, "x", "y")
                ensurePath(at: location).pressDown(atOffset: offset)
            case "touchUp":
                let location = try point(event, "x", "y")
                ensurePath(at: location).liftUp(atOffset: offset)
            case "delay":
                guard let milliseconds = event["milliseconds"] as? Double else {
                    throw GestureError.missingField("milliseconds")
                }
                offset += milliseconds / 1000.0
            case "swipe":
                let from = try point(event, "fromX", "fromY")
                let to = try point(event, "toX", "toY")
                guard let seconds = event["seconds"] as? Double else {
                    throw GestureError.missingField("seconds")
                }
                let activePath = ensurePath(at: from)
                activePath.pressDown(atOffset: offset)
                activePath.move(to: to, atOffset: offset + seconds)
                activePath.liftUp(atOffset: offset + seconds)
                offset += seconds
            default:
                throw GestureError.unknownKind(kind)
            }
        }

        guard let path = path else {
            throw GestureError.missingField("events")
        }

        let record = XCSynthesizedEventRecord(name: "companion", interfaceOrientation: 0)
        record.add(path)

        let semaphore = DispatchSemaphore(value: 0)
        var synthesisError: Error?
        XCTRunnerDaemonSession.shared().synthesize(event: record) { error in
            synthesisError = error
            semaphore.signal()
        }
        if semaphore.wait(timeout: .now() + 30) == .timedOut {
            throw GestureError.synthesisTimeout
        }
        if let synthesisError = synthesisError {
            throw GestureError.synthesisFailed("\(synthesisError)")
        }
    }
}

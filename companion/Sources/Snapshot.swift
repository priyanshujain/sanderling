import XCTest

// Flattens an accessibility snapshot tree into the legacy flat element array.
enum Snapshot {
    // Produces the flat element array for the given bundle identifier. Falls back
    // to springboard if the requested app is not running or its snapshot throws.
    // Snapshot resolution can demand the test's execution context, which lives
    // on the main thread, and a framework assertion raises an exception that
    // would kill the server; so the capture runs on main inside the catch
    // bridge, and an assertion yields an empty dump, which upstream treats as
    // collapsed and retries.
    static func elements(bundleIdentifier: String) -> [[String: Any]] {
        var result: [[String: Any]] = []
        var caughtError: NSError?
        let work = {
            _ = CompanionRunCatching({
                if let captured = try? capture(bundleIdentifier: bundleIdentifier) {
                    result = captured
                } else if let fallback = try? capture(bundleIdentifier: "com.apple.springboard") {
                    result = fallback
                }
            }, &caughtError)
        }
        if Thread.isMainThread {
            work()
        } else {
            DispatchQueue.main.sync(execute: work)
        }
        return result
    }

    private static func capture(bundleIdentifier: String) throws -> [[String: Any]] {
        let application = XCUIApplication(bundleIdentifier: bundleIdentifier)
        let root = try application.snapshot()
        var result: [[String: Any]] = []
        walk(root, into: &result)
        return result
    }

    private static func walk(_ node: XCUIElementSnapshot, into result: inout [[String: Any]]) {
        result.append(serialize(node))
        for child in node.children {
            walk(child, into: &result)
        }
    }

    private static func serialize(_ node: XCUIElementSnapshot) -> [String: Any] {
        let frame = node.frame
        return [
            "type": elementTypeName(node.elementType),
            "frame": [
                "x": Double(frame.origin.x),
                "y": Double(frame.origin.y),
                "width": Double(frame.size.width),
                "height": Double(frame.size.height),
            ],
            "enabled": node.isEnabled,
            "AXLabel": nullableString(node.label),
            "AXValue": nullableString(stringifyValue(node.value)),
            "AXUniqueId": nullableString(node.identifier),
        ]
    }

    private static func nullableString(_ value: String?) -> Any {
        guard let value = value, !value.isEmpty else { return NSNull() }
        return value
    }

    private static func stringifyValue(_ value: Any?) -> String? {
        guard let value = value else { return nil }
        if let text = value as? String { return text }
        return String(describing: value)
    }
}

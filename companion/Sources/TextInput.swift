import XCTest

// Types unicode text into the focused element through the public typeText API,
// avoiding the pasteboard entirely. With replace enabled, the current value
// length is consumed first by prepending that many delete keys so the whole
// replacement happens in one typeText call.
enum TextInput {
    enum TextInputError: Error {
        case typingFailed(String)
    }

    static func type(text: String, replace: Bool, bundleIdentifier: String) throws {
        let application = XCUIApplication(bundleIdentifier: bundleIdentifier)

        var payload = text
        if replace {
            let currentLength = focusedValueLength(bundleIdentifier: bundleIdentifier)
            if currentLength > 0 {
                let deletes = String(repeating: XCUIKeyboardKey.delete.rawValue, count: currentLength)
                payload = deletes + text
            }
        }

        // Type at the application level so it lands in whatever holds keyboard
        // focus, without resolving a specific element. Compose Multiplatform
        // text fields do not reliably expose hasKeyboardFocus to the query layer.
        // typeText must run on the main thread.
        var caughtError: NSError?
        var completed = false
        runOnMain {
            completed = CompanionRunCatching({
                application.typeText(payload)
            }, &caughtError)
        }
        if !completed {
            throw TextInputError.typingFailed(caughtError?.localizedDescription ?? "unknown")
        }
    }

    private static func runOnMain(_ work: () -> Void) {
        if Thread.isMainThread {
            work()
        } else {
            DispatchQueue.main.sync(execute: work)
        }
    }

    // Reads the current value length of the focused editable field from a flat
    // snapshot. Used to size the delete prefix for replace.
    private static func focusedValueLength(bundleIdentifier: String) -> Int {
        let elements = Snapshot.elements(bundleIdentifier: bundleIdentifier)
        for element in elements {
            guard let type = element["type"] as? String,
                  type == "TextArea" || type == "TextField",
                  let value = element["AXValue"] as? String else {
                continue
            }
            if !value.isEmpty {
                return value.count
            }
        }
        return 0
    }
}

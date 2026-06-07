import XCTest

// Types unicode text into the focused element through the public typeText API,
// avoiding the pasteboard entirely. With replace enabled, the current value
// length is consumed first by prepending that many delete keys so the whole
// replacement happens in one typeText call.
enum TextInput {
    static func type(text: String, replace: Bool, bundleIdentifier: String) throws {
        let application = XCUIApplication(bundleIdentifier: bundleIdentifier)
        let focused = focusedElement(in: application)

        var payload = text
        if replace {
            let currentLength = currentValueLength(of: focused)
            if currentLength > 0 {
                let deletes = String(repeating: XCUIKeyboardKey.delete.rawValue, count: currentLength)
                payload = deletes + text
            }
        }

        if let focused = focused {
            focused.typeText(payload)
        } else {
            application.typeText(payload)
        }
    }

    private static func focusedElement(in application: XCUIApplication) -> XCUIElement? {
        let predicate = NSPredicate(format: "hasKeyboardFocus == true")
        let matches = application.descendants(matching: .any).matching(predicate)
        if matches.count > 0 {
            return matches.element(boundBy: 0)
        }
        return nil
    }

    private static func currentValueLength(of element: XCUIElement?) -> Int {
        guard let element = element, let value = element.value as? String else {
            return 0
        }
        return value.count
    }
}

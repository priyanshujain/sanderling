import XCTest

// Launches and terminates apps through the automation session. Lifecycle
// performed outside the session leaves its cached app proxies bound to dead
// processes, after which snapshots hang and typing asserts.
enum AppLifecycle {
    enum LifecycleError: Error {
        case failed(String)
    }

    static func launch(bundleIdentifier: String, foregroundIfRunning: Bool) throws {
        try onMainCatching {
            let application = XCUIApplication(bundleIdentifier: bundleIdentifier)
            if foregroundIfRunning {
                // activate starts the app when needed and foregrounds it when
                // already running, without a forced relaunch.
                application.activate()
            } else {
                application.launch()
            }
        }
    }

    static func terminate(bundleIdentifier: String) throws {
        try onMainCatching {
            let application = XCUIApplication(bundleIdentifier: bundleIdentifier)
            if application.state == .notRunning {
                return
            }
            application.terminate()
        }
    }

    // onMainCatching runs automation work on the main thread and converts a
    // framework assertion into a thrown error so the server survives it.
    private static func onMainCatching(_ work: @escaping () -> Void) throws {
        var caughtError: NSError?
        var completed = false
        let run = {
            completed = CompanionRunCatching(work, &caughtError)
        }
        if Thread.isMainThread {
            run()
        } else {
            DispatchQueue.main.sync(execute: run)
        }
        if !completed {
            throw LifecycleError.failed(caughtError?.localizedDescription ?? "unknown")
        }
    }
}

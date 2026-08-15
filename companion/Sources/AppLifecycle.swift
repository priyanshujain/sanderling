import XCTest

// Launches and terminates apps through the automation session. Lifecycle
// performed outside the session leaves its cached app proxies bound to dead
// processes, after which snapshots hang and typing asserts.
enum AppLifecycle {
    enum LifecycleError: Error {
        case failed(String)
    }

    static func launch(bundleIdentifier: String, foregroundIfRunning: Bool) throws {
        var reached = XCUIApplication.State.unknown
        try onMainCatching {
            let application = XCUIApplication(bundleIdentifier: bundleIdentifier)
            if foregroundIfRunning {
                // activate starts the app when needed and foregrounds it when
                // already running, without a forced relaunch.
                application.activate()
            } else {
                application.launch()
            }
            reached = application.state
        }
        // A refused launch is recorded as a test issue that never throws, so
        // without this the runner answers ok for an app that is not running
        // and the host learns nothing until its own bound expires.
        switch reached {
        case .runningForeground, .runningBackground, .runningBackgroundSuspended:
            return
        default:
            throw LifecycleError.failed(
                "\(bundleIdentifier) is \(name(of: reached)) after launch")
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

    // state reports the app's run state in the vocabulary the Go transport maps
    // to a process state: foreground, background, or notRunning. On the
    // simulator the hybrid never calls it; on device it backs ForegroundApp.
    static func state(bundleIdentifier: String) -> String {
        var result = "notRunning"
        let collect = {
            switch XCUIApplication(bundleIdentifier: bundleIdentifier).state {
            case .runningForeground:
                result = "foreground"
            case .runningBackground, .runningBackgroundSuspended:
                result = "background"
            default:
                result = "notRunning"
            }
        }
        if Thread.isMainThread {
            collect()
        } else {
            DispatchQueue.main.sync(execute: collect)
        }
        return result
    }

    private static func name(of state: XCUIApplication.State) -> String {
        switch state {
        case .runningForeground:
            return "foreground"
        case .runningBackground:
            return "background"
        case .runningBackgroundSuspended:
            return "suspended"
        case .notRunning:
            return "not running"
        default:
            return "unknown"
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

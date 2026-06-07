import XCTest

// The single long-lived test method: it starts the server and parks the runner
// in a run loop so the simulator process stays alive serving the protocol.
final class RunnerTestCase: XCTestCase {
    func testServeForever() throws {
        // A failed automation call must never end the test: the server has to
        // keep serving so the host can recover and retry.
        continueAfterFailure = true
        let server = try Server()
        server.start()
        RunLoop.current.run()
    }
}

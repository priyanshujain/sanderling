import Foundation
import Network
import UIKit
import XCTest

// Newline-delimited JSON-over-TCP server. Listens on 127.0.0.1 on the port from
// the COMPANION_PORT environment variable (default 27753). Each line is one
// request; each reply is one line.
final class Server {
    private let port: NWEndpoint.Port
    private let listener: NWListener
    private let queue = DispatchQueue(label: "dev.sanderling.companion.server")
    // Requests are handled off the network queue so blocking automation work
    // (snapshot, gesture synthesis) never stalls accept and receive.
    private let work = DispatchQueue(label: "dev.sanderling.companion.work")

    // The bundle identifier of the most recently snapshotted app, used as the
    // default target for typeText when no explicit bundleId is supplied.
    private var currentBundleIdentifier = "com.apple.springboard"

    init() throws {
        let resolvedPort = ProcessInfo.processInfo.environment["COMPANION_PORT"]
            .flatMap { UInt16($0) } ?? 27753
        self.port = NWEndpoint.Port(rawValue: resolvedPort)!
        let parameters = NWParameters.tcp
        parameters.allowLocalEndpointReuse = true
        self.listener = try NWListener(using: parameters, on: self.port)
    }

    func start() {
        listener.newConnectionHandler = { [weak self] connection in
            self?.accept(connection)
        }
        listener.start(queue: queue)
    }

    private func accept(_ connection: NWConnection) {
        connection.start(queue: queue)
        receive(on: connection, buffer: Data())
    }

    private func receive(on connection: NWConnection, buffer: Data) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 65536) { [weak self] data, _, isComplete, error in
            guard let self = self else { return }
            var working = buffer
            if let data = data {
                working.append(data)
            }
            while let newlineIndex = working.firstIndex(of: 0x0A) {
                let lineData = working.subdata(in: working.startIndex..<newlineIndex)
                working.removeSubrange(working.startIndex...newlineIndex)
                self.handleLine(lineData, on: connection)
            }
            if isComplete || error != nil {
                connection.cancel()
                return
            }
            self.receive(on: connection, buffer: working)
        }
    }

    private func handleLine(_ lineData: Data, on connection: NWConnection) {
        guard !lineData.isEmpty else { return }
        work.async { [weak self] in
            guard let self = self else { return }
            let requestId = (try? JSONSerialization.jsonObject(with: lineData))
                .flatMap { ($0 as? [String: Any])?["id"] as? Int } ?? 0
            do {
                let response = try self.dispatch(lineData)
                self.send(response, on: connection)
            } catch {
                self.send(["id": requestId, "error": "\(error)"], on: connection)
            }
        }
    }

    private func dispatch(_ lineData: Data) throws -> [String: Any] {
        guard let request = try JSONSerialization.jsonObject(with: lineData) as? [String: Any] else {
            throw ServerError.malformedRequest
        }
        let requestId = request["id"] as? Int ?? 0
        guard let method = request["method"] as? String else {
            throw ServerError.malformedRequest
        }
        let params = request["params"] as? [String: Any] ?? [:]
        let result = try handle(method: method, params: params)
        return ["id": requestId, "result": result]
    }

    private func handle(method: String, params: [String: Any]) throws -> [String: Any] {
        switch method {
        case "health":
            return ["ok": true]
        case "describe":
            return describeScreen()
        case "snapshot":
            let bundleIdentifier = params["bundleId"] as? String ?? currentBundleIdentifier
            currentBundleIdentifier = bundleIdentifier
            return ["elements": Snapshot.elements(bundleIdentifier: bundleIdentifier)]
        case "gesture":
            let events = params["events"] as? [[String: Any]] ?? []
            try Gesture.perform(events: events)
            return ["ok": true]
        case "typeText":
            let text = params["text"] as? String ?? ""
            let replace = params["replace"] as? Bool ?? false
            let bundleIdentifier = params["bundleId"] as? String ?? currentBundleIdentifier
            try TextInput.type(text: text, replace: replace, bundleIdentifier: bundleIdentifier)
            return ["ok": true]
        case "screenshot":
            return try screenshot()
        default:
            throw ServerError.unknownMethod(method)
        }
    }

    private func describeScreen() -> [String: Any] {
        // The runner process has no foreground scene, so UIScreen.main.bounds
        // returns a fallback size. The springboard application snapshot frame is
        // the device size in points; the screen scale comes from UIScreen.
        var scale: CGFloat = 1
        let collect = { scale = UIScreen.main.scale }
        if Thread.isMainThread {
            collect()
        } else {
            DispatchQueue.main.sync(execute: collect)
        }
        var width = 0
        var height = 0
        if let root = Snapshot.elements(bundleIdentifier: "com.apple.springboard").first,
           let frame = root["frame"] as? [String: Any] {
            width = Int(frame["width"] as? Double ?? 0)
            height = Int(frame["height"] as? Double ?? 0)
        }
        return [
            "widthPoints": width,
            "heightPoints": height,
            "scale": Double(scale),
        ]
    }

    private func screenshot() throws -> [String: Any] {
        let image = XCUIScreen.main.screenshot()
        return ["pngBase64": image.pngRepresentation.base64EncodedString()]
    }

    private func send(_ object: [String: Any], on connection: NWConnection) {
        guard var data = try? JSONSerialization.data(withJSONObject: object) else {
            return
        }
        data.append(0x0A)
        connection.send(content: data, completion: .contentProcessed { _ in })
    }

    enum ServerError: Error {
        case malformedRequest
        case unknownMethod(String)
    }
}

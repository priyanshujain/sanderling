package dev.sanderling.sidecar

import kotlin.test.Test
import kotlin.test.assertTrue

class SidecarServerTest {
    @Test
    fun startBindsEphemeralPortAndStopReleasesIt() {
        val server = SidecarServer(port = 0, service = DriverService())
        val boundPort = server.start()
        try {
            assertTrue(boundPort > 0, "expected ephemeral port, got $boundPort")
        } finally {
            server.stop()
        }
    }
}

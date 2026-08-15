package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertEquals

class DadbTargetTest {

    @Test fun nullSerialDefaultsToEmulatorLoopback() {
        assertEquals(DadbTarget.Tcp("localhost", 5555), dadbTargetFor(null))
    }

    @Test fun hostPortSerialConnectsDirectly() {
        assertEquals(
            DadbTarget.Tcp("192.168.1.243", 5555),
            dadbTargetFor("192.168.1.243:5555"),
        )
    }

    @Test fun usbSerialRoutesThroughAdbServer() {
        assertEquals(DadbTarget.Server("663c91b1"), dadbTargetFor("663c91b1"))
    }

    // A colon with a non-numeric port is a USB serial that merely contains a
    // colon, not a host:port, so it must route through the adb server.
    @Test fun colonWithNonNumericPortIsAServerSerial() {
        assertEquals(
            DadbTarget.Server("emulator:5554x"),
            dadbTargetFor("emulator:5554x"),
        )
    }
}

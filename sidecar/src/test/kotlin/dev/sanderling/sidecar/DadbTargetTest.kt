package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

class DadbTargetTest {

    @Test fun nullSerialDefaultsToEmulatorLoopback() {
        assertEquals(DadbTarget.Tcp("localhost", 5555), dadbTargetFor(null))
    }

    @Test fun hostPortSerialConnectsDirectly() {
        assertEquals(DadbTarget.Tcp("192.168.1.243", 5555), dadbTargetFor("192.168.1.243:5555"))
    }

    @Test fun usbSerialRoutesThroughAdbServer() {
        assertEquals(DadbTarget.Server("663c91b1"), dadbTargetFor("663c91b1"))
    }

    // A colon with a non-numeric port is a USB serial that merely contains a
    // colon, not a host:port, so it must route through the adb server.
    @Test fun colonWithNonNumericPortIsAServerSerial() {
        assertEquals(DadbTarget.Server("emulator:5554x"), dadbTargetFor("emulator:5554x"))
    }

    // A serial-addressed device is reached through whichever adb server the
    // environment names. Ignoring it sends the run to this machine's own
    // server, where the serial either is missing or, worse, names a different
    // device that happens to share the emulator numbering.
    @Test fun adbServerSocketNamesARemoteServer() {
        assertEquals(
            AdbServerEndpoint("100.68.126.75", 5037),
            adbServerEndpoint(
                env("ADB_SERVER_SOCKET" to "tcp:100.68.126.75:5037"),
            ),
        )
    }

    @Test fun adbServerSocketWithOnlyAPortStaysLocal() {
        assertEquals(
            AdbServerEndpoint("localhost", 5038),
            adbServerEndpoint(env("ADB_SERVER_SOCKET" to "tcp:5038")),
        )
    }

    @Test fun androidAdbServerAddressAndPortPairIsHonoured() {
        assertEquals(
            AdbServerEndpoint("10.0.0.4", 5040),
            adbServerEndpoint(
                env(
                    "ANDROID_ADB_SERVER_ADDRESS" to "10.0.0.4",
                    "ANDROID_ADB_SERVER_PORT" to "5040",
                ),
            ),
        )
    }

    @Test fun adbServerSocketOutranksTheOlderPair() {
        assertEquals(
            AdbServerEndpoint("100.68.126.75", 5037),
            adbServerEndpoint(
                env(
                    "ADB_SERVER_SOCKET" to "tcp:100.68.126.75:5037",
                    "ANDROID_ADB_SERVER_ADDRESS" to "10.0.0.4",
                    "ANDROID_ADB_SERVER_PORT" to "5040",
                ),
            ),
        )
    }

    @Test fun unsetEnvironmentKeepsTheLoopbackDefault() {
        assertEquals(
            AdbServerEndpoint("localhost", 5037),
            adbServerEndpoint(env()),
        )
        assertEquals(
            AdbServerEndpoint("localhost", 5037),
            adbServerEndpoint(env("ADB_SERVER_SOCKET" to "")),
        )
    }

    @Test fun eitherHalfOfTheOlderPairAloneKeepsTheOtherDefault() {
        assertEquals(
            AdbServerEndpoint("10.0.0.4", 5037),
            adbServerEndpoint(env("ANDROID_ADB_SERVER_ADDRESS" to "10.0.0.4")),
        )
        assertEquals(
            AdbServerEndpoint("localhost", 5040),
            adbServerEndpoint(env("ANDROID_ADB_SERVER_PORT" to "5040")),
        )
    }

    // A value that cannot be read must stop the run and say which variable
    // held what. Falling back to loopback would drive this machine's devices
    // while the operator believes the run is on the remote ones.
    @Test fun malformedValuesFailNamingTheVariableAndItsContents() {
        val cases = mapOf(
            "ADB_SERVER_SOCKET" to listOf(
                "100.68.126.75:5037",
                "tcp:100.68.126.75:pear",
                "tcp:",
                "tcp::5037",
                "unix:/tmp/adb",
                "tcp:100.68.126.75:70000",
            ),
            "ANDROID_ADB_SERVER_PORT" to listOf("pear", "0", "-1"),
        )
        for ((variable, values) in cases) {
            for (value in values) {
                val failure = assertFailsWith<IllegalArgumentException>(value) {
                    adbServerEndpoint(env(variable to value))
                }
                val message = failure.message.orEmpty()
                assertTrue(message.contains(variable), message)
                assertTrue(message.contains(value), message)
            }
        }
    }
}

private fun env(vararg entries: Pair<String, String>): (String) -> String? {
    val values = entries.toMap()
    return { values[it] }
}

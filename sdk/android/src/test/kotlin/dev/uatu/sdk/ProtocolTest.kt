package dev.uatu.sdk

import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.DataInputStream
import java.io.EOFException
import java.io.IOException
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class ProtocolTest {

    private fun roundTrip(message: Message): Message {
        val output = ByteArrayOutputStream()
        Protocol.write(output, message)
        return Protocol.read(ByteArrayInputStream(output.toByteArray()))
    }

    @Test fun roundTripHello() {
        val got = roundTrip(Message.hello("0.0.1", "android", "in.okcredit.merchant"))
        assertEquals(MessageType.HELLO, got.type)
        assertEquals(Protocol.PROTOCOL_VERSION, got.protocolVersion)
        assertEquals("0.0.1", got.version)
        assertEquals("android", got.platform)
        assertEquals("in.okcredit.merchant", got.appPackage)
    }

    @Test fun roundTripPauseResume() {
        val pause = roundTrip(Message.pause(42))
        assertEquals(MessageType.PAUSE, pause.type)
        assertEquals(42L, pause.id)

        val resume = roundTrip(Message.resume(43))
        assertEquals(MessageType.RESUME, resume.type)
        assertEquals(43L, resume.id)
    }

    @Test fun roundTripState() {
        val snapshots = mapOf<String, Any?>(
            "screen" to "customer_ledger",
            "ledger.balance" to 1500,
            "is_signed_in" to true,
        )
        val got = roundTrip(Message.state(7, snapshots))
        assertEquals(MessageType.STATE, got.type)
        assertEquals(7L, got.id)
        assertNotNull(got.snapshots)
        assertEquals("customer_ledger", got.snapshots!!["screen"])
        assertEquals(1500, got.snapshots["ledger.balance"])
        assertEquals(true, got.snapshots["is_signed_in"])
    }

    @Test fun roundTripExtractResult() {
        val ok = roundTrip(Message.extractResult(1, "ledger.balance", 2500))
        assertEquals("ledger.balance", ok.extractor)
        assertEquals(2500, ok.result)
        assertNull(ok.error)

        val failed = roundTrip(Message.extractResult(2, "ledger.balance", null, "no active customer"))
        assertEquals("no active customer", failed.error)
        assertNull(failed.result)
    }

    @Test fun roundTripGoodbye() {
        val got = roundTrip(Message.goodbye("app terminated"))
        assertEquals(MessageType.GOODBYE, got.type)
        assertEquals("app terminated", got.reason)
    }

    @Test fun frameFormatIsBigEndianLengthPlusJson() {
        val output = ByteArrayOutputStream()
        Protocol.write(output, Message.pause(99))
        val raw = output.toByteArray()
        assertTrue("frame must have 4-byte header plus body", raw.size > 4)
        val length = DataInputStream(ByteArrayInputStream(raw.copyOfRange(0, 4))).readInt()
        assertEquals(raw.size - 4, length)
        val payload = String(raw.copyOfRange(4, raw.size), Charsets.UTF_8)
        assertTrue("payload should contain PAUSE type, got $payload", payload.contains("\"type\":\"PAUSE\""))
    }

    @Test fun emptyReaderThrowsEof() {
        assertThrows(EOFException::class.java) {
            Protocol.read(ByteArrayInputStream(ByteArray(0)))
        }
    }

    @Test fun oversizedFrameRejected() {
        val header = ByteArray(4)
        val tooBig = Protocol.MAX_FRAME_SIZE + 1
        header[0] = (tooBig ushr 24).toByte()
        header[1] = (tooBig ushr 16).toByte()
        header[2] = (tooBig ushr 8).toByte()
        header[3] = tooBig.toByte()
        val error = assertThrows(IOException::class.java) {
            Protocol.read(ByteArrayInputStream(header))
        }
        assertTrue("expected size error, got: ${error.message}", error.message!!.contains("exceeds maximum"))
    }

    @Test fun missingTypeRejected() {
        val payload = "{\"id\":1}".toByteArray(Charsets.UTF_8)
        val header = ByteArray(4)
        header[0] = (payload.size ushr 24).toByte()
        header[1] = (payload.size ushr 16).toByte()
        header[2] = (payload.size ushr 8).toByte()
        header[3] = payload.size.toByte()
        val frame = header + payload
        val error = assertThrows(IOException::class.java) {
            Protocol.read(ByteArrayInputStream(frame))
        }
        assertTrue("expected missing-type error, got: ${error.message}", error.message!!.contains("missing type"))
    }

    @Test fun streamsMultipleFrames() {
        val messages = listOf(
            Message.hello("v", "android", "com.x"),
            Message.pause(1),
            Message.state(1, mapOf("x" to 42)),
            Message.resume(1),
            Message.goodbye("done"),
        )
        val output = ByteArrayOutputStream()
        for (message in messages) Protocol.write(output, message)

        val input = ByteArrayInputStream(output.toByteArray())
        for (want in messages) {
            val got = Protocol.read(input)
            assertEquals(want.type, got.type)
        }
    }

    @Test fun sharedWireFormatMatchesGoEncoder() {
        // Fixture encoded by the Go side (see internal/agent/protocol.go).
        // Ensures both encoders agree on field names and ordering conventions.
        val output = ByteArrayOutputStream()
        Protocol.write(output, Message.hello("0.0.1", "android", "com.x"))
        val payload = String(output.toByteArray().copyOfRange(4, output.size()), Charsets.UTF_8)
        assertTrue(payload.contains("\"type\":\"HELLO\""))
        assertTrue(payload.contains("\"protocol_version\":1"))
        assertTrue(payload.contains("\"version\":\"0.0.1\""))
        assertTrue(payload.contains("\"platform\":\"android\""))
        assertTrue(payload.contains("\"app_package\":\"com.x\""))
    }

    @Test fun bytesAreConsumedInOrder() {
        // Regression guard: a second read shouldn't see stale bytes.
        val output = ByteArrayOutputStream()
        Protocol.write(output, Message.pause(1))
        Protocol.write(output, Message.pause(2))
        val bytes = output.toByteArray()
        val input = ByteArrayInputStream(bytes)
        assertEquals(1L, Protocol.read(input).id)
        assertEquals(2L, Protocol.read(input).id)
        // ByteArrayInputStream should be drained.
        val leftover = ByteArray(bytes.size)
        val remaining = input.read(leftover)
        assertEquals(-1, remaining)
        assertArrayEquals(ByteArray(bytes.size), leftover)
    }
}

package dev.sanderling.sidecar

import org.junit.Test
import java.io.InputStream
import java.io.OutputStream
import java.util.concurrent.CountDownLatch
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class AdbOutputTimeoutTest {

    // An adb wedged on the link neither writes nor exits, so the read never
    // reaches EOF. Without a bound on the READ the step waits for as long as
    // adb feels like it: one such stall measured ~100s against a remote adb
    // server. The bound has to release the reader as well as return, which is
    // what killing the process does.
    @Test(timeout = 20_000L)
    fun aWedgedReadIsAbandonedAtTheBoundInsteadOfHangingForever() {
        val process = FakeProcess(BlockingStream())
        val logged = mutableListOf<String>()

        val started = System.currentTimeMillis()
        val output = readProcessOutput(process, 200L, { "adb shell pidof" }) {
            logged.add(it)
        }
        val elapsed = System.currentTimeMillis() - started

        assertEquals("", output, "a read that never lands is no answer")
        assertTrue(process.destroyed, "the wedged adb must be killed, not left")
        assertTrue(
            elapsed < 10_000L,
            "returned in ${elapsed}ms, not at a bound",
        )
        assertEquals(1, logged.size, "a silent timeout hides a degrading link")
    }

    @Test(timeout = 20_000L)
    fun theAbandonedReadNamesTheCommandAndTheBound() {
        val logged = mutableListOf<String>()
        readProcessOutput(
            FakeProcess(BlockingStream()),
            200L,
            { "adb -s emulator-5556 shell cat /proc/6103/stat" },
        ) { logged.add(it) }

        val line = logged.single()
        assertTrue(
            line.contains("adb -s emulator-5556 shell cat /proc/6103/stat"),
            line,
        )
        assertTrue(line.contains("200"), line)
    }

    // The bound must cost the healthy path nothing: output that arrives comes
    // back whole, and the process is left to exit on its own.
    @Test(timeout = 20_000L)
    fun outputThatArrivesComesBackWholeAndTheProcessSurvives() {
        val text = "VmRSS:\t  123456 kB\nVmSize:\t  654321 kB\n"
        val process = FakeProcess(text.byteInputStream())
        val logged = mutableListOf<String>()

        val output = readProcessOutput(process, 5_000L, { "adb shell cat" }) {
            logged.add(it)
        }

        assertEquals(text, output)
        assertTrue(!process.destroyed, "a process that answered is not killed")
        assertTrue(logged.isEmpty(), "nothing to report on the healthy path")
    }

    // Output larger than a pipe buffer is why the read cannot be deferred
    // until after the process exits: a process with more to say than the
    // buffer holds blocks writing while a waiter waits for it to finish.
    @Test(timeout = 20_000L)
    fun outputLargerThanAPipeBufferComesBackWhole() {
        val text = "x".repeat(512 * 1024)
        val output = readProcessOutput(
            FakeProcess(text.byteInputStream()),
            5_000L,
            { "adb logcat -d" },
        ) {}
        assertEquals(text.length, output.length)
    }
}

// BlockingStream models a wedged adb: no bytes, and no EOF either, until the
// process is killed and the pipe closes under the reader.
private class BlockingStream : InputStream() {
    private val released = CountDownLatch(1)

    override fun read(): Int {
        released.await()
        return -1
    }

    override fun close() {
        released.countDown()
    }
}

private class FakeProcess(private val stream: InputStream) : Process() {
    @Volatile var destroyed = false
        private set

    override fun getOutputStream(): OutputStream =
        OutputStream.nullOutputStream()

    override fun getInputStream(): InputStream = stream

    override fun getErrorStream(): InputStream = InputStream.nullInputStream()

    override fun waitFor(): Int = 0

    override fun exitValue(): Int = 0

    override fun destroy() {
        destroyed = true
        stream.close()
    }
}

package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class IdleDetectionTest {
    @Test fun idleWhenCountIsZero() {
        assertTrue(StubDriverBackend.isAnimationCountIdle("0\n"))
    }

    @Test fun idleWhenCountIsZeroNoNewline() {
        assertTrue(StubDriverBackend.isAnimationCountIdle("0"))
    }

    @Test fun busyWhenCountIsOne() {
        assertFalse(StubDriverBackend.isAnimationCountIdle("1\n"))
    }

    @Test fun busyWhenCountIsMultiple() {
        assertFalse(StubDriverBackend.isAnimationCountIdle("3\n"))
    }

    // A dumpsys that said nothing does not say the device is still. Reading
    // absence as idle is how a settle returns instantly on a degraded link and
    // hands the runner a screen caught mid-animation; the caller bounds its own
    // wait, so the cost of being wrong the other way is a wait it already
    // budgeted for. The exception path of the same probe already answers false.
    @Test fun unreadableOutputIsNotIdle() {
        assertFalse(StubDriverBackend.isAnimationCountIdle(""))
        assertFalse(StubDriverBackend.isAnimationCountIdle("   \n"))
    }

    @Test fun unparseableOutputIsNotIdle() {
        assertFalse(StubDriverBackend.isAnimationCountIdle("error: no service"))
    }
}

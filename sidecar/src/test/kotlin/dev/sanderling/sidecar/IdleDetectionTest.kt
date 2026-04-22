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

    @Test fun idleWhenOutputEmpty() {
        assertTrue(StubDriverBackend.isAnimationCountIdle(""))
    }

    @Test fun idleWhenOutputIsNotANumber() {
        assertTrue(StubDriverBackend.isAnimationCountIdle("error: no service"))
    }
}

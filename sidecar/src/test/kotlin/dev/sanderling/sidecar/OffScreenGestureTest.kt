package dev.sanderling.sidecar

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class OffScreenGestureTest {

    @Test
    fun `a point inside the screen is delivered`() {
        assertFalse(offScreen(160, 283, 320, 640))
        assertFalse(offScreen(0, 0, 320, 640))
        assertFalse(offScreen(319, 639, 320, 640))
    }

    @Test
    fun `a point past an edge is off screen`() {
        assertTrue(offScreen(160, 900, 320, 640))
        assertTrue(offScreen(900, 283, 320, 640))
        assertTrue(offScreen(160, -100, 320, 640))
        assertTrue(offScreen(-1, 283, 320, 640))
    }

    @Test
    fun `the far edge is exclusive because InputDispatcher drops it`() {
        assertTrue(offScreen(320, 283, 320, 640))
        assertTrue(offScreen(160, 640, 320, 640))
    }
}

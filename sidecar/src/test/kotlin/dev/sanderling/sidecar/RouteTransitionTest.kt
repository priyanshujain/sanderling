package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

// The Android backend's snapshot waits out a NavHost cross-fade before it
// reads. Without that wait the runner is handed a tree holding two routes at
// once, refuses to act on it, and the step applies nothing; how many steps land
// that way varies run to run, so the same seed walks a different trajectory
// each time. These cover the wait (awaitSettledTree) and what it costs.
class RouteTransitionTest {
    private fun screen(id: String, child: String = "") =
        """{"attributes":{"resource-id":"$id"},"children":[$child]}"""

    private fun tree(vararg children: String) =
        """{"attributes":{"resource-id":"root"},"children":[${children.joinToString(",")}]}"""

    private val crossFade = tree(screen("LedgerScreen"), screen("AddTransactionScreen"))
    private val landed = tree(screen("AddTransactionScreen"))

    @Test fun waitsForTheCrossFadeToLandAndReturnsTheLandedTree() {
        // The first read caught both routes alive. The wait must keep reading
        // until only the destination is left, and hand back that tree: the
        // caller records what it returns, so returning the fade would put the
        // frame in the trace whether or not the wait happened.
        var reads = 0
        val settled = awaitSettledTree {
            reads++
            if (reads <= 3) crossFade else landed
        }
        assertTrue(reads > 3, "must keep reading until the fade lands, reads=$reads")
        assertEquals(1, countRouteScreens(settled), "must return a tree with one route")
    }

    @Test fun settledFrameCostsExactlyOneRead() {
        // A hierarchy read is the expensive part of a step, and this one is the
        // read the snapshot was going to do anyway. A frame that is already on
        // one route must not pay for a second: that cost, on every step, is why
        // the unconditional structural poll was removed from waitForIdle.
        var reads = 0
        val settled = awaitSettledTree {
            reads++
            landed
        }
        assertEquals(1, reads, "a one-route frame must read once and return")
        assertEquals(landed, settled)
    }

    @Test fun repeatedRouteIdIsOneScreenNotATransition() {
        // A screen nesting a node that repeats its own route id puts two tagged
        // nodes in the tree while one destination is on screen. Counting nodes
        // would read that as a fade that never ends: every step of that screen
        // would burn the whole poll budget and still hand over a frame the
        // runner refuses to act on.
        val nested = tree(screen("HomeScreen", screen("HomeScreen")))
        assertEquals(1, countRouteScreens(nested), "the same id twice is one route")
        var reads = 0
        awaitSettledTree {
            reads++
            nested
        }
        assertEquals(1, reads, "a repeated route id must not be treated as a transition")
    }

    @Test fun aLayoutThatKeepsTwoRoutesIsBoundedByTheCap() {
        // Two routes alive at rest is a real layout, not a fade, and no amount
        // of waiting will resolve it. The wait is bounded by wall clock, so
        // such a screen costs the cap once per step and nothing more.
        val start = System.currentTimeMillis()
        val settled = awaitSettledTree { crossFade }
        val elapsed = System.currentTimeMillis() - start
        assertTrue(
            elapsed >= TRANSITION_POLL_CAP_MILLIS,
            "must actually wait out a two-route tree, elapsed=${elapsed}ms",
        )
        assertTrue(
            elapsed < TRANSITION_POLL_CAP_MILLIS + 1000L,
            "must stop at the cap, elapsed=${elapsed}ms",
        )
        assertEquals(crossFade, settled, "the caller still gets a tree to record")
    }

    @Test fun capCoversTheNavHostFadePlusTheStreak() {
        // Compose navigation's default enter and exit are a 700ms tween, and
        // the fade starts when the action lands, not when the snapshot begins.
        // A cap that does not clear the fade and the streak after it hands the
        // caller a transitional frame, which is the whole defect.
        val fadeMillis = 700L
        val start = System.currentTimeMillis()
        val settled = awaitSettledTree {
            if (System.currentTimeMillis() - start < fadeMillis) crossFade else landed
        }
        val elapsed = System.currentTimeMillis() - start

        assertEquals(landed, settled, "must hand back the landed tree, not the fade")
        assertTrue(
            elapsed >= fadeMillis,
            "cannot have settled before the fade ended, elapsed=${elapsed}ms",
        )
        assertTrue(
            elapsed < TRANSITION_POLL_CAP_MILLIS,
            "the ${TRANSITION_POLL_CAP_MILLIS}ms cap has to leave room for a ${fadeMillis}ms " +
                "fade and the ${TRANSITION_STABLE_STREAK_MILLIS}ms streak after it, but the " +
                "wait ran to the cap instead, elapsed=${elapsed}ms",
        )
    }
}

package dev.uatu.sdk

import android.view.Choreographer

class ChoreographerPoster : FrameCallbackPoster {
    override fun postFrameCallback(callback: () -> Unit) {
        Choreographer.getInstance().postFrameCallback { callback() }
    }
}

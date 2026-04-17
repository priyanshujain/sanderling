package dev.uatu.sdk

import android.os.Handler
import android.os.Looper
import android.view.Choreographer

class ChoreographerPoster : FrameCallbackPoster {
    private val mainHandler = Handler(Looper.getMainLooper())

    override fun postFrameCallback(callback: () -> Unit) {
        mainHandler.post {
            Choreographer.getInstance().postFrameCallback { callback() }
        }
    }
}

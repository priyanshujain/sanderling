package app.folio

import androidx.compose.ui.window.ComposeUIViewController
import app.folio.core.data.DriverFactory
import app.folio.db.LedgerDatabase
import app.folio.di.AppGraph
import app.folio.ui.IosBackGesture
import dev.zacsweers.metro.createGraphFactory
import kotlinx.cinterop.BetaInteropApi
import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.cinterop.ObjCAction
import platform.Foundation.NSSelectorFromString
import platform.UIKit.UIGestureRecognizerStateEnded
import platform.UIKit.UIRectEdgeLeft
import platform.UIKit.UIScreenEdgePanGestureRecognizer
import platform.UIKit.UIViewController
import platform.darwin.NSObject

@OptIn(ExperimentalForeignApi::class, BetaInteropApi::class)
class BackGestureTarget : NSObject() {
    @ObjCAction
    fun handleGesture(gesture: UIScreenEdgePanGestureRecognizer) {
        if (gesture.state == UIGestureRecognizerStateEnded) {
            IosBackGesture.dispatch()
        }
    }
}

private val backGestureTarget = BackGestureTarget()

@OptIn(ExperimentalForeignApi::class)
fun MainViewController(): UIViewController {
    val driverFactory = DriverFactory()
    val graphFactory = createGraphFactory<AppGraph.Factory>()
    val vc = ComposeUIViewController {
        App(graphBuilder = { graphFactory.create(LedgerDatabase(driverFactory.create())) })
    }
    val gesture = UIScreenEdgePanGestureRecognizer(
        target = backGestureTarget,
        action = NSSelectorFromString("handleGesture:"),
    )
    gesture.edges = UIRectEdgeLeft
    vc.view.addGestureRecognizer(gesture)
    return vc
}

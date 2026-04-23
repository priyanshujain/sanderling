package app.folio

import androidx.compose.ui.window.ComposeUIViewController
import app.folio.data.Repository
import app.folio.ui.IosBackGesture
import kotlin.native.ObjCName
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
    Repository.init()
    app.folio.sanderling.SanderlingIos.start()
    app.folio.sanderling.AuthSnapshots
    app.folio.sanderling.AccountSnapshots
    app.folio.sanderling.LedgerSnapshots
    app.folio.sanderling.NavigationSnapshots
    val vc = ComposeUIViewController { App() }
    val gesture = UIScreenEdgePanGestureRecognizer(
        target = backGestureTarget,
        action = NSSelectorFromString("handleGesture:"),
    )
    gesture.edges = UIRectEdgeLeft
    vc.view.addGestureRecognizer(gesture)
    return vc
}

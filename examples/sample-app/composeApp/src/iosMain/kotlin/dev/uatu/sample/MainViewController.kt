package dev.uatu.sample

import androidx.compose.ui.window.ComposeUIViewController
import platform.UIKit.UIViewController

fun MainViewController(): UIViewController {
    Repository.init(DriverFactory())
    return ComposeUIViewController { App() }
}

package app.folio.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp

data class Tokens(
    val bg: Color = Color(0xFFFFFFFF),
    val surface: Color = Color(0xFFFAFAFA),
    val surface2: Color = Color(0xFFF4F4F4),
    val surface3: Color = Color(0xFFEBEBEB),
    val border: Color = Color(0xFFE7E7E7),
    val borderStrong: Color = Color(0xFFD6D6D6),
    val text: Color = Color(0xFF000000),
    val textMuted: Color = Color(0xFF6B6B6B),
    val textFaint: Color = Color(0xFF9A9A9A),
    val onAccent: Color = Color(0xFFFFFFFF),
)

val LocalTokens = staticCompositionLocalOf { Tokens() }

@Composable
fun LedgerTheme(content: @Composable () -> Unit) {
    @Suppress("UNUSED_VARIABLE")
    val dark = isSystemInDarkTheme()
    val scheme = lightColorScheme(
        background = Color(0xFFFFFFFF),
        surface = Color(0xFFFAFAFA),
        primary = Color(0xFF000000),
        onPrimary = Color(0xFFFFFFFF),
    )
    MaterialTheme(colorScheme = scheme) { content() }
}

val FrameWidth = 420.dp
val FrameHeight = 900.dp
val ScreenPad = 20.dp
val RadiusSm = 8.dp
val RadiusMd = 12.dp
val RadiusLg = 16.dp
val RadiusPill = 999.dp

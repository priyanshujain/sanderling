package dev.uatu.sample.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

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

val Mono: FontFamily = FontFamily.Monospace

object Type {
    val body = TextStyle(fontFamily = Mono, fontSize = 15.sp, fontWeight = FontWeight.Normal)
    val bodyStrong = TextStyle(fontFamily = Mono, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
    val title = TextStyle(fontFamily = Mono, fontSize = 20.sp, fontWeight = FontWeight.Bold, letterSpacing = (-0.2).sp)
    val caption = TextStyle(fontFamily = Mono, fontSize = 12.sp, fontWeight = FontWeight.Normal)
    val label = TextStyle(fontFamily = Mono, fontSize = 12.sp, fontWeight = FontWeight.Normal, letterSpacing = 0.7.sp)
    val balance = TextStyle(fontFamily = Mono, fontSize = 28.sp, fontWeight = FontWeight.Bold, letterSpacing = (-0.3).sp)
    val amountInput = TextStyle(fontFamily = Mono, fontSize = 36.sp, fontWeight = FontWeight.Bold, letterSpacing = (-0.3).sp)
    val button = TextStyle(fontFamily = Mono, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
    val brand = TextStyle(fontFamily = Mono, fontSize = 22.sp, fontWeight = FontWeight.Bold, letterSpacing = (-0.2).sp)
    val status = TextStyle(fontFamily = Mono, fontSize = 12.sp, fontWeight = FontWeight.SemiBold, letterSpacing = 0.5.sp)
}

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

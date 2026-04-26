package app.folio.ui.theme

import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

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
}

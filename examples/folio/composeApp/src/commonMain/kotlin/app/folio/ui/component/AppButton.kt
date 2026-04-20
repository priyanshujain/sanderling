package app.folio.ui.component

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.RadiusMd
import app.folio.ui.theme.Type

enum class ButtonStyle { Primary, Secondary, Ghost }

@Composable
fun AppButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    style: ButtonStyle = ButtonStyle.Secondary,
    enabled: Boolean = true,
    description: String? = null,
) {
    val t = LocalTokens.current
    val (bg, fg, border) = when (style) {
        ButtonStyle.Primary -> Triple(t.text, t.onAccent, t.text)
        ButtonStyle.Secondary -> Triple(t.surface2, t.text, t.borderStrong)
        ButtonStyle.Ghost -> Triple(t.bg, t.textMuted, t.bg)
    }
    Box(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(RadiusMd))
            .background(if (enabled) bg else t.surface3)
            .border(BorderStroke(1.dp, if (enabled) border else t.border), RoundedCornerShape(RadiusMd))
            .then(
                if (description != null) Modifier.semantics { contentDescription = description }
                else Modifier
            )
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(vertical = 14.dp, horizontal = 16.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text, style = Type.button, color = if (enabled) fg else t.textFaint)
    }
}

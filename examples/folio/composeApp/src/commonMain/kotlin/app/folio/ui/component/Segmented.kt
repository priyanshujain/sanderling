package app.folio.ui.component

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
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
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.RadiusMd
import app.folio.ui.theme.RadiusSm
import app.folio.ui.theme.Type

@Composable
fun Segmented(
    selected: Int,
    labels: List<String>,
    onSelect: (Int) -> Unit,
    descriptions: List<String>? = null,
) {
    val t = LocalTokens.current
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(RadiusMd))
            .background(t.surface)
            .border(1.dp, t.border, RoundedCornerShape(RadiusMd))
            .padding(4.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        labels.forEachIndexed { i, label ->
            val active = i == selected
            val desc = descriptions?.getOrNull(i)
            Box(
                modifier = Modifier
                    .weight(1f)
                    .clip(RoundedCornerShape(RadiusSm))
                    .background(if (active) t.surface3 else t.surface)
                    .semantics {
                        this.selected = active
                        if (desc != null) contentDescription = desc
                    }
                    .clickable(role = Role.Tab) { onSelect(i) }
                    .padding(vertical = 10.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text(label, style = Type.button, color = if (active) t.text else t.textMuted)
            }
        }
    }
}

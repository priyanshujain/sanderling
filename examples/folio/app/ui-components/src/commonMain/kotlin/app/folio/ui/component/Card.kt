package app.folio.ui.component

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.RadiusLg

@Composable
fun Card(
    modifier: Modifier = Modifier,
    dashed: Boolean = false,
    content: @Composable () -> Unit,
) {
    val t = LocalTokens.current
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(RadiusLg))
            .background(if (dashed) t.surface2 else t.surface)
            .border(1.dp, t.border, RoundedCornerShape(RadiusLg))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) { content() }
}

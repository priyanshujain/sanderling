package app.folio.ui.component

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.Type

@Composable
fun EmptyState(title: String, subtitle: String, icon: ImageVector? = null) {
    val t = LocalTokens.current
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 48.dp, horizontal = 16.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        if (icon != null) {
            Box(
                modifier = Modifier
                    .size(56.dp)
                    .background(t.surface2, CircleShape)
                    .border(1.dp, t.border, CircleShape),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    painter = rememberVectorPainter(icon),
                    contentDescription = null,
                    tint = t.textMuted,
                    modifier = Modifier.size(22.dp),
                )
            }
            Spacer(Modifier.height(2.dp))
        }
        Text(title, style = Type.bodyStrong, color = t.text, textAlign = TextAlign.Center)
        Text(subtitle, style = Type.caption, color = t.textMuted, textAlign = TextAlign.Center)
    }
}

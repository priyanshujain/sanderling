package app.folio.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

@Composable
fun Screen(
    header: @Composable (() -> Unit)? = null,
    footer: @Composable (() -> Unit)? = null,
    content: @Composable () -> Unit,
) {
    val t = LocalTokens.current
    Column(Modifier.fillMaxSize()) {
        if (header != null) header()
        Column(
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = ScreenPad, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            content()
            Spacer(Modifier.height(16.dp))
        }
        if (footer != null) {
            Box(Modifier.fillMaxWidth().height(1.dp).background(t.border))
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(t.bg)
                    .padding(horizontal = ScreenPad, vertical = 16.dp),
                verticalArrangement = Arrangement.spacedBy(14.dp),
            ) {
                footer()
            }
        }
    }
}

@Composable
fun Header(
    title: String,
    subtitle: String? = null,
    left: @Composable (() -> Unit)? = null,
    right: @Composable (() -> Unit)? = null,
) {
    val t = LocalTokens.current
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = ScreenPad, end = ScreenPad, top = 14.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (left != null) left()
        Column(Modifier.weight(1f)) {
            Text(
                title,
                style = Type.title,
                color = t.text,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (subtitle != null) {
                Text(subtitle, style = Type.caption, color = t.textMuted)
            }
        }
        if (right != null) right()
    }
}

@Composable
fun IconButton(
    onClick: () -> Unit,
    description: String,
    icon: ImageVector,
) {
    val t = LocalTokens.current
    Box(
        modifier = Modifier
            .size(36.dp)
            .clip(RoundedCornerShape(10.dp))
            .background(t.surface2)
            .border(1.dp, t.border, RoundedCornerShape(10.dp))
            .clickable(onClick = onClick)
            .semantics { contentDescription = description },
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            painter = rememberVectorPainter(icon),
            contentDescription = null,
            tint = t.text,
            modifier = Modifier.size(16.dp),
        )
    }
}

@Composable
fun BackButton(onClick: () -> Unit) {
    IconButton(onClick = onClick, description = "Back", icon = Icons.Back)
}

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

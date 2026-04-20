package app.folio.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import app.folio.FocusTracker

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

@Composable
fun FieldLabel(text: String) {
    val t = LocalTokens.current
    Text(text.uppercase(), style = Type.label, color = t.textMuted)
}

@Composable
fun TextInput(
    value: String,
    onChange: (String) -> Unit,
    placeholder: String = "",
    invalid: Boolean = false,
    password: Boolean = false,
    keyboardType: KeyboardType = KeyboardType.Text,
    textAlign: TextAlign = TextAlign.Start,
    textStyle: TextStyle = Type.body,
    label: String? = null,
    description: String? = null,
    modifier: Modifier = Modifier,
) {
    val t = LocalTokens.current
    var focused by remember { mutableStateOf(false) }
    val borderColor = if (focused || invalid) t.text else t.border
    val bg = if (focused) t.surface2 else t.surface
    Box(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(RadiusMd))
            .background(bg)
            .border(BorderStroke(1.dp, borderColor), RoundedCornerShape(RadiusMd))
            .padding(horizontal = 14.dp, vertical = 14.dp),
        contentAlignment = if (textAlign == TextAlign.Center) Alignment.Center else Alignment.CenterStart,
    ) {
        if (value.isEmpty() && placeholder.isNotEmpty()) {
            Text(
                placeholder,
                style = textStyle.copy(color = t.textFaint, textAlign = textAlign),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        BasicTextField(
            value = value,
            onValueChange = onChange,
            textStyle = textStyle.copy(color = t.text, textAlign = textAlign),
            singleLine = true,
            visualTransformation = if (password) PasswordVisualTransformation() else VisualTransformation.None,
            keyboardOptions = KeyboardOptions(keyboardType = keyboardType),
            cursorBrush = SolidColor(t.text),
            modifier = Modifier
                .fillMaxWidth()
                .onFocusChanged {
                    focused = it.isFocused
                    if (description != null) {
                        if (it.isFocused) FocusTracker.enter(description)
                        else FocusTracker.leave(description)
                    }
                }
                .semantics {
                    val desc = description ?: label
                    if (desc != null) contentDescription = desc
                    if (invalid) stateDescription = "Invalid"
                },
        )
    }
}

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

@Composable
fun ErrorText(err: String?) {
    val t = LocalTokens.current
    Text(
        err ?: "",
        style = Type.caption,
        color = t.text,
        modifier = Modifier.fillMaxWidth().height(18.dp),
    )
}

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

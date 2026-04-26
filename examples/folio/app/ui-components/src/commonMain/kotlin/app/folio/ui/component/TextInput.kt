package app.folio.ui.component

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
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
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.RadiusMd
import app.folio.ui.theme.Type

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
    label: String,
    testTag: String? = null,
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
                .onFocusChanged { focused = it.isFocused }
                .then(if (testTag != null) Modifier.testTag(testTag) else Modifier)
                .semantics {
                    contentDescription = label
                    if (invalid) stateDescription = "Invalid"
                },
        )
    }
}

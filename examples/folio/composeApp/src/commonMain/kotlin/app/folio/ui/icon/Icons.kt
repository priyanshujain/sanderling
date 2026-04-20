package app.folio.ui.icon

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.unit.dp

object Icons {
    val Back: ImageVector = strokeIcon("M15 18 L9 12 L15 6")

    val Logout: ImageVector = strokeIcon(
        "M9 21 H5 A2 2 0 0 1 3 19 V5 A2 2 0 0 1 5 3 H9 " +
            "M16 17 L21 12 L16 7 " +
            "M21 12 H9",
    )

    val Bank: ImageVector = strokeIcon(
        "M3 6 H21 V19 A2 2 0 0 1 19 21 H5 A2 2 0 0 1 3 19 Z " +
            "M3 10 H21",
    )

    val Lines: ImageVector = strokeIcon(
        "M3 6 H21 " +
            "M3 12 H21 " +
            "M3 18 H15",
    )
}

private fun strokeIcon(path: String): ImageVector =
    ImageVector.Builder(
        defaultWidth = 24.dp,
        defaultHeight = 24.dp,
        viewportWidth = 24f,
        viewportHeight = 24f,
    ).addPath(
        pathData = addPathNodes(path),
        stroke = SolidColor(Color.Black),
        strokeLineWidth = 2f,
        strokeLineCap = StrokeCap.Round,
        strokeLineJoin = StrokeJoin.Round,
    ).build()

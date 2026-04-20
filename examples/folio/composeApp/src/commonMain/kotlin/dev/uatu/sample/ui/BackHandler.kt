package dev.uatu.sample.ui

import androidx.compose.runtime.Composable

@Composable
expect fun BackHandler(onBack: () -> Unit)

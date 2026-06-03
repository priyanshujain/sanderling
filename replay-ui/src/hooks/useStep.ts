import { useEffect } from "react";
import { useNavigate, useParams } from "react-router-dom";

export interface UseStepResult {
  runId: string;
  stepIndex: number;
  goTo: (index: number) => void;
}

export function useStep(maxIndex: number | undefined): UseStepResult {
  const navigate = useNavigate();
  const { id, step } = useParams<{ id: string; step: string }>();
  const runId = id ?? "";
  const parsed = Number(step ?? "1");
  const stepIndex = Number.isFinite(parsed) && parsed > 0 ? parsed : 1;

  const clamped = clampIndex(stepIndex, maxIndex);

  useEffect(() => {
    if (clamped !== stepIndex && runId !== "") {
      navigate(`/runs/${runId}/steps/${clamped}`, { replace: true });
    }
  }, [clamped, stepIndex, runId, navigate]);

  const goTo = (index: number) => {
    const target = clampIndex(index, maxIndex);
    if (target !== stepIndex && runId !== "") {
      navigate(`/runs/${runId}/steps/${target}`);
    }
  };

  return { runId, stepIndex: clamped, goTo };
}

function clampIndex(index: number, maxIndex: number | undefined): number {
  if (index < 1) {
    return 1;
  }
  if (maxIndex !== undefined && index > maxIndex) {
    return maxIndex;
  }
  return index;
}

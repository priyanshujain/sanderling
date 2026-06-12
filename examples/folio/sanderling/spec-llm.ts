// LLM action-backend variant of spec.ts.
//
// The properties and the login `setup` are reused verbatim; only the action
// generator changes. Instead of the seeded fuzzer drawing a random candidate,
// `llm({ model })` hands selection to a vision model: each step it sees the
// screenshot plus the candidate list the system already enumerates and returns
// which candidate to act on. The candidate set, the typed input values, action
// execution, and the trace are all identical to the seeded run.
//
// Requirements:
//   - OPENROUTER_API_KEY (OpenRouter) or OPENAI_API_KEY (OpenAI) in the
//     environment; OpenRouter wins when both are set. With a plain OpenAI key,
//     drop the vendor prefix from the model id ("gpt-5.4-nano").
//   - A model that supports image input AND strict json_schema structured
//     outputs. A model lacking either fails clearly.
import { llm } from "@sanderling/spec";

export { properties, setup } from "./spec";

export const actionsRoot = llm({ model: "openai/gpt-5.4-nano" });

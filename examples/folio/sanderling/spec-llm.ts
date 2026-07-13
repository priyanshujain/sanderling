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

// instructions only describe WHAT the app is — its purpose and features. They
// say nothing about HOW to test it: no bug, no technique, no "try to break it"
// (the base prompt already carries the bug-finding goal). The model figures out
// how to test entirely on its own. If we encoded the answer here, a "pass"
// would prove nothing and the feature would be worse than useless.
export const actionsRoot = llm({
  model: "gpt-5.4-nano",
  instructions:
    "Folio is a personal-finance ledger app. After signing in, the home screen lists accounts, each with a balance. You can create accounts, open an account to see its ledger, and add transactions; each transaction has an amount and changes that account's balance and the overall total.",
});

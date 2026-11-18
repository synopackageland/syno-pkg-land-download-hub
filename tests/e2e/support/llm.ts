// The e2e agent's model: magpie's GPT-6 Luna at max reasoning effort. Each
// request's model, effort and token usage go to E2E_LLM_CALLS (JSON lines),
// so a run proves the effort reached the provider and counts its calls.
import { appendFileSync } from 'node:fs';
import { createOpenAI } from '@ai-sdk/openai';

type Env = Record<string, string | undefined>;
type Fetch = (input: any, init?: RequestInit) => Promise<Response>;

function recording(log: string | undefined, upstream: Fetch): Fetch {
  if (!log) return upstream;
  return async (input, init) => {
    const request = JSON.parse(String(init?.body ?? '{}'));
    const response = await upstream(input, init);
    const usage = await response.clone().json().then(body => body.usage ?? {}, () => ({}));
    appendFileSync(log, JSON.stringify({
      model: request.model, reasoningEffort: request.reasoning_effort ?? null,
      inputTokens: usage.prompt_tokens ?? null, cachedTokens: usage.prompt_tokens_details?.cached_tokens ?? null,
      outputTokens: usage.completion_tokens ?? null, time: new Date().toISOString(),
    }) + '\n', { mode: 0o600 });
    return response;
  };
}

export function magpie(env: Env = process.env, upstream: Fetch = fetch) {
  return createOpenAI({
    baseURL: env.E2E_LLM_BASE_URL ?? 'http://127.0.0.1:3425/v1', apiKey: 'magpie-local',
    fetch: recording(env.E2E_LLM_CALLS, upstream),
  }).chat(env.E2E_LLM_MODEL ?? 'codex/gpt-6-luna');
}

/** The config's `agents.default` model and provider options. */
export function agentOptions(env: Env = process.env) {
  return {
    model: magpie(env),
    providerOptions: { openai: { reasoningEffort: env.E2E_LLM_REASONING_EFFORT ?? 'max' } },
  };
}
